package main

import (
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type kubeResource interface {
	APIVersion() string
	Kind() string
	Name() string
	UID() types.UID
	Annotations() map[string]string
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

func (s statefulSet) UID() types.UID {
	return s.StatefulSet.UID
}

func (s statefulSet) Annotations() map[string]string {
	return s.StatefulSet.Annotations
}

func (s statefulSet) TemplateLabels() map[string]string {
	return s.StatefulSet.Spec.Template.Labels
}

func (s statefulSet) Replicas() int32 {
	if s.StatefulSet.Spec.Replicas == nil {
		return 1
	}
	return *s.StatefulSet.Spec.Replicas
}

func (s statefulSet) StatusReadyReplicas() int32 {
	return s.StatefulSet.Status.ReadyReplicas
}

func (s statefulSet) Selector() *metav1.LabelSelector {
	return s.StatefulSet.Spec.Selector
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

func (d deployment) UID() types.UID {
	return d.Deployment.UID
}

func (d deployment) Annotations() map[string]string {
	return d.Deployment.Annotations
}

func (d deployment) TemplateLabels() map[string]string {
	return d.Deployment.Spec.Template.Labels
}

func (d deployment) Replicas() int32 {
	if d.Deployment.Spec.Replicas == nil {
		return 1
	}
	return *d.Deployment.Spec.Replicas
}

func (d deployment) StatusReadyReplicas() int32 {
	return d.Deployment.Status.ReadyReplicas
}

func (d deployment) Selector() *metav1.LabelSelector {
	return d.Deployment.Spec.Selector
}
