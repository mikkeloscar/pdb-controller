# Pod Disruption Budget Controller

Hard fork of https://github.com/mikkeloscar/pdb-controller

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

Additionally you can run the controller with the flag `--non-ready-ttl=15m`
which means it will remove owned PDBs in case the pods of a targeted deployment
or statefulset are non-ready for more than the specified ttl. This is another
way to ensure broken deployments doesn't block cluster operations.

## Building

To build this project you need to install [Go](https://golang.org/dl/) and checkout the repository.

```bash
$ git clone https://github.com/dreamteam-gg/pdb-controller.git
$ cd pdb-controller
$ go build
```

## GolangCI-Lint

To run code linting for this project you need to install [GolangCI-Lint](https://github.com/golangci/golangci-lint#install) and checkout the repository.

```bash
$ git clone https://github.com/dreamteam-gg/pdb-controller.git
$ cd pdb-controller
$ golangci-lint run
```

## Setup

The `pdb-controller` can be run as a deployment in the cluster or locally. See
[deployment.yaml](/Docs/deployment.yaml) for an in-cluster example.

Deploy it to cluster by running:

```bash
$ kubectl apply -f Docs/deployment.yaml
```

Or run locally for debug purposes:

```bash
$ go run pdb-controller  --kubeconfig=[path-to-kubeconfig]
```
This will use the current context or your local kubeconfig

## TODO

* [ ] Instead of long polling, add a Watch feature.

## LICENSE

See [LICENSE](LICENSE) file.

[pdb]: https://kubernetes.io/docs/tasks/run-application/configure-pdb/
