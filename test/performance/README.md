[comment]: # ( Copyright Contributors to the Open Cluster Management project )
# Performance test
We can use the `perftool` to do the performance test for the multicluster controlplane

## Required

- One Kubernetes cluster, we will deploy the multicluster controlplane on that.
- [Kind](https://kind.sigs.k8s.io/) environment, we will create one kind cluster for performance test.

## Run

1. export your Kubernetes cluster kubeconfig with `KUBECONFIG`.
2. run `make test-performance` to start the performance test.

## Configuration

By default, this performance test will create 1k clusters and create 5 manifestworks on the each clsuter, you can
configure the `--count` and `--work-count` in the `hack/performance.sh` to modify the cluster and manifestwork conuts.

## Test Result

There is a [doc](https://docs.google.com/spreadsheets/d/11GcIXAxPpQlu35VWnN5sVtqrtkM0EYm3rqj8sTz2Pvs/edit#gid=0) to record our test result.
