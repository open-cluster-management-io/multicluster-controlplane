// Copyright Contributors to the Open Cluster Management project

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/cli"
	logsapi "k8s.io/component-base/logs/api/v1"

	"open-cluster-management.io/multicluster-controlplane/pkg/cmd/agent"
	"open-cluster-management.io/multicluster-controlplane/pkg/cmd/controller"
)

func init() {
	utilruntime.Must(logsapi.AddFeatureGates(utilfeature.DefaultMutableFeatureGate)) // register log to featuregate
}

func main() {
	command := newControlPlaneCommand()
	os.Exit(cli.Run(command))
}

func newControlPlaneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controlplane",
		Short: "Start a multicluster controlplane",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	cmd.AddCommand(controller.NewController())
	cmd.AddCommand(agent.NewAgent())

	return cmd
}
