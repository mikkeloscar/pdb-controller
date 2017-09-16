# Pod Disruption Budget Controller
[![Build Status](https://travis-ci.org/mikkeloscar/pdb-controller.svg?branch=master)](https://travis-ci.org/mikkeloscar/pdb-controller)
[![Coverage Status](https://coveralls.io/repos/github/mikkeloscar/pdb-controller/badge.svg)](https://coveralls.io/github/mikkeloscar/pdb-controller)

This is a simple Kubernetes controller for adding default [Pod Disruption
Budgets (PDBs)][pdb] for Deployments and StatefulSets in case none are defined. This
is inspired by the dicussion in
[kubernetes/kubernetes#35318](https://github.com/kubernetes/kubernetes/issues/35318).

## How it works

The controller simply gets all Pod Disruption Budgets for each namespace and
compares them to Deployments and StatefulSets. For any resource with more than
1 replica and no matching Pod Disruption Budget, a default PDB will be created:

```yaml
apiVersion: policy/v1beta1
kind: PodDisruptionBudget
metadata:
  name: my-app
  namespace: kube-system
  labels:
    application: my-app
    heritage: pdb-controller
    version: v1.0.0
spec:
  minAvailable: 1
  selector:
    matchLabels:
      application: my-app
```

The selector and labels are based on those from the related Deployment or
StatefulSet. The special `heritage=pdb-controller` label is set by the
controller and is used to find owned PDBs. Owned PDBs are removed in case
replicas of the related resource is scaled to 1 or less. This
is done to prevent deadlocking for clients depending on the PDBs e.g. cluster
upgrade tools.

## Setup

The `pdb-controller` can be run as a deployment in the cluster. See
[deployment.yaml](/Docs/deployment.yaml).

Deploy it by running:

```bash
$ kubectl apply -f Docs/deployment.yaml
```

## TODO

* [ ] Instead of long polling, add a Watch feature.

[pdb]: https://kubernetes.io/docs/tasks/run-application/configure-pdb/
