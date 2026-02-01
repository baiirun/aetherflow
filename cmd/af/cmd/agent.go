package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/geobrowser/aetherflow/internal/client"
	"github.com/geobrowser/aetherflow/internal/protocol"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
	Long:  `Register, list, and manage agents.`,
}

var agentRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new agent",
	Long:  `Register a new agent with the daemon and receive an assigned ID.`,
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New("")
		resp, err := c.Register()
		if err != nil {
			Fatal("%v", err)
		}
		fmt.Println(resp.AgentID)
	},
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered agents",
	Run: func(cmd *cobra.Command, args []string) {
		c := client.New("")
		agents, err := c.ListAgents()
		if err != nil {
			Fatal("%v", err)
		}

		if len(agents) == 0 {
			fmt.Println("No agents registered")
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATE\tTASK")
		for _, a := range agents {
			task := a.CurrentTask
			if task == "" {
				task = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", a.ID, a.State, task)
		}
		w.Flush()
	},
}

var agentUnregisterCmd = &cobra.Command{
	Use:   "unregister <agent-id>",
	Short: "Unregister an agent",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		agentID := protocol.AgentID(args[0])

		c := client.New("")
		if err := c.Unregister(agentID); err != nil {
			Fatal("%v", err)
		}

		fmt.Printf("Unregistered: %s\n", agentID)
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentRegisterCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentUnregisterCmd)
}
