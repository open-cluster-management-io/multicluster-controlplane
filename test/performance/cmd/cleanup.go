// Copyright Contributors to the Open Cluster Management project

package cmd

import (
	"github.com/spf13/cobra"

	"open-cluster-management.io/multicluster-controlplane/test/performance/cluster"
)

func NewCleanupCommand() *cobra.Command {
	options := cluster.NewClusterCleanupOptions()
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Cleanup clusters in the controlplane",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(); err != nil {
				return err
			}
			return options.Run()
		},
	}
	options.AddFlags(cmd.Flags())
	return cmd
}
