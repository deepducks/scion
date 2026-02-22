/*
Copyright 2025 The Scion Authors.
*/

package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/ptone/scion-agent/pkg/sciontool/hooks/handlers"
	"github.com/ptone/scion-agent/pkg/sciontool/hub"
	"github.com/ptone/scion-agent/pkg/sciontool/log"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status <status-type> <message>",
	Short: "Update agent status",
	Long: `The status command updates the agent's session status and logs the event.

This is used by agents to signal state changes to the scion orchestrator.

Status Types:
  ask_user         Signal that the agent is waiting for user input
  task_completed   Signal that the agent has completed its task
  limits_exceeded  Signal that the agent has exceeded its configured limits

Examples:
  # Signal waiting for user input
  sciontool status ask_user "What should I do next?"

  # Signal task completion
  sciontool status task_completed "Implemented feature X"

  # Signal limits exceeded
  sciontool status limits_exceeded "max_turns of 50 exceeded"`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		statusType := args[0]
		message := strings.Join(args[1:], " ")

		switch statusType {
		case "ask_user":
			if message == "" {
				message = "Input requested"
			}
			runStatusAskUser(message)
		case "task_completed":
			if message == "" {
				message = "Task completed"
			}
			runStatusTaskCompleted(message)
		case "limits_exceeded":
			if message == "" {
				message = "Agent limits exceeded"
			}
			runStatusLimitsExceeded(message)
		default:
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: unknown status type %q\n", statusType)
			fmt.Fprintf(cmd.ErrOrStderr(), "Valid types: ask_user, task_completed, limits_exceeded\n")
			cmd.Root().SetArgs([]string{"status", "--help"})
			cmd.Root().Execute()
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

// runStatusAskUser updates status to waiting for input.
func runStatusAskUser(message string) {
	statusHandler := handlers.NewStatusHandler()
	loggingHandler := handlers.NewLoggingHandler()

	// Update status to waiting for input (sticky)
	if err := statusHandler.UpdateStatus(hooks.StateWaitingForInput); err != nil {
		log.Error("Failed to update status: %v", err)
	}

	// Log the event
	logMessage := fmt.Sprintf("Agent requested input: %s", message)
	if err := loggingHandler.LogEvent(hooks.StateWaitingForInput, logMessage); err != nil {
		log.Error("Failed to log event: %v", err)
	}

	// Report to Hub if configured
	if hubClient := hub.NewClient(); hubClient != nil && hubClient.IsConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hubClient.UpdateStatus(ctx, hub.StatusUpdate{
			Status:  hub.StatusWaitingForInput,
			Message: message,
		}); err != nil {
			log.Error("Failed to report to Hub: %v", err)
		}
	}

	log.Info("Agent asked: %s", message)
}

// runStatusLimitsExceeded updates status to limits exceeded.
func runStatusLimitsExceeded(message string) {
	statusHandler := handlers.NewStatusHandler()
	loggingHandler := handlers.NewLoggingHandler()

	// Update status to limits exceeded (sticky)
	if err := statusHandler.UpdateStatus(hooks.StateLimitsExceeded); err != nil {
		log.Error("Failed to update status: %v", err)
	}

	// Log the event
	logMessage := fmt.Sprintf("Agent limits exceeded: %s", message)
	if err := loggingHandler.LogEvent(hooks.StateLimitsExceeded, logMessage); err != nil {
		log.Error("Failed to log event: %v", err)
	}

	// Report to Hub if configured
	if hubClient := hub.NewClient(); hubClient != nil && hubClient.IsConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hubClient.ReportLimitsExceeded(ctx, message); err != nil {
			log.Error("Failed to report to Hub: %v", err)
		}
	}

	log.Info("Agent limits exceeded: %s", message)
}

// runStatusTaskCompleted updates status to completed.
func runStatusTaskCompleted(message string) {
	statusHandler := handlers.NewStatusHandler()
	loggingHandler := handlers.NewLoggingHandler()

	// Update status to completed (sticky)
	if err := statusHandler.UpdateStatus(hooks.StateCompleted); err != nil {
		log.Error("Failed to update status: %v", err)
	}

	// Log the event
	logMessage := fmt.Sprintf("Agent completed task: %s", message)
	if err := loggingHandler.LogEvent(hooks.StateCompleted, logMessage); err != nil {
		log.Error("Failed to log event: %v", err)
	}

	// Report to Hub if in hosted mode
	if hubClient := hub.NewClient(); hubClient != nil && hubClient.IsConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hubClient.ReportTaskCompleted(ctx, message); err != nil {
			log.Error("Failed to report to Hub: %v", err)
		}
	}

	log.Info("Agent completed: %s", message)
}
