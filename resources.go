package main

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// rolloutGVR is the argoproj.io/v1alpha1 Rollout resource. Argo Rollouts is
// optional, so it's queried via the dynamic client and a missing CRD is handled
// gracefully (see PDBController.listRollouts).
var rolloutGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "rollouts",
}

type kubeResource interface {
	APIVersion() string
	Kind() string
	Name() string
	Namespace() string
	UID() types.UID
	Annotations() map[string]string
	Labels() map[string]string
	TemplateLabels() map[string]string
	Replicas() int32
	StatusReadyReplicas() int32
	Selector() *metav1.LabelSelector
}

type statefulSet struct {
	appsv1.StatefulSet
}

func (s statefulSet) APIVersion() string {
	return s.StatefulSet.APIVersion
}

func (s statefulSet) Kind() string {
	return s.StatefulSet.Kind
}

func (s statefulSet) Name() string {
	return s.StatefulSet.Name
}

func (s statefulSet) Namespace() string {
	return s.StatefulSet.Namespace
}

func (s statefulSet) UID() types.UID {
	return s.StatefulSet.UID
}

func (s statefulSet) Annotations() map[string]string {
	return s.StatefulSet.Annotations
}

func (s statefulSet) Labels() map[string]string {
	return s.StatefulSet.Labels
}

func (s statefulSet) TemplateLabels() map[string]string {
	return s.Spec.Template.Labels
}

func (s statefulSet) Replicas() int32 {
	if s.Spec.Replicas == nil {
		return 1
	}
	return *s.Spec.Replicas
}

func (s statefulSet) StatusReadyReplicas() int32 {
	return s.Status.ReadyReplicas
}

func (s statefulSet) Selector() *metav1.LabelSelector {
	return s.Spec.Selector
}

type deployment struct {
	appsv1.Deployment
}

func (d deployment) APIVersion() string {
	return d.Deployment.APIVersion
}

func (d deployment) Kind() string {
	return d.Deployment.Kind
}

func (d deployment) Name() string {
	return d.Deployment.Name
}

func (d deployment) Namespace() string {
	return d.Deployment.Namespace
}

func (d deployment) UID() types.UID {
	return d.Deployment.UID
}

func (d deployment) Annotations() map[string]string {
	return d.Deployment.Annotations
}

func (d deployment) Labels() map[string]string {
	return d.Deployment.Labels
}

func (d deployment) TemplateLabels() map[string]string {
	return d.Spec.Template.Labels
}

func (d deployment) Replicas() int32 {
	if d.Spec.Replicas == nil {
		return 1
	}
	return *d.Spec.Replicas
}

func (d deployment) StatusReadyReplicas() int32 {
	return d.Status.ReadyReplicas
}

func (d deployment) Selector() *metav1.LabelSelector {
	return d.Spec.Selector
}

// argoRollout is a minimal typed projection of an argoproj.io/v1alpha1 Rollout —
// only the fields kubeResource needs. Rollout is a Deployment-like resource, so
// spec.replicas/selector/template and status.readyReplicas mirror the Deployment
// shapes. It's decoded from the dynamic client's unstructured payload so the
// controller doesn't depend on the argo-rollouts module just to read these.
type argoRollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              struct {
		Replicas *int32                `json:"replicas,omitempty"`
		Selector *metav1.LabelSelector `json:"selector,omitempty"`
		Template struct {
			Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
		} `json:"template,omitempty"`
	} `json:"spec,omitempty"`
	Status struct {
		ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	} `json:"status,omitempty"`
}

type rollout struct {
	argoRollout
}

func (r rollout) APIVersion() string {
	return r.argoRollout.APIVersion
}

func (r rollout) Kind() string {
	return r.argoRollout.Kind
}

func (r rollout) Name() string {
	return r.argoRollout.Name
}

func (r rollout) Namespace() string {
	return r.argoRollout.Namespace
}

func (r rollout) UID() types.UID {
	return r.argoRollout.UID
}

func (r rollout) Annotations() map[string]string {
	return r.argoRollout.Annotations
}

func (r rollout) Labels() map[string]string {
	return r.argoRollout.Labels
}

func (r rollout) TemplateLabels() map[string]string {
	return r.Spec.Template.Metadata.Labels
}

func (r rollout) Replicas() int32 {
	if r.Spec.Replicas == nil {
		return 1
	}
	return *r.Spec.Replicas
}

func (r rollout) StatusReadyReplicas() int32 {
	return r.Status.ReadyReplicas
}

func (r rollout) Selector() *metav1.LabelSelector {
	return r.Spec.Selector
}
