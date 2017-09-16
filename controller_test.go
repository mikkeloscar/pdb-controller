package main

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pv1beta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
)

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
