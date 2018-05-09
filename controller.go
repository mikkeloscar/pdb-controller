package main

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	pv1beta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
	"k8s.io/client-go/rest"
)

const (
	heritageLabel = "heritage"
	pdbController = "pdb-controller"
)

var (
	ownerLabels = map[string]string{heritageLabel: pdbController}
)

// PDBController creates PodDistruptionBudgets for deployments and StatefulSets
// if missing.
type PDBController struct {
	kubernetes.Interface
	interval      time.Duration
	pdbNameSuffix string
}

// NewPDBController initializes a new PDBController.
func NewPDBController(interval time.Duration, config *rest.Config, pdbNameSuffix string) (*PDBController, error) {
	var err error
	if config == nil {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	controller := &PDBController{
		Interface:     client,
		interval:      interval,
		pdbNameSuffix: pdbNameSuffix,
	}

	return controller, nil
}

func (n *PDBController) runOnce() error {
	namespaces, err := n.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, ns := range namespaces.Items {
		err = n.addPDBs(&ns)
		if err != nil {
			log.Error(err)
			continue
		}
	}

	return nil
}

// Run runs the controller loop until it receives a stop signal over the stop
// channel.
func (n *PDBController) Run(stopChan <-chan struct{}) {
	for {
		log.Debug("Running main control loop.")
		err := n.runOnce()
		if err != nil {
			log.Error(err)
		}

		select {
		case <-time.After(n.interval):
		case <-stopChan:
			log.Info("Terminating main controller loop.")
			return
		}
	}
}

// addPDBs adds PodDisruptionBudgets for deployments and statefulsets in a
// given namespace. A PodDisruptionBudget is only added if there is none
// defined already for the deployment or statefulset.
func (n *PDBController) addPDBs(namespace *v1.Namespace) error {
	pdbs, err := n.PolicyV1beta1().PodDisruptionBudgets(namespace.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	deployments, err := n.AppsV1beta1().Deployments(namespace.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	statefulSets, err := n.AppsV1beta1().StatefulSets(namespace.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	addPDB := make([]interface{}, 0, len(deployments.Items)+len(statefulSets.Items))
	removePDB := make([]pv1beta1.PodDisruptionBudget, 0, len(deployments.Items)+len(statefulSets.Items))

	for _, deployment := range deployments.Items {
		matchedPDBs := getPDBs(deployment.Spec.Template.Labels, pdbs.Items, nil)

		// if no PDB exist for the deployment, add one
		if len(matchedPDBs) == 0 && *deployment.Spec.Replicas > 1 {
			addPDB = append(addPDB, deployment)
		}

		// for resources with only one replica, check if we previously
		// created PDBs and remove them.
		ownedPDBs := getPDBs(deployment.Spec.Template.Labels, matchedPDBs, ownerLabels)

		if len(ownedPDBs) > 0 && *deployment.Spec.Replicas <= 1 {
			removePDB = append(removePDB, ownedPDBs...)
			continue
		}

		// if there are owned PDBs and a non-owned PDBs remove the
		// owned PDBs to not shadow what's defined by users.
		if len(ownedPDBs) != len(matchedPDBs) {
			removePDB = append(removePDB, ownedPDBs...)
		}
	}

	for _, statefulSet := range statefulSets.Items {
		matchedPDBs := getPDBs(statefulSet.Spec.Template.Labels, pdbs.Items, nil)

		// if no PDB exist for the statefulset, add one
		if len(matchedPDBs) == 0 && *statefulSet.Spec.Replicas > 1 {
			addPDB = append(addPDB, statefulSet)
		}

		// for resources with only one replica, check if we previously
		// created PDBs and remove them.
		ownedPDBs := getPDBs(statefulSet.Spec.Template.Labels, matchedPDBs, ownerLabels)

		if len(ownedPDBs) > 0 && *statefulSet.Spec.Replicas <= 1 {
			removePDB = append(removePDB, ownedPDBs...)
			continue
		}

		// if there are owned PDBs and a non-owned PDBs remove the
		// owned PDBs to not shadow what's defined by users.
		if len(ownedPDBs) != len(matchedPDBs) {
			removePDB = append(removePDB, ownedPDBs...)
		}
	}

	// add missing PDBs
	for _, resource := range addPDB {
		minAvailable := intstr.FromInt(1)
		pdb := &pv1beta1.PodDisruptionBudget{
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				MinAvailable: &minAvailable,
			},
		}

		switch r := resource.(type) {
		case v1beta1.Deployment:
			if r.Labels == nil {
				r.Labels = make(map[string]string)
			}
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.Labels = labels
			pdb.Spec.Selector = r.Spec.Selector
		case v1beta1.StatefulSet:
			if r.Labels == nil {
				r.Labels = make(map[string]string)
			}
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.Labels = labels
			pdb.Spec.Selector = r.Spec.Selector
		}

		if n.pdbNameSuffix != "" {
			pdb.Name = fmt.Sprintf("%s-%s", pdb.Name, n.pdbNameSuffix)
		}

		_, err := n.PolicyV1beta1().PodDisruptionBudgets(pdb.Namespace).Create(pdb)
		if err != nil {
			log.Error(err)
			continue
		}

		log.WithFields(log.Fields{
			"action":    "added",
			"pdb":       pdb.Name,
			"namespace": pdb.Namespace,
			"selector":  pdb.Spec.Selector.String(),
		}).Info("")
	}

	// remove obsolete PDBs
	for _, pdb := range removePDB {
		err := n.PolicyV1beta1().PodDisruptionBudgets(pdb.Namespace).Delete(pdb.Name, nil)
		if err != nil {
			log.Error(err)
			continue
		}

		log.WithFields(log.Fields{
			"action":    "removed",
			"pdb":       pdb.Name,
			"namespace": pdb.Namespace,
			"selector":  pdb.Spec.Selector.String(),
		}).Info("")
	}

	return nil
}

// getPDBs gets matching PodDisruptionBudgets.
func getPDBs(labels map[string]string, pdbs []pv1beta1.PodDisruptionBudget, selector map[string]string) []pv1beta1.PodDisruptionBudget {
	matchedPDBs := make([]pv1beta1.PodDisruptionBudget, 0)
	for _, pdb := range pdbs {
		if labelsIntersect(labels, pdb.Spec.Selector.MatchLabels) && containLabels(pdb.Labels, selector) {
			matchedPDBs = append(matchedPDBs, pdb)
		}
	}
	return matchedPDBs
}

// containLabels reports whether expectedLabels are in labels.
func containLabels(labels, expectedLabels map[string]string) bool {
	for key, val := range expectedLabels {
		if v, ok := labels[key]; !ok || v != val {
			return false
		}
	}
	return true
}

// labelsIntersect checks whether two maps a and b intersects. Intersection is
// defined as at least one identical key value pair must exist in both maps and
// there must be no keys which match where the values doesn't match.
func labelsIntersect(a, b map[string]string) bool {
	intersect := false
	for key, val := range a {
		v, ok := b[key]
		if ok {
			if v == val {
				intersect = true
			} else { // if the key exists but the values doesn't match, don't consider it an intersection
				return false
			}
		}
	}

	return intersect
}
