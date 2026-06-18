# Pod Disruption Budget Controller
[![Build Status](https://github.com/mikkeloscar/pdb-controller/workflows/ci/badge.svg)](https://github.com/mikkeloscar/pdb-controller/actions?query=branch:master)
[![Coverage Status](https://coveralls.io/repos/github/mikkeloscar/pdb-controller/badge.svg)](https://coveralls.io/github/mikkeloscar/pdb-controller)

This is a simple Kubernetes controller for adding default [Pod Disruption
Budgets (PDBs)][pdb] for Deployments and StatefulSets in case none are defined. This
is inspired by the dicussion in
[kubernetes/kubernetes#35318](https://github.com/kubernetes/kubernetes/issues/35318)
and was created for lack of an alternative.

## How it works

The controller uses Kubernetes informers and watch functionality to detect changes in Deployments, StatefulSets and PodDisruptionBudgets.
It automatically gets all Pod Disruption Budgets for each namespace and compares them to Deployments and StatefulSets. 
For any resource with more than 1 replica and no matching Pod Disruption Budget, a default PDB will be created:

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
  maxUnavailable: 1
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

Additionally you can run the controller with the flag `--non-ready-ttl=15m`
which means it will remove owned PDBs in case the pods of a targeted deployment
or statefulset are non-ready for more than the specified ttl. This is another
way to ensure broken deployments doesn't block cluster operations.

This global value can also be overriden by specifying the annotation
`pdb-controller.zalando.org/non-ready-ttl` on a deployment or statefulset.

## Building

This project uses [Go modules](https://github.com/golang/go/wiki/Modules) as
introduced in Go 1.11 therefore you need Go >=1.11 installed in order to build.
If using Go 1.11 you also need to [activate Module
support](https://github.com/golang/go/wiki/Modules#installing-and-activating-module-support).

Assuming Go has been setup with module support it can be built simply by running:

```sh
$ make
```

## Setup

The `pdb-controller` can be run as a deployment in the cluster. See
[deployment.yaml](docs/deployment.yaml) for an example.

Deploy it by running:

```bash
$ kubectl apply -f docs/deployment.yaml
```

## TODO

* [ ] Instead of long polling, add a Watch feature.

## LICENSE

See [LICENSE](LICENSE) file.

[pdb]: https://kubernetes.io/docs/tasks/run-application/configure-pdb/
