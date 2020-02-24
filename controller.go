package main

import (
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	pv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	heritageLabel               = "heritage"
	pdbController               = "pdb-controller"
	nonReadyTTLAnnotationName   = "pdb-controller.zalando.org/non-ready-ttl"
	nonReadySinceAnnotationName = "pdb-controller.zalando.org/non-ready-since"
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
func NewPDBController(interval time.Duration, client kubernetes.Interface, pdbNameSuffix string, nonReadyTTL time.Duration) *PDBController {
	return &PDBController{
		Interface:     client,
		interval:      interval,
		pdbNameSuffix: pdbNameSuffix,
		nonReadyTTL:   nonReadyTTL,
	}
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

// runOnce runs the main reconcilation loop of the controller.
func (n *PDBController) runOnce() error {
	allPDBs, err := n.PolicyV1beta1().PodDisruptionBudgets(v1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	managedPDBs, unmanagedPDBs := filterPDBs(allPDBs.Items)

	deployments, err := n.AppsV1().Deployments(v1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	statefulSets, err := n.AppsV1().StatefulSets(v1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	resources := make([]kubeResource, 0, len(deployments.Items)+len(statefulSets.Items))

	for _, d := range deployments.Items {
		// manually set Kind and APIVersion because of a bug in
		// client-go
		// https://github.com/kubernetes/client-go/issues/308
		d.Kind = "Deployment"
		d.APIVersion = "apps/v1"
		resources = append(resources, deployment{d})
	}

	for _, s := range statefulSets.Items {
		// manually set Kind and APIVersion because of a bug in
		// client-go
		// https://github.com/kubernetes/client-go/issues/308
		s.Kind = "StatefulSet"
		s.APIVersion = "apps/v1"
		resources = append(resources, statefulSet{s})
	}

	desiredPDBs := n.generateDesiredPDBs(resources, managedPDBs, unmanagedPDBs)
	n.reconcilePDBs(desiredPDBs, managedPDBs)
	return nil
}

func (n *PDBController) generateDesiredPDBs(resources []kubeResource, managedPDBs, unmanagedPDBs map[string]pv1beta1.PodDisruptionBudget) map[string]pv1beta1.PodDisruptionBudget {
	desiredPDBs := make(map[string]pv1beta1.PodDisruptionBudget, len(managedPDBs))

	nonReadyTTL := time.Time{}
	if n.nonReadyTTL > 0 {
		nonReadyTTL = time.Now().UTC().Add(-n.nonReadyTTL)
	}

	for _, resource := range resources {
		matchedPDBs := getMatchedPDBs(resource.TemplateLabels(), unmanagedPDBs)

		// don't create managed PDB if there is already unmanaged and
		// matched PDBs
		if len(matchedPDBs) > 0 {
			continue
		}

		// don't create PDB if the resource has 1 or less replicas
		if resource.Replicas() <= 1 {
			continue
		}

		// ensure PDB if the resource has more than one replica and all
		// of them are ready
		if resource.StatusReadyReplicas() >= resource.Replicas() {
			pdb := n.generatePDB(resource, time.Time{})
			desiredPDBs[pdb.Namespace+"/"+pdb.Name] = pdb
			continue
		}

		ownedPDBs := getOwnedPDBs(managedPDBs, resource)
		validPDBs := make([]pv1beta1.PodDisruptionBudget, 0, len(ownedPDBs))
		// only consider valid PDBs. If they're invalid they'll be
		// recreated on the next iteration
		for _, pdb := range ownedPDBs {
			if pdbSpecValid(pdb) {
				validPDBs = append(validPDBs, pdb)
			}
		}

		if len(validPDBs) > 0 {
			// it's unlikely that we will have more than a single
			// valid owned PDB. If we do simply pick the first one
			// and check if it's still valid. Any other PDBs will
			// automatically get dropped.
			pdb := validPDBs[0]
			if pdb.Annotations == nil {
				pdb.Annotations = make(map[string]string)
			}

			ttl, err := overrideNonReadyTTL(resource.Annotations(), nonReadyTTL)
			if err != nil {
				log.Errorf("Failed to override PDB Delete TTL: %s", err)
			}

			var nonReadySince time.Time
			if nonReadySinceStr, ok := pdb.Annotations[nonReadySinceAnnotationName]; ok {
				nonReadySince, err = time.Parse(time.RFC3339, nonReadySinceStr)
				if err != nil {
					log.Errorf("Failed to parse non-ready-since annotation '%s': %v", nonReadySinceStr, err)
				}
			}

			if !nonReadySince.IsZero() {
				if !ttl.IsZero() && nonReadySince.Before(ttl) {
					continue
				}
			} else {
				nonReadySince = time.Now().UTC()
			}

			generatedPDB := n.generatePDB(resource, nonReadySince)
			desiredPDBs[generatedPDB.Namespace+"/"+generatedPDB.Name] = generatedPDB
		}
	}

	return desiredPDBs
}

func (n *PDBController) reconcilePDBs(desiredPDBs, managedPDBs map[string]pv1beta1.PodDisruptionBudget) {
	for key, managedPDB := range managedPDBs {
		desiredPDB, ok := desiredPDBs[key]
		if !ok {
			err := n.PolicyV1beta1().PodDisruptionBudgets(managedPDB.Namespace).Delete(managedPDB.Name, nil)
			if err != nil {
				log.Error(err)
				continue
			}

			log.WithFields(log.Fields{
				"action":    "removed",
				"pdb":       managedPDB.Name,
				"namespace": managedPDB.Namespace,
				"selector":  managedPDB.Spec.Selector.String(),
			}).Info("")
		}

		// check if PDBs are equal an only update if not
		if !equality.Semantic.DeepEqual(managedPDB, desiredPDB) {
			_, err := n.PolicyV1beta1().PodDisruptionBudgets(desiredPDB.Namespace).Update(&desiredPDB)
			if err != nil {
				log.Error(err)
				continue
			}

			log.WithFields(log.Fields{
				"action":    "updated",
				"pdb":       desiredPDB.Name,
				"namespace": desiredPDB.Namespace,
				"selector":  desiredPDB.Spec.Selector.String(),
			}).Info("")
		}
	}

	for key, desiredPDB := range desiredPDBs {
		if _, ok := managedPDBs[key]; !ok {
			_, err := n.PolicyV1beta1().PodDisruptionBudgets(desiredPDB.Namespace).Create(&desiredPDB)
			if err != nil {
				log.Error(err)
				continue
			}

			log.WithFields(log.Fields{
				"action":    "added",
				"pdb":       desiredPDB.Name,
				"namespace": desiredPDB.Namespace,
				"selector":  desiredPDB.Spec.Selector.String(),
			}).Info("")
		}
	}
}

func overrideNonReadyTTL(annotations map[string]string, nonReadyTTL time.Time) (time.Time, error) {
	if ttlVal, ok := annotations[nonReadyTTLAnnotationName]; ok {
		duration, err := time.ParseDuration(ttlVal)
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().UTC().Add(-duration), nil
	}
	return nonReadyTTL, nil
}

// pdbSpecValid returns true if the PDB spec is up-to-date
func pdbSpecValid(pdb pv1beta1.PodDisruptionBudget) bool {
	return pdb.Spec.MinAvailable == nil
}

// getMatchedPDBs gets matching PodDisruptionBudgets.
func getMatchedPDBs(labels map[string]string, pdbs map[string]pv1beta1.PodDisruptionBudget) []pv1beta1.PodDisruptionBudget {
	matchedPDBs := make([]pv1beta1.PodDisruptionBudget, 0)
	for _, pdb := range pdbs {
		if labelsIntersect(labels, pdb.Spec.Selector.MatchLabels) {
			matchedPDBs = append(matchedPDBs, pdb)
		}
	}
	return matchedPDBs
}

func getOwnedPDBs(pdbs map[string]pv1beta1.PodDisruptionBudget, owner kubeResource) []pv1beta1.PodDisruptionBudget {
	ownedPDBs := make([]pv1beta1.PodDisruptionBudget, 0, len(pdbs))
	for _, pdb := range pdbs {
		if isOwnedReference(owner, pdb.ObjectMeta) {
			ownedPDBs = append(ownedPDBs, pdb)
		}
	}
	return ownedPDBs
}

// isOwnedReference returns true if the dependent object is owned by the owner
// object.
func isOwnedReference(owner kubeResource, dependent metav1.ObjectMeta) bool {
	for _, ref := range dependent.OwnerReferences {
		if ref.APIVersion == owner.APIVersion() &&
			ref.Kind == owner.Kind() &&
			ref.UID == owner.UID() &&
			ref.Name == owner.Name() {
			return true
		}
	}
	return false
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

func filterPDBs(pdbs []pv1beta1.PodDisruptionBudget) (map[string]pv1beta1.PodDisruptionBudget, map[string]pv1beta1.PodDisruptionBudget) {
	managed := make(map[string]pv1beta1.PodDisruptionBudget, len(pdbs))
	unmanaged := make(map[string]pv1beta1.PodDisruptionBudget, len(pdbs))
	for _, pdb := range pdbs {
		if containLabels(pdb.Labels, ownerLabels) {
			managed[pdb.Namespace+"/"+pdb.Name] = pdb
			continue
		}
		unmanaged[pdb.Namespace+"/"+pdb.Name] = pdb
	}
	return managed, unmanaged
}

func (n *PDBController) generatePDB(owner kubeResource, ttl time.Time) pv1beta1.PodDisruptionBudget {
	var suffix string
	if n.pdbNameSuffix != "" {
		suffix = "-" + n.pdbNameSuffix
	}

	maxUnavailable := intstr.FromInt(1)
	pdb := pv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name() + suffix,
			Namespace: owner.Namespace(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: owner.APIVersion(),
					Kind:       owner.Kind(),
					Name:       owner.Name(),
					UID:        owner.UID(),
				},
			},
			Labels:      owner.Labels(),
			Annotations: make(map[string]string),
		},
		Spec: pv1beta1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       owner.Selector(),
		},
	}

	if pdb.Labels == nil {
		pdb.Labels = make(map[string]string)
	}
	pdb.Labels[heritageLabel] = pdbController

	if !ttl.IsZero() {
		pdb.Annotations[nonReadySinceAnnotationName] = ttl.Format(time.RFC3339)
	}
	return pdb
}
