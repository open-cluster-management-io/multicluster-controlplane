// Copyright Contributors to the Open Cluster Management project
package controller

import (
	"fmt"
	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/ocm/pkg/features"

	"open-cluster-management.io/multicluster-controlplane/pkg/servers"
	"open-cluster-management.io/multicluster-controlplane/pkg/servers/options"

	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/version/verflag"
)

func init() {
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates))
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))
	utilruntime.Must(features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubAddonManagerFeatureGates))
}

func NewController() *cobra.Command {
	options := options.NewServerRunOptions()
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start a Multicluster Controlplane Server",
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			if err := logsapi.ValidateAndApply(options.Logs, utilfeature.DefaultFeatureGate); err != nil {
				return err
			}

			stopChan := genericapiserver.SetupSignalHandler()
			if err := options.Complete(stopChan); err != nil {
				return err
			}

			if err := options.Validate(); err != nil {
				return err
			}

			return servers.NewServer(*options).Start(stopChan)
		},
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if len(arg) > 0 {
					return fmt.Errorf("%q does not take any arguments, got %q", cmd.CommandPath(), args)
				}
			}
			return nil
		},
	}

	features.HubMutableFeatureGate.AddFlag(cmd.Flags())
	options.AddFlags(cmd.Flags())
	return cmd
}
