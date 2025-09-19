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
