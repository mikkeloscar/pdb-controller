package main

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	pv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	heritageLabel = "heritage"
	pdbController = "pdb-controller"
)

var (
	ownerLabels = map[string]string{heritageLabel: pdbController}
)

// PDBController creates PodDisruptionBudgets for deployments and StatefulSets
// if missing.
type PDBController struct {
	kubernetes.Interface
	interval      time.Duration
	pdbNameSuffix string
	nonReadyTTL   time.Duration
}

// NewPDBController initializes a new PDBController.
func NewPDBController(interval time.Duration, client kubernetes.Interface, pdbNameSuffix string, nonReadyTTL time.Duration) (*PDBController, error) {
	controller := &PDBController{
		Interface:     client,
		interval:      interval,
		pdbNameSuffix: pdbNameSuffix,
		nonReadyTTL:   nonReadyTTL,
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

	deployments, err := n.AppsV1().Deployments(namespace.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	statefulSets, err := n.AppsV1().StatefulSets(namespace.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	addPDB := make([]interface{}, 0, len(deployments.Items)+len(statefulSets.Items))
	removePDB := make([]pv1beta1.PodDisruptionBudget, 0, len(deployments.Items)+len(statefulSets.Items))

	nonReadTTL := time.Now().UTC().Add(-n.nonReadyTTL)

	for _, deployment := range deployments.Items {
		matchedPDBs := getPDBs(deployment.Spec.Template.Labels, pdbs.Items, nil)

		// if no PDB exist for the deployment and all replicas are
		// ready, add one
		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			if len(matchedPDBs) == 0 && *deployment.Spec.Replicas > 1 {
				addPDB = append(addPDB, deployment)
			}
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
			continue
		}

		// remove PDBs that are no longer valid, they'll be recreated on the next iteration
		for _, pdb := range ownedPDBs {
			if !pdbSpecValid(pdb) {
				removePDB = append(removePDB, pdb)
			}
		}

		// if nonReadyTTL is enabled and not all replicas of the
		// deployment is ready then we check when the pods were last
		// ready. This is done to ensure we don't keep PDBs for broken
		// deployments which would block cluster operations for no
		// reason (Disrupting a broken deployment doesn't matter).
		if n.nonReadyTTL > 0 && len(ownedPDBs) > 0 && deployment.Status.ReadyReplicas < *deployment.Spec.Replicas {
			lastTransitionTime, err := n.getPodsLastTransitionTime(namespace.Name, deployment.Spec.Selector.MatchLabels)
			if err != nil {
				log.Errorf("Failed to get pod lastTransitionTime: %s", err)
				continue
			}

			if !lastTransitionTime.IsZero() && lastTransitionTime.Before(nonReadTTL) {
				removePDB = append(removePDB, ownedPDBs...)
			}
		}
	}

	for _, statefulSet := range statefulSets.Items {
		// only add PDB for statefulsets where all replicas are ready
		if statefulSet.Status.ReadyReplicas < statefulSet.Status.Replicas {
			continue
		}

		matchedPDBs := getPDBs(statefulSet.Spec.Template.Labels, pdbs.Items, nil)

		// if no PDB exist for the statefulset and all replicas are
		// ready, add one
		if statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas {
			if len(matchedPDBs) == 0 && *statefulSet.Spec.Replicas > 1 {
				addPDB = append(addPDB, statefulSet)
			}
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
			continue
		}

		// remove PDBs that are no longer valid, they'll be recreated on the next iteration
		for _, pdb := range ownedPDBs {
			if !pdbSpecValid(pdb) {
				removePDB = append(removePDB, pdb)
			}
		}

		// if nonReadyTTL is enabled and not all replicas of the
		// statefulset is ready then we check when the pods were last
		// ready. This is done to ensure we don't keep PDBs for broken
		// statefulsets which would block cluster operations for no
		// reason (Disrupting a broken statefulset doesn't matter).
		if n.nonReadyTTL > 0 && len(ownedPDBs) > 0 && statefulSet.Status.ReadyReplicas < *statefulSet.Spec.Replicas {
			lastTransitionTime, err := n.getPodsLastTransitionTime(namespace.Name, statefulSet.Spec.Selector.MatchLabels)
			if err != nil {
				log.Errorf("Failed to get pod lastTransitionTime: %s", err)
				continue
			}

			if !lastTransitionTime.IsZero() && lastTransitionTime.Before(nonReadTTL) {
				removePDB = append(removePDB, ownedPDBs...)
			}
		}
	}

	// add missing PDBs
	for _, resource := range addPDB {
		maxUnavailable := intstr.FromInt(1)
		pdb := &pv1beta1.PodDisruptionBudget{
			Spec: pv1beta1.PodDisruptionBudgetSpec{
				MaxUnavailable: &maxUnavailable,
			},
		}

		switch r := resource.(type) {
		case appsv1.Deployment:
			if r.Labels == nil {
				r.Labels = make(map[string]string)
			}
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       r.Name,
					UID:        r.UID,
				},
			}
			pdb.Labels = labels
			pdb.Spec.Selector = r.Spec.Selector
		case appsv1.StatefulSet:
			if r.Labels == nil {
				r.Labels = make(map[string]string)
			}
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       r.Name,
					UID:        r.UID,
				},
			}
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

// getPodsLastTransitionTime returns the latest transition time for the pod not
// ready condition of all pods matched by the selector.
func (n *PDBController) getPodsLastTransitionTime(namespace string, selector map[string]string) (time.Time, error) {
	opts := metav1.ListOptions{
		LabelSelector: labels.Set(selector).String(),
	}
	pods, err := n.CoreV1().Pods(namespace).List(opts)
	if err != nil {
		return time.Time{}, err
	}

	lastTransitionTime := time.Time{}

	for _, pod := range pods.Items {
		for _, cond := range pod.Status.Conditions {
			if cond.Type != v1.PodReady {
				continue
			}

			if cond.Status == v1.ConditionTrue {
				break
			}

			if cond.LastTransitionTime.After(lastTransitionTime) {
				lastTransitionTime = cond.LastTransitionTime.Time
			}
			break
		}
	}

	return lastTransitionTime, nil
}

// pdbSpecValid returns true if the PDB spec is up-to-date
func pdbSpecValid(pdb pv1beta1.PodDisruptionBudget) bool {
	return pdb.Spec.MinAvailable == nil
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
