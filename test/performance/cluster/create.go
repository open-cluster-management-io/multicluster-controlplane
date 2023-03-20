// Copyright Contributors to the Open Cluster Management project

package cluster

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/spf13/pflag"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/multicluster-controlplane/pkg/agent"
	"open-cluster-management.io/multicluster-controlplane/test/performance/metrics"
	"open-cluster-management.io/multicluster-controlplane/test/performance/utils"
)

const (
	performanceTestLabel = "test.multicluster-controlplane.open-cluster-management.io/performance"
	defaultNamespace     = "multicluster-controlplane"
)

type clusterCreateOptions struct {
	ClusterNamePrefix       string
	Kubeconfig              string
	HubKubeconfig           string
	SpokeKubeconfig         string
	Namespace               string
	ResourceMetricsFileName string
	Count                   int
	WorkCount               int
	Interval                time.Duration
	Pseudo                  bool

	hubKubeClient    kubernetes.Interface
	hubClusterClient clusterclient.Interface
	hubWorkClient    workclient.Interface

	spokeRestConfig *rest.Config

	metricsRecorder *metrics.MetricsRecorder
}

func NewClusterRunOptions() *clusterCreateOptions {
	return &clusterCreateOptions{
		Count:             1,
		Interval:          5 * time.Second,
		ClusterNamePrefix: "test",
		Namespace:         defaultNamespace,
		WorkCount:         5,
	}
}

func (o *clusterCreateOptions) Complete() error {
	if o.HubKubeconfig == "" {
		return fmt.Errorf("flag `--controlplane-kubeconfig` is requried")
	}

	hubConfig, err := clientcmd.BuildConfigFromFlags("", o.HubKubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build hub kubeconfig with %s, %v", o.HubKubeconfig, err)
	}

	o.hubKubeClient, err = kubernetes.NewForConfig(hubConfig)
	if err != nil {
		return fmt.Errorf("failed to build hub kube client with %s, %v", o.HubKubeconfig, err)
	}

	o.hubClusterClient, err = clusterclient.NewForConfig(hubConfig)
	if err != nil {
		return fmt.Errorf("failed to build hub cluster client with %s, %v", o.HubKubeconfig, err)
	}

	o.hubWorkClient, err = workclient.NewForConfig(hubConfig)
	if err != nil {
		return fmt.Errorf("failed to build hub work client with %s, %v", o.HubKubeconfig, err)
	}

	o.metricsRecorder, err = metrics.BuildMetricsGetter(o.Kubeconfig, o.Namespace)
	if err != nil {
		return fmt.Errorf("failed to build metrics getter with %s, %v", o.Kubeconfig, err)
	}

	if !o.Pseudo {
		o.spokeRestConfig, err = clientcmd.BuildConfigFromFlags("", o.SpokeKubeconfig)
		if err != nil {
			return fmt.Errorf("failed to build spoke kubeconfig with %s, %v", o.SpokeKubeconfig, err)
		}
	}

	return nil
}

func (o *clusterCreateOptions) Validate() error {
	if o.Count <= 0 {
		return fmt.Errorf("flag `--count` must be greater than 0")
	}

	if o.ClusterNamePrefix == "" {
		return fmt.Errorf("flag `--cluster-name-prefix` is required")
	}

	if o.Interval <= 0 {
		return fmt.Errorf("flag `--interval` must be greater than 0")
	}

	return nil
}

func (o *clusterCreateOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "The kubeconfig of multicluster controlplane running cluster")
	fs.StringVar(&o.HubKubeconfig, "controlplane-kubeconfig", o.HubKubeconfig, "The kubeconfig of multicluster controlplane")
	fs.StringVar(&o.SpokeKubeconfig, "spoke-kubeconfig", o.SpokeKubeconfig, "The kubeconfig of spoke cluster")
	fs.StringVar(&o.Namespace, "controlplane-namespace", o.Namespace, "The namespace of multicluster controlplane")
	fs.StringVar(&o.ClusterNamePrefix, "cluster-name-prefix", o.ClusterNamePrefix, "The name prefix of clusters")
	fs.StringVar(&o.ResourceMetricsFileName, "res-metrics-file-name", o.ResourceMetricsFileName, "The file name of resource metrics")
	fs.IntVar(&o.Count, "count", o.Count, "The count of clusters")
	fs.IntVar(&o.WorkCount, "work-count", o.WorkCount, "The count of works in one cluster")
	fs.DurationVar(&o.Interval, "interval", o.Interval, "The interval time of creating cluster, only for psedudo clusters")
	fs.BoolVar(&o.Pseudo, "pseduo", o.Pseudo, "Only create an accepted managed cluster")
}

func (o *clusterCreateOptions) Run(ctx context.Context) error {
	currentClusters, err := o.getCurrentClusterCount(ctx)
	if err != nil {
		return err
	}

	utils.PrintMsg(fmt.Sprintf("current clusters count %d, expected clusters count %d", currentClusters, o.Count))

	if (o.Count - currentClusters) <= 0 {
		return nil
	}

	if err := o.metricsRecorder.Record(ctx, o.ResourceMetricsFileName, currentClusters); err != nil {
		return err
	}

	utils.PrintMsg(fmt.Sprintf("%d clusters will be created ...", o.Count-currentClusters))

	for i := currentClusters; i < o.Count; i++ {
		clusterName := getClusterName(o.ClusterNamePrefix, i)

		utils.PrintMsg(fmt.Sprintf("Cluster %q is creating", clusterName))
		startTime := time.Now()
		if err := o.createClusterNamespace(ctx, clusterName); err != nil {
			return err
		}

		if err := o.createCluster(ctx, clusterName); err != nil {
			return err
		}

		if !o.Pseudo {
			if err := o.registerCluster(ctx, clusterName); err != nil {
				return err
			}
		}

		usedTime := time.Since(startTime)
		utils.PrintMsg(fmt.Sprintf("Cluster %q is created, time used %ds",
			clusterName, usedTime/(time.Millisecond*time.Microsecond)))

		if i != 0 && i%10 == 0 {
			if err := o.metricsRecorder.Record(ctx, o.ResourceMetricsFileName, i); err != nil {
				return err
			}
		}

		time.Sleep(o.Interval)
	}

	if err := o.metricsRecorder.Record(ctx, o.ResourceMetricsFileName, o.Count); err != nil {
		return err
	}

	return nil
}

func (o *clusterCreateOptions) getCurrentClusterCount(ctx context.Context) (int, error) {
	clusters, err := o.hubClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", performanceTestLabel),
	})
	if err != nil {
		return -1, err
	}

	return len(clusters.Items), nil
}

func (o *clusterCreateOptions) createClusterNamespace(ctx context.Context, name string) error {
	_, err := o.hubKubeClient.CoreV1().Namespaces().Create(
		ctx,
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					performanceTestLabel: "true",
				},
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func (o *clusterCreateOptions) createCluster(ctx context.Context, name string) error {
	_, err := o.hubClusterClient.ClusterV1().ManagedClusters().Create(
		ctx,
		&clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					performanceTestLabel: "true",
				},
			},
			Spec: clusterv1.ManagedClusterSpec{
				HubAcceptsClient: true,
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func (o *clusterCreateOptions) registerCluster(ctx context.Context, clusterName string) error {
	agentHubKubeconfigDir := path.Join("/tmp", "performance-test-agent", rand.String(6), clusterName, "hub-kubeconfig")
	if err := os.MkdirAll(agentHubKubeconfigDir, os.ModePerm); err != nil {
		return err
	}

	utils.PrintMsg(fmt.Sprintf("starting the agent for cluster %q ...", clusterName))
	klusterletAgent := agent.NewAgentOptions().
		WithClusterName(clusterName).
		WithSpokeKubeconfig(o.spokeRestConfig).
		WithBootstrapKubeconfig(o.HubKubeconfig).
		WithHubKubeconfigDir(agentHubKubeconfigDir).
		WithHubKubeconfigSecreName(fmt.Sprintf("%s-hub-kubeconfig-secret", clusterName))
	go func() {
		if err := klusterletAgent.RunAgent(ctx); err != nil {
			klog.Fatalf("Failed to start agent for cluster %s, %v", clusterName, err)
		}
	}()

	utils.PrintMsg(fmt.Sprintf("approving the cluster %q ...", clusterName))
	if err := o.approveCSR(ctx, clusterName); err != nil {
		return err
	}

	utils.PrintMsg(fmt.Sprintf("waiting the cluster %q becomes available ...", clusterName))
	if err := o.waitClusterAvailable(ctx, clusterName); err != nil {
		return err
	}

	utils.PrintMsg(fmt.Sprintf("creating %d works in the cluster %q ...", o.WorkCount, clusterName))
	for i := 0; i < o.WorkCount; i++ {
		workName := fmt.Sprintf("%s-work-%d", clusterName, i)
		if err := o.createManifestWork(ctx, clusterName, workName); err != nil {
			return err
		}
	}

	return nil
}

// TODO remove this after registration starts supporting auto approve
func (o *clusterCreateOptions) approveCSR(ctx context.Context, clusterName string) error {
	return wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
		csrs, err := o.hubKubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s", clusterName),
		})
		if err != nil {
			return false, err
		}

		if len(csrs.Items) == 0 {
			return false, nil
		}

		for _, csr := range csrs.Items {
			if isCSRInTerminalState(&csr.Status) {
				continue
			}

			copied := csr.DeepCopy()
			copied.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
				Type:           certificatesv1.CertificateApproved,
				Status:         corev1.ConditionTrue,
				Reason:         "AutoApprovedByE2ETest",
				Message:        "Auto approved by e2e test",
				LastUpdateTime: metav1.Now(),
			})
			_, err := o.hubKubeClient.CertificatesV1().CertificateSigningRequests().UpdateApproval(
				ctx, copied.Name, copied, metav1.UpdateOptions{})
			if err != nil {
				return false, err
			}
		}

		return true, nil
	})
}

func (o *clusterCreateOptions) waitClusterAvailable(ctx context.Context, clusterName string) error {
	return wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		cluster, err := o.hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable) {
			return true, nil
		}

		return false, nil
	})
}

func (o *clusterCreateOptions) createManifestWork(ctx context.Context, clusterName, workName string) error {
	work := getManifestWork(workName)
	_, err := o.hubWorkClient.WorkV1().ManifestWorks(clusterName).Create(ctx, work, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		work, err := o.hubWorkClient.WorkV1().ManifestWorks(clusterName).Get(ctx, workName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(work.Status.Conditions, workv1.WorkAvailable) {
			return true, nil
		}

		return false, nil
	})
}

func getClusterName(prefix string, index int) string {
	return fmt.Sprintf("%s-%d", prefix, index)
}

func isCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

func getManifestWork(workName string) *workv1.ManifestWork {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      fmt.Sprintf("cm-%s", workName),
		},
		Data: map[string]string{
			"test": "I'm a test configmap",
		},
	}

	manifest := workv1.Manifest{}
	manifest.Object = cm

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: workName,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{
					manifest,
				},
			},
		},
	}
}
