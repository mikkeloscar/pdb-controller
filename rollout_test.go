package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

var rolloutGVRToListKind = map[schema.GroupVersionResource]string{
	rolloutGVR: "RolloutList",
}

// rolloutObj builds an unstructured argoproj.io/v1alpha1 Rollout, mirroring what
// the dynamic client returns from the API server.
func rolloutObj(name string, replicas, ready int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"replicas": replicas,
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": name},
			},
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": name},
				},
			},
		},
		"status": map[string]interface{}{
			"readyReplicas": ready,
		},
	}}
}

func fakeDynamic(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutGVRToListKind, objs...)
}

func defaultNamespace() []*v1.Namespace {
	return []*v1.Namespace{{ObjectMeta: metav1.ObjectMeta{Name: "default"}}}
}

func newController(t *testing.T, dynamicClient dynamic.Interface, deployments []*appsv1.Deployment) *PDBController {
	t.Helper()
	return NewPDBController(
		0,
		setupMockKubernetes(t, nil, deployments, nil, defaultNamespace()),
		dynamicClient,
		"pdb-controller",
		0,
		false,
		testMaxUnavailable,
	)
}

// A healthy multi-replica Rollout with no matching PDB gets one created, with an
// ownerReference pointing back at the Rollout.
func TestRolloutGetsPDB(t *testing.T) {
	controller := newController(t, fakeDynamic(rolloutObj("rollout-x", 2, 2)), nil)

	require.NoError(t, controller.runOnce(context.Background()))

	pdb, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").
		Get(context.Background(), "rollout-x-pdb-controller", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, testMaxUnavailable, *pdb.Spec.MaxUnavailable)
	require.Equal(t, map[string]string{"app": "rollout-x"}, pdb.Spec.Selector.MatchLabels)
	require.Len(t, pdb.OwnerReferences, 1)
	require.Equal(t, "Rollout", pdb.OwnerReferences[0].Kind)
	require.Equal(t, "argoproj.io/v1alpha1", pdb.OwnerReferences[0].APIVersion)
}

// A single-replica Rollout must NOT get a PDB (same rule as Deployments).
func TestSingleReplicaRolloutGetsNoPDB(t *testing.T) {
	controller := newController(t, fakeDynamic(rolloutObj("rollout-solo", 1, 1)), nil)

	require.NoError(t, controller.runOnce(context.Background()))

	_, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").
		Get(context.Background(), "rollout-solo-pdb-controller", metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err), "expected no PDB for a single-replica Rollout")
}

// Supersafe: when Rollout support is disabled (nil dynamic client) the loop runs
// normally and never touches Rollouts.
func TestRolloutsDisabledByDefault(t *testing.T) {
	controller := newController(t, nil, nil)
	require.NoError(t, controller.runOnce(context.Background()))
}

// Supersafe: a missing Rollout CRD (API server returns NotFound) must not break
// the reconcile — the Deployment still gets its PDB.
func TestRolloutCRDMissingDoesNotBreakReconcile(t *testing.T) {
	dynamicClient := fakeDynamic()
	dynamicClient.PrependReactor("list", "rollouts", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(rolloutGVR.GroupResource(), "")
	})

	controller := newController(t, dynamicClient, []*appsv1.Deployment{makeDeployment("deployment-x", map[string]string{"app": "deployment-x"}, 2, 2, "")})

	require.NoError(t, controller.runOnce(context.Background()))

	_, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").
		Get(context.Background(), "deployment-x-pdb-controller", metav1.GetOptions{})
	require.NoError(t, err, "Deployment PDB must still be created when the Rollout CRD is absent")
}

// Supersafe: lacking RBAC for rollouts (Forbidden) must not break the reconcile.
func TestRolloutForbiddenDoesNotBreakReconcile(t *testing.T) {
	dynamicClient := fakeDynamic()
	dynamicClient.PrependReactor("list", "rollouts", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(rolloutGVR.GroupResource(), "", nil)
	})

	controller := newController(t, dynamicClient, []*appsv1.Deployment{makeDeployment("deployment-x", map[string]string{"app": "deployment-x"}, 2, 2, "")})

	require.NoError(t, controller.runOnce(context.Background()))

	_, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").
		Get(context.Background(), "deployment-x-pdb-controller", metav1.GetOptions{})
	require.NoError(t, err, "Deployment PDB must still be created when rollouts RBAC is missing")
}

// Supersafe: any other (transient) list error must not break the reconcile.
func TestRolloutGenericErrorDoesNotBreakReconcile(t *testing.T) {
	dynamicClient := fakeDynamic()
	dynamicClient.PrependReactor("list", "rollouts", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("boom")
	})

	controller := newController(t, dynamicClient, []*appsv1.Deployment{makeDeployment("deployment-x", map[string]string{"app": "deployment-x"}, 2, 2, "")})

	require.NoError(t, controller.runOnce(context.Background()))

	_, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").
		Get(context.Background(), "deployment-x-pdb-controller", metav1.GetOptions{})
	require.NoError(t, err, "Deployment PDB must still be created when the Rollout list errors")
}

// Direct coverage of the rollout kubeResource accessors, including the
// replicas-unset default and annotations (only otherwise hit on the
// non-ready-ttl path).
func TestRolloutResourceAccessors(t *testing.T) {
	reps := int32(3)
	r := rollout{argoRollout{
		TypeMeta: metav1.TypeMeta{Kind: "Rollout", APIVersion: "argoproj.io/v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "r",
			Namespace:   "ns",
			UID:         types.UID("uid-1"),
			Labels:      map[string]string{"l": "1"},
			Annotations: map[string]string{"a": "1"},
		},
	}}
	r.Spec.Replicas = &reps
	r.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "r"}}
	r.Spec.Template.Metadata.Labels = map[string]string{"app": "r"}
	r.Status.ReadyReplicas = 3

	require.Equal(t, "argoproj.io/v1alpha1", r.APIVersion())
	require.Equal(t, "Rollout", r.Kind())
	require.Equal(t, "r", r.Name())
	require.Equal(t, "ns", r.Namespace())
	require.Equal(t, types.UID("uid-1"), r.UID())
	require.Equal(t, map[string]string{"a": "1"}, r.Annotations())
	require.Equal(t, map[string]string{"l": "1"}, r.Labels())
	require.Equal(t, map[string]string{"app": "r"}, r.TemplateLabels())
	require.Equal(t, int32(3), r.Replicas())
	require.Equal(t, int32(3), r.StatusReadyReplicas())
	require.Equal(t, map[string]string{"app": "r"}, r.Selector().MatchLabels)

	// replicas unset defaults to 1, same as Deployments/StatefulSets
	r.Spec.Replicas = nil
	require.Equal(t, int32(1), r.Replicas())
}
