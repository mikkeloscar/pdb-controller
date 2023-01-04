package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	pv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	testMaxUnavailable          = intstr.Parse("1")
	testMaxUnavailableDifferent = intstr.Parse("2")
)

func setupMockKubernetes(t *testing.T, pdbs []*pv1.PodDisruptionBudget, deployments []*appsv1.Deployment, statefulSets []*appsv1.StatefulSet, namespaces []*v1.Namespace) kubernetes.Interface {
	client := fake.NewSimpleClientset()

	if len(namespaces) == 0 {
		t.Error("Cannot create mock client with no namespaces")
	}

	for _, namespace := range namespaces {
		_, err := client.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
		if err != nil {
			t.Error(err)
		}
	}

	for _, pdb := range pdbs {
		_, err := client.PolicyV1().PodDisruptionBudgets(namespaces[0].Name).Create(context.Background(), pdb, metav1.CreateOptions{})
		if err != nil {
			t.Error(err)
		}
	}

	for _, depl := range deployments {
		_, err := client.AppsV1().Deployments(namespaces[0].Name).Create(context.Background(), depl, metav1.CreateOptions{})
		if err != nil {
			t.Error(err)
		}
	}

	for _, statefulSet := range statefulSets {
		_, err := client.AppsV1().StatefulSets(namespaces[0].Name).Create(context.Background(), statefulSet, metav1.CreateOptions{})
		if err != nil {
			t.Error(err)
		}
	}

	return client
}

func TestRunOnce(t *testing.T) {
	namespaces := []*v1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
	}

	controller := &PDBController{
		Interface: setupMockKubernetes(t, nil, nil, nil, namespaces),
	}

	err := controller.runOnce(context.Background())
	if err != nil {
		t.Error(err)
	}
}

func TestRun(t *testing.T) {
	namespaces := []*v1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
	}

	controller := &PDBController{
		Interface: setupMockKubernetes(t, nil, nil, nil, namespaces),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go controller.Run(ctx)
	cancel()
}

func TestContainLabels(t *testing.T) {
	labels := map[string]string{
		"foo": "bar",
	}

	expected := map[string]string{
		"foo": "bar",
	}

	if !containLabels(labels, expected) {
		t.Errorf("expected %s to be contained in %s", expected, labels)
	}

	notExpected := map[string]string{
		"foo": "baz",
	}

	if containLabels(labels, notExpected) {
		t.Errorf("did not expect %s to be contained in %s", notExpected, labels)
	}
}

func TestLabelsIntersect(tt *testing.T) {
	for _, tc := range []struct {
		msg       string
		a         map[string]string
		b         map[string]string
		intersect bool
	}{
		{
			msg: "matching maps should intersect",
			a: map[string]string{
				"foo": "bar",
			},
			b: map[string]string{
				"foo": "bar",
			},
			intersect: true,
		},
		{
			msg: "partly matching maps should intersect",
			a: map[string]string{
				"foo": "bar",
			},
			b: map[string]string{
				"foo": "bar",
				"bar": "foo",
			},
			intersect: true,
		},
		{
			msg: "maps with matching keys but different values should not inersect",
			a: map[string]string{
				"foo": "bar",
				"bar": "baz",
			},
			b: map[string]string{
				"foo": "bar",
				"bar": "foo",
			},
			intersect: false,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			if labelsIntersect(tc.a, tc.b) != tc.intersect {
				t.Errorf("expected intersection to be %t, was %t", tc.intersect, labelsIntersect(tc.a, tc.b))
			}
		})
	}
}

func makePDB(name string, selector map[string]string, ownerReferences []metav1.OwnerReference, maxUnavailable *intstr.IntOrString, lastReadyTime time.Duration) *pv1.PodDisruptionBudget {
	pdb := &pv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: make(map[string]string),
			UID:         types.UID(name),
		},
		Spec: pv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			MaxUnavailable: maxUnavailable,
		},
	}
	if len(ownerReferences) > 0 {
		pdb.Labels = ownerLabels
		pdb.OwnerReferences = ownerReferences
	}
	if lastReadyTime > 0 {
		pdb.Annotations[nonReadySinceAnnotationName] = time.Now().Add(-lastReadyTime).Format(time.RFC3339)
	}
	return pdb
}

func makeDeployment(name string, selector map[string]string, replicas, readyReplicas int32, nonReadyTTL string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			UID:         types.UID("deployment-uid-" + name),
			Annotations: make(map[string]string),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Replicas: &replicas,
			Template: v1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: readyReplicas,
		},
	}
	if nonReadyTTL != "" {
		deployment.Annotations[nonReadyTTLAnnotationName] = nonReadyTTL
	}
	return deployment
}

func makeStatefulset(name string, selector map[string]string, replicas, readyReplicas int32, nonReadyTTL string) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			UID:         types.UID("statefulset-uid-" + name),
			Annotations: make(map[string]string),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Replicas: &replicas,
			Template: v1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: selector}},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas: readyReplicas,
		},
	}
	if nonReadyTTL != "" {
		sts.Annotations[nonReadyTTLAnnotationName] = nonReadyTTL
	}
	return sts
}

func TestController(tt *testing.T) {
	deployHash, err := resourceHash("Deployment", "deployment-x")
	require.NoError(tt, err)
	statefulHash, err := resourceHash("StatefulSet", "statefulset-x")
	require.NoError(tt, err)
	for _, tc := range []struct {
		msg                    string
		replicas               int32
		readyReplicas          int32
		nonReadyTTL            string
		lastReadyTime          time.Duration
		maxUnavailable         *intstr.IntOrString
		pdbExists              bool
		pdbDeploymentSelector  map[string]string
		pdbStatefulSetSelector map[string]string
		addtionalPDBs          []*pv1.PodDisruptionBudget
		overridePDBs           []*pv1.PodDisruptionBudget
		parentResourceHash     bool
	}{
		{
			msg:            "drop pdb when nonReadyTTL is exceeded",
			replicas:       3,
			readyReplicas:  0,
			nonReadyTTL:    "5s",
			lastReadyTime:  1 * time.Minute,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      false,
		},
		{
			msg:            "keep pdb when nonReadyTTL is not exceeded",
			replicas:       3,
			readyReplicas:  0,
			nonReadyTTL:    "15m",
			lastReadyTime:  5 * time.Minute,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      true,
		},
		{
			msg:            "drop pdb when default nonReadyTTL is exceeded",
			replicas:       3,
			readyReplicas:  0,
			nonReadyTTL:    "",
			lastReadyTime:  60 * time.Minute,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      false,
		},
		{
			msg:            "keep pdb when default nonReadyTTL is not exceeded",
			replicas:       3,
			readyReplicas:  0,
			nonReadyTTL:    "",
			lastReadyTime:  59 * time.Minute,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      true,
		},
		{
			msg:            "keep pdb when replicas are ready",
			replicas:       3,
			readyReplicas:  3,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      true,
		},
		{
			msg:            "keep pdb when first observed not-ready and TTL not exceeded",
			replicas:       3,
			readyReplicas:  0,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      true,
		},
		{
			msg:            "reset nonReadyTTL when replicas are ready",
			replicas:       3,
			readyReplicas:  3,
			nonReadyTTL:    "",
			lastReadyTime:  60 * time.Minute,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      true,
		},
		{
			msg:            "update owned PDB not matching",
			replicas:       3,
			readyReplicas:  3,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			pdbExists:      true,
			maxUnavailable: &testMaxUnavailable,
			pdbDeploymentSelector: map[string]string{
				"not-matching": "deployment",
			},
			pdbStatefulSetSelector: map[string]string{
				"not-matching": "statefulset",
			},
		},
		{
			msg:            "drop owned PDB if others are matching",
			replicas:       3,
			readyReplicas:  3,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			pdbExists:      false,
			maxUnavailable: &testMaxUnavailable,
			addtionalPDBs: []*pv1.PodDisruptionBudget{
				makePDB(
					"custom-deployment-pdb",
					map[string]string{
						"type":                  "deployment",
						parentResourceHashLabel: deployHash,
					},
					nil,
					&testMaxUnavailable,
					0,
				),
				makePDB(
					"custom-statefulset-pdb",
					map[string]string{
						"type":                  "statefulset",
						parentResourceHashLabel: statefulHash,
					},
					nil,
					&testMaxUnavailable,
					0,
				),
			},
		},
		{
			msg:            "drop owned PDB if less than 2 ready replicas",
			replicas:       1,
			readyReplicas:  1,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			maxUnavailable: &testMaxUnavailable,
			pdbExists:      false,
		},
		{
			msg:            "add PDB is none exists",
			replicas:       2,
			readyReplicas:  2,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			pdbExists:      true,
			maxUnavailable: &testMaxUnavailable,
			overridePDBs:   []*pv1.PodDisruptionBudget{},
		},
		{
			msg:            "update PDBs if settings are different",
			replicas:       2,
			readyReplicas:  2,
			nonReadyTTL:    "",
			lastReadyTime:  0,
			pdbExists:      true,
			maxUnavailable: &testMaxUnavailableDifferent,
		},
	} {
		tt.Run(tc.msg, func(t *testing.T) {
			// Run both with and without parent-resource-hash
			// support enabled.
			for _, parentResourceHash := range []bool{true, false} {
				deploymentSelector := map[string]string{"type": "deployment"}
				statefulSetSelector := map[string]string{"type": "statefulset"}

				if parentResourceHash {
					deploymentSelector = map[string]string{
						parentResourceHashLabel: deployHash,
					}

					statefulSetSelector = map[string]string{
						parentResourceHashLabel: statefulHash,
					}
				}

				pdbDeploymentSelector := deploymentSelector
				pdbStatefulSetSelector := statefulSetSelector
				if tc.pdbDeploymentSelector != nil {
					pdbDeploymentSelector = tc.pdbDeploymentSelector
				}
				if tc.pdbStatefulSetSelector != nil {
					pdbStatefulSetSelector = tc.pdbStatefulSetSelector
				}

				pdbs := []*pv1.PodDisruptionBudget{
					makePDB(
						"deployment-x-pdb-controller",
						pdbDeploymentSelector,
						[]metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "Deployment",
								Name:       "deployment-x",
								UID:        "deployment-uid-deployment-x",
							},
						},
						tc.maxUnavailable,
						tc.lastReadyTime,
					),
					makePDB(
						"statefulset-x-pdb-controller",
						pdbStatefulSetSelector,
						[]metav1.OwnerReference{
							{
								APIVersion: "apps/v1",
								Kind:       "StatefulSet",
								Name:       "statefulset-x",
								UID:        "statefulset-uid-statefulset-x",
							},
						},
						tc.maxUnavailable,
						tc.lastReadyTime,
					),
				}
				if len(tc.addtionalPDBs) > 0 {
					pdbs = append(pdbs, tc.addtionalPDBs...)
				}

				if tc.overridePDBs != nil {
					pdbs = tc.overridePDBs
				}

				deployments := []*appsv1.Deployment{
					makeDeployment(
						"deployment-x",
						deploymentSelector,
						tc.replicas,
						tc.readyReplicas,
						tc.nonReadyTTL,
					),
				}
				statefulSets := []*appsv1.StatefulSet{
					makeStatefulset(
						"statefulset-x",
						statefulSetSelector,
						tc.replicas,
						tc.readyReplicas,
						tc.nonReadyTTL,
					),
				}
				namespaces := []*v1.Namespace{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "default",
						},
					},
				}

				controller := NewPDBController(
					0,
					setupMockKubernetes(t, pdbs, deployments, statefulSets, namespaces),
					"pdb-controller",
					time.Hour,
					parentResourceHash,
					testMaxUnavailable,
				)

				err := controller.runOnce(context.Background())
				if err != nil {
					t.Errorf("controller failed to run: %s", err)
				}

				// deployment
				pdb, err := controller.Interface.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "deployment-x-pdb-controller", metav1.GetOptions{})
				if tc.pdbExists {
					require.NoError(t, err)
					require.Equal(t, pdb.Spec.Selector.MatchLabels, deploymentSelector)
					require.Equal(t, pv1.PodDisruptionBudgetSpec{
						Selector:       &metav1.LabelSelector{MatchLabels: deploymentSelector},
						MaxUnavailable: &testMaxUnavailable,
					}, pdb.Spec)
				} else {
					require.Error(t, err)
					require.True(t, errors.IsNotFound(err))
				}

				// statefulset
				pdb, err = controller.Interface.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "statefulset-x-pdb-controller", metav1.GetOptions{})
				if tc.pdbExists {
					require.NoError(t, err)
					require.Equal(t, pdb.Spec.Selector.MatchLabels, statefulSetSelector)
				} else {
					require.Error(t, err)
					require.True(t, errors.IsNotFound(err))
				}
			}
		})
	}
}
