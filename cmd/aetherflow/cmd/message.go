package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var messageCmd = &cobra.Command{
	Use:     "message",
	Aliases: []string{"msg"},
	Short:   "Send and receive messages",
	Long:    `Send messages to agents/overseer and receive messages from inbox.`,
}

var messageSendCmd = &cobra.Command{
	Use:   "send <to> <summary>",
	Short: "Send a message",
	Long: `Send a message to an agent, team, or the overseer.

Examples:
  aetherflow message send overseer "Task complete, ready for review"
  aetherflow message send agent:worker-1 "Need clarification on requirements"
  aetherflow message send team:frontend "Starting integration work"`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		to := args[0]
		summary := args[1]

		lane, _ := cmd.Flags().GetString("lane")
		priority, _ := cmd.Flags().GetString("priority")
		msgType, _ := cmd.Flags().GetString("type")
		taskID, _ := cmd.Flags().GetString("task")

		fmt.Printf("Sending message:\n")
		fmt.Printf("  to: %s\n", to)
		fmt.Printf("  lane: %s\n", lane)
		fmt.Printf("  priority: %s\n", priority)
		fmt.Printf("  type: %s\n", msgType)
		if taskID != "" {
			fmt.Printf("  task: %s\n", taskID)
		}
		fmt.Printf("  summary: %s\n", summary)
		// TODO: Send message to daemon
	},
}

var messageReceiveCmd = &cobra.Command{
	Use:     "receive",
	Aliases: []string{"recv"},
	Short:   "Receive messages from inbox",
	Long: `Receive and display messages from the agent's inbox.

By default, reads from both control and task lanes (control first).
Use --lane to filter to a specific lane.`,
	Run: func(cmd *cobra.Command, args []string) {
		lane, _ := cmd.Flags().GetString("lane")
		wait, _ := cmd.Flags().GetBool("wait")
		limit, _ := cmd.Flags().GetInt("limit")

		fmt.Printf("Receiving messages: lane=%s wait=%v limit=%d\n", lane, wait, limit)
		// TODO: Query daemon for messages
		// TODO: Print messages in structured format
	},
}

var messagePeekCmd = &cobra.Command{
	Use:   "peek",
	Short: "Peek at inbox without consuming",
	Long:  `View messages in the inbox without marking them as received.`,
	Run: func(cmd *cobra.Command, args []string) {
		lane, _ := cmd.Flags().GetString("lane")
		limit, _ := cmd.Flags().GetInt("limit")

		fmt.Printf("Peeking at inbox: lane=%s limit=%d\n", lane, limit)
		// TODO: Query daemon for messages (peek mode)
	},
}

var messageAckCmd = &cobra.Command{
	Use:   "ack <message-id>",
	Short: "Acknowledge a message",
	Long:  `Mark a message as processed. Required for at-least-once delivery.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		msgID := args[0]
		fmt.Printf("Acknowledging message: %s\n", msgID)
		// TODO: Send ack to daemon
	},
}

func init() {
	rootCmd.AddCommand(messageCmd)
	messageCmd.AddCommand(messageSendCmd)
	messageCmd.AddCommand(messageReceiveCmd)
	messageCmd.AddCommand(messagePeekCmd)
	messageCmd.AddCommand(messageAckCmd)

	// Send flags
	messageSendCmd.Flags().StringP("lane", "L", "task", "Message lane (control|task)")
	messageSendCmd.Flags().StringP("priority", "P", "P1", "Priority (P0|P1|P2)")
	messageSendCmd.Flags().StringP("type", "t", "status", "Message type (assign|ack|question|blocker|status|review_ready|review_feedback|done|abandoned)")
	messageSendCmd.Flags().StringP("task", "T", "", "Associated task ID (required for lane=task)")

	// Receive flags
	messageReceiveCmd.Flags().StringP("lane", "L", "", "Filter by lane (control|task)")
	messageReceiveCmd.Flags().BoolP("wait", "w", false, "Block until a message is available")
	messageReceiveCmd.Flags().IntP("limit", "n", 1, "Maximum messages to receive")

	// Peek flags
	messagePeekCmd.Flags().StringP("lane", "L", "", "Filter by lane (control|task)")
	messagePeekCmd.Flags().IntP("limit", "n", 10, "Maximum messages to show")
}
