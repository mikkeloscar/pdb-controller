package main

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	pv1beta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
)

func setupMockKubernetes(t *testing.T, pdbs []*pv1beta1.PodDisruptionBudget, deployments []*v1beta1.Deployment, statefulSets []*v1beta1.StatefulSet, namespaces []*v1.Namespace) kubernetes.Interface {
	client := fake.NewSimpleClientset()

	for _, pdb := range pdbs {
		_, err := client.PolicyV1beta1().PodDisruptionBudgets(namespaces[0].Name).Create(pdb)
		if err != nil {
			t.Error(err)
		}
	}

	for _, depl := range deployments {
		_, err := client.AppsV1beta1().Deployments(namespaces[0].Name).Create(depl)
		if err != nil {
			t.Error(err)
		}
	}

	for _, statefulSet := range statefulSets {
		_, err := client.AppsV1beta1().StatefulSets(namespaces[0].Name).Create(statefulSet)
		if err != nil {
			t.Error(err)
		}
	}

	for _, namespace := range namespaces {
		_, err := client.CoreV1().Namespaces().Create(namespace)
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

	err := controller.runOnce()
	if err != nil {
		t.Error(err)
	}
}

func TestRun(t *testing.T) {
	stopCh := make(chan struct{}, 1)
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

	go controller.Run(stopCh)
	stopCh <- struct{}{}
}

func TestAddPDBs(t *testing.T) {
	labels := map[string]string{"foo": "bar"}
	notFoundLabels := map[string]string{"bar": "foo"}
	pdbs := []*pv1beta1.PodDisruptionBudget{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pdb-1",
				Labels: ownerLabels,
			},
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		},
	}

	noReplicas := int32(0)
	replicas := int32(2)

	deployments := []*v1beta1.Deployment{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "deployment-1",
				Labels: labels,
			},
			Spec: v1beta1.DeploymentSpec{
				Replicas: &noReplicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "deployment-2",
				Labels: notFoundLabels,
			},
			Spec: v1beta1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: notFoundLabels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: notFoundLabels,
					},
				},
			},
		},
	}

	statefulSets := []*v1beta1.StatefulSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "stateful-set-1",
				Labels: labels,
			},
			Spec: v1beta1.StatefulSetSpec{
				Replicas: &noReplicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "stateful-set-2",
				Labels: labels,
			},
			Spec: v1beta1.StatefulSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: notFoundLabels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: notFoundLabels,
					},
				},
			},
		},
	}

	namespaces := []*v1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
	}

	controller := &PDBController{
		Interface: setupMockKubernetes(t, pdbs, deployments, statefulSets, namespaces),
	}

	err := controller.addPDBs(namespaces[0])
	if err != nil {
		t.Error(err)
	}
}

func TestGetPDB(t *testing.T) {
	labels := map[string]string{"k": "v"}
	pdbs := []pv1beta1.PodDisruptionBudget{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		},
	}

	pdb := getPDB(labels, pdbs, nil)
	if pdb == nil {
		t.Errorf("expected to get matching PDB")
	}

	pdb = getPDB(labels, pdbs, labels)
	if pdb == nil {
		t.Errorf("expected to get matching PDB")
	}

	pdb = getPDB(nil, pdbs, labels)
	if pdb != nil {
		t.Errorf("did not expect to find matching PDB")
	}
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
