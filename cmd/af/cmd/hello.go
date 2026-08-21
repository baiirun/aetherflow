package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var helloCmd = &cobra.Command{
	Use:   "hello",
	Short: "Print a greeting",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "hello world")
	},
}

func init() {
	rootCmd.AddCommand(helloCmd)
}
