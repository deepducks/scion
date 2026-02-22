/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/ptone/scion-agent/pkg/sciontool/hub"
	"github.com/ptone/scion-agent/pkg/sciontool/log"
)

// HubHandler sends status updates to the Scion Hub.
type HubHandler struct {
	client *hub.Client
}

// NewHubHandler creates a new hub handler.
// Returns nil if the Hub client is not configured.
func NewHubHandler() *HubHandler {
	client := hub.NewClient()
	if client == nil || !client.IsConfigured() {
		return nil
	}
	return &HubHandler{
		client: client,
	}
}

// Handle processes an event and sends a status update to the Hub.
// It mirrors the sticky status logic from StatusHandler: when the local status
// is WAITING_FOR_INPUT or COMPLETED, non-new-work events won't overwrite it.
func (h *HubHandler) Handle(event *hooks.Event) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	switch event.Name {
	case hooks.EventSessionStart:
		// Session starting - report running (clears any sticky status)
		log.Debug("Hub: Reporting running (session start)")
		err = h.client.ReportRunning(ctx, "Session started")

	case hooks.EventPromptSubmit, hooks.EventAgentStart:
		// New work events - always clear sticky status
		message := "Processing"
		if event.Data.Prompt != "" {
			message = truncateMessage(event.Data.Prompt, 100)
		}
		log.Debug("Hub: Reporting busy (thinking)")
		err = h.client.ReportBusy(ctx, message)

	case hooks.EventModelStart:
		// Model start - report busy, but respect sticky status
		// (model-start can fire during wrap-up after task completion)
		if h.isLocalStatusSticky() {
			log.Debug("Hub: Skipping busy (local status is sticky)")
			return nil
		}
		message := "Processing"
		if event.Data.Prompt != "" {
			message = truncateMessage(event.Data.Prompt, 100)
		}
		log.Debug("Hub: Reporting busy (thinking)")
		err = h.client.ReportBusy(ctx, message)

	case hooks.EventToolStart:
		// Claude-specific: ExitPlanMode and AskUserQuestion mean waiting for user
		if event.Dialect == "claude" && (event.Data.ToolName == "ExitPlanMode" || event.Data.ToolName == "AskUserQuestion") {
			message := "Waiting for input"
			if event.Data.ToolName == "ExitPlanMode" {
				message = "Waiting for plan approval"
			}
			log.Debug("Hub: Reporting waiting_for_input (waiting: %s)", event.Data.ToolName)
			err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
				Status:  hub.StatusWaitingForInput,
				Message: message,
			})
			break
		}

		// Tool-start clears WAITING_FOR_INPUT (user has responded) but
		// preserves COMPLETED (tools may fire after task_completed as wrap-up).
		localStatus := readLocalStatus()
		if localStatus == string(hooks.StateCompleted) || localStatus == string(hooks.StateLimitsExceeded) {
			log.Debug("Hub: Skipping busy (completed is sticky, post-completion tool)")
			return nil
		}

		// Agent is executing a tool
		message := "Executing tool"
		if event.Data.ToolName != "" {
			message = "Executing: " + event.Data.ToolName
		}
		log.Debug("Hub: Reporting busy (tool: %s)", event.Data.ToolName)
		err = h.client.ReportBusy(ctx, message)

	case hooks.EventToolEnd, hooks.EventAgentEnd, hooks.EventModelEnd:
		// Check if local status is sticky before sending idle
		if h.isLocalStatusSticky() {
			log.Debug("Hub: Skipping idle (local status is sticky)")
			return nil
		}
		log.Debug("Hub: Reporting idle (step completed)")
		err = h.client.ReportIdle(ctx, "Ready")

	case hooks.EventNotification:
		// Agent is waiting for input
		message := "Waiting for input"
		if event.Data.Message != "" {
			message = truncateMessage(event.Data.Message, 100)
		}
		log.Debug("Hub: Reporting waiting_for_input (notification)")
		err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
			Status:  hub.StatusWaitingForInput,
			Message: message,
		})

	case hooks.EventSessionEnd:
		// Session ended
		log.Debug("Hub: Reporting stopped (session end)")
		err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
			Status:  hub.StatusStopped,
			Message: "Session ended",
		})

	default:
		// No status update for this event
		return nil
	}

	if err != nil {
		log.Error("Hub status update failed: %v", err)
		// Don't return error - we don't want Hub failures to break the hook chain
	} else {
		log.Debug("Hub status update sent successfully")
	}

	return nil
}

// isLocalStatusSticky reads the local agent-info.json (written by StatusHandler
// which runs before HubHandler) and returns true if the status is sticky
// (WAITING_FOR_INPUT or COMPLETED).
func (h *HubHandler) isLocalStatusSticky() bool {
	status := readLocalStatus()
	return isStickyStatus(status)
}

// readLocalStatus reads the current status from the local agent-info.json file.
func readLocalStatus() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/scion"
	}
	statusPath := filepath.Join(home, "agent-info.json")

	data, err := os.ReadFile(statusPath)
	if err != nil {
		return ""
	}

	var info map[string]interface{}
	if err := json.Unmarshal(data, &info); err != nil {
		return ""
	}

	status, _ := info["status"].(string)
	return status
}

// ReportWaitingForInput sends a waiting-for-input status to the Hub.
func (h *HubHandler) ReportWaitingForInput(message string) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debug("Hub: Reporting waiting_for_input (ask_user: %s)", truncateMessage(message, 50))
	return h.client.UpdateStatus(ctx, hub.StatusUpdate{
		Status:  hub.StatusWaitingForInput,
		Message: message,
	})
}

// ReportTaskCompleted sends a task-completed status to the Hub.
func (h *HubHandler) ReportTaskCompleted(taskSummary string) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debug("Hub: Reporting task completed: %s", truncateMessage(taskSummary, 50))
	return h.client.UpdateStatus(ctx, hub.StatusUpdate{
		Status:      hub.StatusCompleted,
		TaskSummary: taskSummary,
	})
}

// truncateMessage truncates a message to the specified length.
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
