package main

import (
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
	interval time.Duration
}

// NewPDBController initializes a new PDBController.
func NewPDBController(interval time.Duration) (*PDBController, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	controller := &PDBController{
		Interface: client,
		interval:  interval,
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
	removePDB := make([]*pv1beta1.PodDisruptionBudget, 0, len(deployments.Items)+len(statefulSets.Items))

	for _, deployment := range deployments.Items {
		if pdb := getPDB(deployment.Spec.Template.Labels, pdbs.Items, nil); pdb == nil && *deployment.Spec.Replicas > 1 {
			addPDB = append(addPDB, deployment)
		}
		// for resources with only one replica, check if we previously
		// created a pdb and remove it.
		if pdb := getPDB(deployment.Spec.Template.Labels, pdbs.Items, ownerLabels); pdb != nil && *deployment.Spec.Replicas <= 1 {
			removePDB = append(removePDB, pdb)
		}
	}

	for _, statefulSet := range statefulSets.Items {
		if pdb := getPDB(statefulSet.Spec.Template.Labels, pdbs.Items, nil); pdb == nil && *statefulSet.Spec.Replicas > 1 {
			addPDB = append(addPDB, statefulSet)
		}
		// for resources with only one or less replicas, check if we
		// previously created a PDB, and remove it.
		if pdb := getPDB(statefulSet.Spec.Template.Labels, pdbs.Items, ownerLabels); pdb != nil && *statefulSet.Spec.Replicas <= 1 {
			removePDB = append(removePDB, pdb)
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
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.Labels = labels
			pdb.Spec.Selector = r.Spec.Selector
		case v1beta1.StatefulSet:
			labels := r.Labels
			labels[heritageLabel] = pdbController
			pdb.Name = r.Name
			pdb.Namespace = r.Namespace
			pdb.Labels = labels
			pdb.Spec.Selector = r.Spec.Selector
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

// getPDB gets matching PodDisruptionBudget.
func getPDB(labels map[string]string, pdbs []pv1beta1.PodDisruptionBudget, selector map[string]string) *pv1beta1.PodDisruptionBudget {
	for _, pdb := range pdbs {
		if containLabels(labels, pdb.Spec.Selector.MatchLabels) && containLabels(pdb.Labels, selector) {
			return &pdb
		}
	}
	return nil
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
