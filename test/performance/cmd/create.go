// Copyright Contributors to the Open Cluster Management project

package cmd

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/server"

	"open-cluster-management.io/multicluster-controlplane/test/performance/cluster"
)

func NewCreateCommand() *cobra.Command {
	options := cluster.NewClusterRunOptions()
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create clusters in the controlplane",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Complete(); err != nil {
				return err
			}

			if err := options.Validate(); err != nil {
				return err
			}

			shutdownCtx, cancel := context.WithCancel(context.TODO())
			shutdownHandler := server.SetupSignalHandler()
			go func() {
				defer cancel()
				<-shutdownHandler
			}()

			ctx, terminate := context.WithCancel(shutdownCtx)
			defer terminate()

			return options.Run(ctx)
		},
	}

	options.AddFlags(cmd.Flags())
	return cmd
}
