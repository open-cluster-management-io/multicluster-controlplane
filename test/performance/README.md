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

By default, this performance test creates 1000 clusters and generates 5 manifest works for each cluster, you can
configure the `--count` and `--work-count` in the `hack/performance.sh` to modify the cluster and manifestwork conuts.

By default, we use the below manifestwork as the workload

```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: perftest-<cluster-name>-work-<work-number>
spec:
  workload:
    manifests:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: perftest-<cluster-name>-work-<work-number>
        namespace: default
      data:
        test-data: "I'm a test configmap"
```

You can customize your own workload by following steps

1. Define your manifestworks in a directory and use `perftest.open-cluster-management.io/expected-work-count` annotation
to specify your expected manifestwork counts, e.g.

```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: work-1
  annotations:
    "perftest.open-cluster-management.io/expected-work-count": "10"
spec:	
  workload:
    manifests: <your-manifests>

---

apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: work-2
  annotations:
    "perftest.open-cluster-management.io/expected-work-count": "20"
spec:	
  workload:
    manifests: <your-manifests>
```

2. Use `--work-template-dir` flag to specify your manifestworks directory path, then the `perftool` will generate the 
manifest works with your expected work count on each cluster, and the generated work will use a `configmap` to save your
manifestwork to simulate the same size with your customized workloads, for above example, the `perftool` will generate
10 manifest works for `work-1` and 20 manifest works `work-2` on each cluster, and the generated manifest work will be

```yaml
apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: perftest-<cluster-name>-work-work-1-<work-number>
spec:	
  workload:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: perftest-<cluster-name>-work-1-<work-number>
        namespace: default
      data:
        test-data: |-
          <the content of work-1>

---

apiVersion: work.open-cluster-management.io/v1
kind: ManifestWork
metadata:
  name: <cluster-name>-work-work-2-<work-number>
spec:	
  workload:
    - apiVersion: v1
      kind: ConfigMap
      metadata:
        name: perftest-<cluster-name>-work-2-<work-number>
        namespace: default
      data:
        test-data: |-
          <the content of work-2>
```

## Test Result

There is a [doc](https://docs.google.com/spreadsheets/d/11GcIXAxPpQlu35VWnN5sVtqrtkM0EYm3rqj8sTz2Pvs/edit#gid=0) to record our test result.
