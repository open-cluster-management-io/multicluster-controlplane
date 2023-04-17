// Copyright Contributors to the Open Cluster Management project
package metrics

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
	metricsapi "k8s.io/metrics/pkg/apis/metrics"
	metricsv1beta1api "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"open-cluster-management.io/multicluster-controlplane/test/performance/utils"
)

const labelSelector = "app=multicluster-controlplane"

var supportedMetricsAPIVersions = []string{
	"v1beta1",
}

type MetricsRecorder struct {
	namespace     string
	metricsClient metricsclientset.Interface
}

func BuildMetricsGetter(kubeConfig, namespace string) (*MetricsRecorder, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig with %s, %v", kubeConfig, err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build discovery client with %s, %v", kubeConfig, err)
	}

	apiGroups, err := discoveryClient.ServerGroups()
	if err != nil {
		return nil, err
	}

	if !metricsAPIAvailable(apiGroups) {
		return nil, fmt.Errorf("metrics API not available on the %s", kubeConfig)
	}

	metricsClient, err := metricsclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &MetricsRecorder{
		namespace:     namespace,
		metricsClient: metricsClient,
	}, nil
}

func (g *MetricsRecorder) Record(ctx context.Context, filename string, clusterCounts int) error {
	versionedMetrics, err := g.metricsClient.MetricsV1beta1().PodMetricses(g.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector})
	if err != nil {
		return fmt.Errorf("failed to list metrics in the namespace %s with %s, %v", g.namespace, labelSelector, err)
	}

	metrics := &metricsapi.PodMetricsList{}
	err = metricsv1beta1api.Convert_v1beta1_PodMetricsList_To_metrics_PodMetricsList(versionedMetrics, metrics, nil)
	if err != nil {
		return fmt.Errorf("failed to convert metrics, %v", err)
	}

	for _, m := range metrics.Items {
		for _, c := range m.Containers {
			if c.Name == "POD" {
				continue
			}

			memory, ok := c.Usage.Memory().AsInt64()
			if !ok {
				utils.PrintMsg(fmt.Sprintf("container=%s, cpu=%s, memory=unknown", c.Name, c.Usage.Cpu()))
				continue
			}

			// millicore
			cpu := c.Usage.Cpu()
			// megabytes
			memory = memory / 1024 / 1024
			utils.PrintMsg(fmt.Sprintf("container=%s, counts=%d, cpu=%s, memory=%dMi",
				c.Name, clusterCounts, cpu, memory))
			if err := utils.AppendRecordToFile(filename, fmt.Sprintf("%d,%s,%d",
				clusterCounts, strings.ReplaceAll(cpu.String(), "m", ""), memory)); err != nil {
				return fmt.Errorf("failed to dump metrics to file, %v", err)
			}
		}
	}

	return nil
}

func metricsAPIAvailable(discoveredAPIGroups *metav1.APIGroupList) bool {
	for _, discoveredAPIGroup := range discoveredAPIGroups.Groups {
		if discoveredAPIGroup.Name != metricsapi.GroupName {
			continue
		}
		for _, version := range discoveredAPIGroup.Versions {
			for _, supportedVersion := range supportedMetricsAPIVersions {
				if version.Version == supportedVersion {
					return true
				}
			}
		}
	}
	return false
}
