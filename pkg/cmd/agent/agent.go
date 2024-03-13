// Copyright Contributors to the Open Cluster Management project
package agent

import (
	"context"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/ocm/pkg/features"

	"open-cluster-management.io/multicluster-controlplane/pkg/agent"
	mcfeature "open-cluster-management.io/multicluster-controlplane/pkg/feature"
)

func init() {
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
	utilruntime.Must(features.SpokeMutableFeatureGate.Add(mcfeature.DefaultControlPlaneAgentFeatureGates))
}

func NewAgent() *cobra.Command {
	agentOptions := agent.NewAgentOptions().
		WithWorkloadSourceDriverConfig("/spoke/hub-kubeconfig/kubeconfig")

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Start a Multicluster Controlplane Agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			shutdownCtx, cancel := context.WithCancel(context.TODO())

			shutdownHandler := server.SetupSignalHandler()
			go func() {
				defer cancel()
				<-shutdownHandler
				klog.Infof("Received SIGTERM or SIGINT signal, shutting down agent.")
			}()

			ctx, terminate := context.WithCancel(shutdownCtx)
			defer terminate()

			go func() {
				klog.Info("starting the controlplane agent")
				if err := agentOptions.RunAgent(ctx); err != nil {
					klog.Fatalf("failed to run agent, %v", err)
				}
			}()

			if err := agentOptions.RunAddOns(ctx); err != nil {
				return err
			}

			<-ctx.Done()
			return nil
		},
	}

	flags := cmd.Flags()
	features.SpokeMutableFeatureGate.AddFlag(flags)
	agentOptions.AddFlags(flags)
	return cmd
}
