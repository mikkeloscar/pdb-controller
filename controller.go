package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	pv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
)

const (
	heritageLabel               = "heritage"
	pdbController               = "pdb-controller"
	nonReadyTTLAnnotationName   = "pdb-controller.zalando.org/non-ready-ttl"
	nonReadySinceAnnotationName = "pdb-controller.zalando.org/non-ready-since"
	parentResourceHashLabel     = "parent-resource-hash"
)

var (
	ownerLabels = map[string]string{heritageLabel: pdbController}
)

// PDBController creates PodDisruptionBudgets for deployments and StatefulSets
// if missing.
type PDBController struct {
	kubernetes.Interface
	interval            time.Duration // kept for backward compatibility
	pdbNameSuffix       string
	nonReadyTTL         time.Duration
	parentResourceHash  bool
	maxUnavailable      intstr.IntOrString
	queue               workqueue.TypedRateLimitingInterface[string]
	deploymentInformer  cache.SharedIndexInformer
	statefulSetInformer cache.SharedIndexInformer
	pdbInformer         cache.SharedIndexInformer
}

// NewPDBController initializes a new PDBController.
func NewPDBController(interval time.Duration, client kubernetes.Interface, pdbNameSuffix string, nonReadyTTL time.Duration, parentResourceHash bool, maxUnavailable intstr.IntOrString) *PDBController {
	log.Info("Initializing PDB controller with TypedRateLimitingInterface - v20250304")
	controller := &PDBController{
		Interface:          client,
		interval:           interval,
		pdbNameSuffix:      pdbNameSuffix,
		nonReadyTTL:        nonReadyTTL,
		parentResourceHash: parentResourceHash,
		maxUnavailable:     maxUnavailable,
		queue:              workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}

	// Setup Deployment informer
	controller.deploymentInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.AppsV1().Deployments(v1.NamespaceAll).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.AppsV1().Deployments(v1.NamespaceAll).Watch(context.Background(), options)
			},
		},
		&appsv1.Deployment{},
		0, // resync disabled
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Setup StatefulSet informer
	controller.statefulSetInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.AppsV1().StatefulSets(v1.NamespaceAll).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.AppsV1().StatefulSets(v1.NamespaceAll).Watch(context.Background(), options)
			},
		},
		&appsv1.StatefulSet{},
		0, // resync disabled
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Setup PodDisruptionBudget informer
	controller.pdbInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.PolicyV1().PodDisruptionBudgets(v1.NamespaceAll).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.PolicyV1().PodDisruptionBudgets(v1.NamespaceAll).Watch(context.Background(), options)
			},
		},
		&pv1.PodDisruptionBudget{},
		0, // resync disabled
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Setup event handlers
	controller.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueResource,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueResource(new)
		},
		DeleteFunc: controller.enqueueResource,
	})

	controller.statefulSetInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueResource,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueResource(new)
		},
		DeleteFunc: controller.enqueueResource,
	})

	controller.pdbInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: controller.enqueuePDB,
	})

	return controller
}

func (n *PDBController) enqueueResource(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	n.queue.Add(key)
}

func (n *PDBController) enqueuePDB(obj interface{}) {
	pdb, ok := obj.(*pv1.PodDisruptionBudget)
	if !ok {
		log.Errorf("Expected PodDisruptionBudget but got %+v", obj)
		return
	}

	// Only enqueue for our managed PDBs
	if !containLabels(pdb.Labels, ownerLabels) {
		return
	}

	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	n.queue.Add(key)
}

// Run runs the controller with the specified number of workers.
func (n *PDBController) Run(ctx context.Context) {
	defer n.queue.ShutDown()

	log.Info("Starting PDB controller")

	// Start the informers
	go n.deploymentInformer.Run(ctx.Done())
	go n.statefulSetInformer.Run(ctx.Done())
	go n.pdbInformer.Run(ctx.Done())

	// Wait for the caches to be synced
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(ctx.Done(),
		n.deploymentInformer.HasSynced,
		n.statefulSetInformer.HasSynced,
		n.pdbInformer.HasSynced) {
		log.Fatal("Failed to wait for caches to sync")
		return
	}
	log.Info("Informer caches synced")

	// Run the reconcile loop
	go n.worker(ctx)

	<-ctx.Done()
	log.Info("Shutting down PDB controller")
}

func (n *PDBController) worker(ctx context.Context) {
	for n.processNextItem(ctx) {
	}
}

func (n *PDBController) processNextItem(ctx context.Context) bool {
	key, quit := n.queue.Get()
	if quit {
		return false
	}
	defer n.queue.Done(key)

	err := n.reconcile(ctx, key)
	if err != nil {
		log.Errorf("Error processing item %s: %v", key, err)
		n.queue.AddRateLimited(key)
		return true
	}

	n.queue.Forget(key)
	return true
}

func (n *PDBController) reconcile(ctx context.Context, key string) error {
	log.Debugf("Processing key: %s", key)

	// Process all resources and PDBs
	return n.runOnce(ctx)
}

// runOnce runs the main reconcilation loop of the controller.
func (n *PDBController) runOnce(ctx context.Context) error {
	// Get all PDBs from the informer
	var allPDBs []pv1.PodDisruptionBudget
	for _, obj := range n.pdbInformer.GetStore().List() {
		pdb, ok := obj.(*pv1.PodDisruptionBudget)
		if !ok {
			continue
		}
		allPDBs = append(allPDBs, *pdb)
	}

	managedPDBs, unmanagedPDBs := filterPDBs(allPDBs)

	// Get all resources from the informers
	resources := make([]kubeResource, 0)

	// Process Deployments
	for _, obj := range n.deploymentInformer.GetStore().List() {
		d, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}
		// manually set Kind and APIVersion because of a bug in
		// client-go
		// https://github.com/kubernetes/client-go/issues/308
		d.Kind = "Deployment"
		d.APIVersion = "apps/v1"
		resources = append(resources, deployment{*d})
	}

	// Process StatefulSets
	for _, obj := range n.statefulSetInformer.GetStore().List() {
		s, ok := obj.(*appsv1.StatefulSet)
		if !ok {
			continue
		}
		// manually set Kind and APIVersion because of a bug in
		// client-go
		// https://github.com/kubernetes/client-go/issues/308
		s.Kind = "StatefulSet"
		s.APIVersion = "apps/v1"
		resources = append(resources, statefulSet{*s})
	}

	desiredPDBs := n.generateDesiredPDBs(resources, managedPDBs, unmanagedPDBs)
	n.reconcilePDBs(ctx, desiredPDBs, managedPDBs)
	return nil
}

func (n *PDBController) generateDesiredPDBs(resources []kubeResource, managedPDBs, unmanagedPDBs map[string]pv1.PodDisruptionBudget) map[string]pv1.PodDisruptionBudget {
	desiredPDBs := make(map[string]pv1.PodDisruptionBudget, len(managedPDBs))

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
		validPDBs := make([]pv1.PodDisruptionBudget, 0, len(ownedPDBs))
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

// mergeActualAndDesiredPDB takes the current definition of a PDB as it is in cluster and a PDB
// with our desired configurations and does a merge between them. We also return a boolean to tell the
// caller if any change actually had to be made to achieve our desired state or not
func mergeActualAndDesiredPDB(managedPDB, desiredPDB pv1.PodDisruptionBudget) (pv1.PodDisruptionBudget, bool) {

	needsUpdate := false

	// check if PDBs are equal an only update if not
	if !equality.Semantic.DeepEqual(managedPDB.Spec, desiredPDB.Spec) ||
		!equality.Semantic.DeepEqual(managedPDB.Labels, desiredPDB.Labels) ||
		!equality.Semantic.DeepEqual(managedPDB.Annotations, desiredPDB.Annotations) {
		managedPDB.Annotations = desiredPDB.Annotations
		managedPDB.Labels = desiredPDB.Labels
		managedPDB.Spec = desiredPDB.Spec

		needsUpdate = true
	}

	return managedPDB, needsUpdate
}

func (n *PDBController) reconcilePDBs(ctx context.Context, desiredPDBs, managedPDBs map[string]pv1.PodDisruptionBudget) {
	for key, managedPDB := range managedPDBs {
		desiredPDB, ok := desiredPDBs[key]
		if !ok {
			err := n.PolicyV1().PodDisruptionBudgets(managedPDB.Namespace).Delete(ctx, managedPDB.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Errorf("Failed to delete PDB: %v", err)
				continue
			}

			log.WithFields(log.Fields{
				"action":    "removed",
				"pdb":       managedPDB.Name,
				"namespace": managedPDB.Namespace,
				"selector":  managedPDB.Spec.Selector.String(),
			}).Info("")

			// If we delete a PDB then we don't want to attempt to update it later since this will
			// result in a `StorageError` since we can't find the PDB to make an update to it.
			continue
		}

		// check if PDBs are equal an only update if not
		updatedPDB, needsUpdate := mergeActualAndDesiredPDB(managedPDB, desiredPDB)
		if needsUpdate {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				// Technically the updatedPDB and managedPDB namespace should never be different
				// but just to be **certain** we're updating the correct namespace we'll just use
				// the one that was given to us and not potentially modified
				_, err := n.PolicyV1().PodDisruptionBudgets(managedPDB.Namespace).Update(ctx, &updatedPDB, metav1.UpdateOptions{})

				// If the update failed that likely means that our definition of what was on the cluster
				// has become out of date. To resolve this we'll need to get a more up to date copy of
				// the object we're attempting to modify
				if err != nil {
					currentPDB, err := n.PolicyV1().PodDisruptionBudgets(managedPDB.Namespace).Get(ctx, managedPDB.Name, metav1.GetOptions{})

					// This err is locally scoped to this if block and will not cause our `RetryOnConflict`
					// to pass if it is nil. If we're in this block then we will get another Retry
					if err != nil {
						return err
					}

					updatedPDB, _ = mergeActualAndDesiredPDB(
						*currentPDB,
						desiredPDB,
					)
				}

				// If this err != nil then the current block will be re-run by `RetryOnConflict`
				// on an exponential backoff schedule to see if we can fix the problem by trying again
				return err
			})
			if err != nil {
				log.Errorf("Failed to update PDB: %v", err)
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
			_, err := n.PolicyV1().PodDisruptionBudgets(desiredPDB.Namespace).Create(ctx, &desiredPDB, metav1.CreateOptions{})
			if err != nil {
				log.Errorf("Failed to create PDB: %v", err)
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
func pdbSpecValid(pdb pv1.PodDisruptionBudget) bool {
	return pdb.Spec.MinAvailable == nil
}

// getMatchedPDBs gets matching PodDisruptionBudgets.
func getMatchedPDBs(labels map[string]string, pdbs map[string]pv1.PodDisruptionBudget) []pv1.PodDisruptionBudget {
	matchedPDBs := make([]pv1.PodDisruptionBudget, 0)
	for _, pdb := range pdbs {
		if labelsIntersect(labels, pdb.Spec.Selector.MatchLabels) {
			matchedPDBs = append(matchedPDBs, pdb)
		}
	}
	return matchedPDBs
}

func getOwnedPDBs(pdbs map[string]pv1.PodDisruptionBudget, owner kubeResource) []pv1.PodDisruptionBudget {
	ownedPDBs := make([]pv1.PodDisruptionBudget, 0, len(pdbs))
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

func filterPDBs(pdbs []pv1.PodDisruptionBudget) (map[string]pv1.PodDisruptionBudget, map[string]pv1.PodDisruptionBudget) {
	managed := make(map[string]pv1.PodDisruptionBudget, len(pdbs))
	unmanaged := make(map[string]pv1.PodDisruptionBudget, len(pdbs))
	for _, pdb := range pdbs {
		if containLabels(pdb.Labels, ownerLabels) {
			managed[pdb.Namespace+"/"+pdb.Name] = pdb
			continue
		}
		unmanaged[pdb.Namespace+"/"+pdb.Name] = pdb
	}
	return managed, unmanaged
}

func (n *PDBController) generatePDB(owner kubeResource, ttl time.Time) pv1.PodDisruptionBudget {
	var suffix string
	if n.pdbNameSuffix != "" {
		suffix = "-" + n.pdbNameSuffix
	}

	pdb := pv1.PodDisruptionBudget{
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
		Spec: pv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &n.maxUnavailable,
			Selector:       owner.Selector(),
		},
	}

	if n.parentResourceHash {
		// if we fail to generate the hash simply fall back to using
		// the existing selector
		hash, err := resourceHash(owner.Kind(), owner.Name())
		if err == nil {
			pdb.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					parentResourceHashLabel: hash,
				},
			}
		}
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

func resourceHash(kind, name string) (string, error) {
	h := sha1.New()
	_, err := h.Write([]byte(kind + "-" + name))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
