// Copyright Contributors to the Open Cluster Management project

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	"open-cluster-management.io/multicluster-controlplane/test/performance/cmd"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "perftool",
		Short: "A preformance test tool for controlplane",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stdout, "%v\n", err)
			}

			os.Exit(1)
		},
	}

	klog.InitFlags(nil)
	klog.LogToStderr(true)

	rootCmd.AddCommand(cmd.NewCreateCommand())
	rootCmd.AddCommand(cmd.NewCleanupCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stdout, err)
		os.Exit(1)
	}
}
