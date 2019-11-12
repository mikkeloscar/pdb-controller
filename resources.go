package main

import appsv1 "k8s.io/api/apps/v1"

type kubeResource interface {
	Annotations() map[string]string
	TemplateLabels() map[string]string
	Replicas() int32
	StatusReadyReplicas() int32
}

type statefulSet struct {
	appsv1.StatefulSet
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

type deployment struct {
	appsv1.Deployment
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
