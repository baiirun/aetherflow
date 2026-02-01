package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
	Long:  `Register, list, and manage agents in the aetherflow system.`,
}

var agentRegisterCmd = &cobra.Command{
	Use:   "register [name]",
	Short: "Register a new agent",
	Long: `Register a new agent with the daemon.

The daemon assigns an agent ID on successful registration. If a name is
provided, it will be used as a human-readable identifier.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}

		labels, _ := cmd.Flags().GetStringSlice("label")
		capacity, _ := cmd.Flags().GetInt("capacity")

		fmt.Printf("Registering agent: name=%q labels=%v capacity=%d\n", name, labels, capacity)
		// TODO: Send registration request to daemon
		// TODO: Print assigned agent ID
	},
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered agents",
	Long:  `List all agents currently registered with the daemon.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Listing agents...")
		// TODO: Query daemon for registered agents
		fmt.Println("ID\t\tNAME\t\tSTATE\t\tQUEUE")
		fmt.Println("agent-001\tworker-1\tidle\t\t0/5")
	},
}

var agentUnregisterCmd = &cobra.Command{
	Use:   "unregister <agent-id>",
	Short: "Unregister an agent",
	Long:  `Unregister an agent from the daemon. Pending messages will be requeued.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		agentID := args[0]
		fmt.Printf("Unregistering agent: %s\n", agentID)
		// TODO: Send unregister request to daemon
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentRegisterCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentUnregisterCmd)

	agentRegisterCmd.Flags().StringSliceP("label", "l", nil, "Labels for the agent (can be repeated)")
	agentRegisterCmd.Flags().IntP("capacity", "n", 5, "Inbox capacity (max queued tasks)")
}
