# Pod Disruption Budget Controller
[![Build Status](https://travis-ci.org/mikkeloscar/pdb-controller.svg?branch=master)](https://travis-ci.org/mikkeloscar/pdb-controller)
[![Coverage Status](https://coveralls.io/repos/github/mikkeloscar/pdb-controller/badge.svg)](https://coveralls.io/github/mikkeloscar/pdb-controller)

This is a simple Kubernetes controller for adding default [Pod Disruption
Budgets (PDBs)][pdb] for Deployments and StatefulSets in case none are defined. This
is inspired by the dicussion in
[kubernetes/kubernetes#35318](https://github.com/kubernetes/kubernetes/issues/35318)
and was created for lack of an alternative.

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

## Building

To build this project you need to have [Go](https://golang.org/dl/) installed
and checkout the repository into your `$GOPATH`.

```bash
$ git clone https://github.com/mikkeloscar/pdb-controller.git $GOPATH/src/github.com/mikkeloscar/pdb-controller
```

[Dep](https://github.com/golang/dep) is used for vendoring dependencies. They
are not checked into the repository so you need to fetch then before building.

```bash
$ go get -u github.com/golang/dep/cmd/dep
$ cd $GOPATH/src/github.com/mikkeloscar/pdb-controller
$ dep ensure -vendor-only -v
```

Once dependencies are fetch you can build the binary simply by running `make`.

## Setup

The `pdb-controller` can be run as a deployment in the cluster. See
[deployment.yaml](/Docs/deployment.yaml) for an example.

Deploy it by running:

```bash
$ kubectl apply -f Docs/deployment.yaml
```

## TODO

* [ ] Instead of long polling, add a Watch feature.

## LICENSE

See [LICENSE](LICENSE) file.

[pdb]: https://kubernetes.io/docs/tasks/run-application/configure-pdb/
