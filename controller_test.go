package main

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	pv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func setupMockKubernetes(t *testing.T, pdbs []*pv1beta1.PodDisruptionBudget, deployments []*appsv1.Deployment, statefulSets []*appsv1.StatefulSet, namespaces []*v1.Namespace) kubernetes.Interface {
	client := fake.NewSimpleClientset()

	for _, pdb := range pdbs {
		_, err := client.PolicyV1beta1().PodDisruptionBudgets(namespaces[0].Name).Create(pdb)
		if err != nil {
			t.Error(err)
		}
	}

	for _, depl := range deployments {
		_, err := client.AppsV1().Deployments(namespaces[0].Name).Create(depl)
		if err != nil {
			t.Error(err)
		}
	}

	for _, statefulSet := range statefulSets {
		_, err := client.AppsV1().StatefulSets(namespaces[0].Name).Create(statefulSet)
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

func TestRemoveInvalidPDBs(t *testing.T) {
	deplabels := map[string]string{"foo": "deployment"}
	sslabels := map[string]string{"foo": "statefulset"}
	replicas := int32(2)

	one := intstr.FromInt(1)
	pdbs := []*pv1beta1.PodDisruptionBudget{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pdb-1",
				Labels: ownerLabels,
			},
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: deplabels,
				},
				MinAvailable: &one,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "pdb-2",
				Labels: ownerLabels,
			},
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: sslabels,
				},
				MinAvailable: &one,
			},
		},
	}

	deployments := []*appsv1.Deployment{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "deployment-1",
				Labels: deplabels,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: deplabels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: deplabels,
					},
				},
			},
		},
	}

	statefulSets := []*appsv1.StatefulSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "stateful-set-1",
				Labels: sslabels,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: sslabels,
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: sslabels,
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

	for _, pdb := range []string{"pdb-1", "pdb-2"} {
		pdbResource, err := controller.Interface.PolicyV1beta1().PodDisruptionBudgets("default").Get(pdb, metav1.GetOptions{})
		if err == nil {
			t.Fatalf("unexpected pdb (%s) found: %v", pdb, pdbResource)
		}
		if !errors.IsNotFound(err) {
			t.Fatalf("unexpected error: %s", err)
		}
	}
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

	deployments := []*appsv1.Deployment{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "deployment-1",
				Labels: labels,
			},
			Spec: appsv1.DeploymentSpec{
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
			Spec: appsv1.DeploymentSpec{
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

	statefulSets := []*appsv1.StatefulSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "stateful-set-1",
				Labels: labels,
			},
			Spec: appsv1.StatefulSetSpec{
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
			Spec: appsv1.StatefulSetSpec{
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

func TestGetPDBs(t *testing.T) {
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

	matchedPDBs := getPDBs(labels, pdbs, nil)
	if len(matchedPDBs) == 0 {
		t.Errorf("expected to get matching PDB")
	}

	matchedPDBs = getPDBs(labels, pdbs, labels)
	if len(matchedPDBs) == 0 {
		t.Errorf("expected to get matching PDB")
	}

	matchedPDBs = getPDBs(nil, pdbs, labels)
	if len(matchedPDBs) != 0 {
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
