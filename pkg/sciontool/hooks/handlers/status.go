/*
Copyright 2025 The Scion Authors.
*/

// Package handlers provides hook handler implementations.
package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
)

// StatusHandler manages agent status in a JSON file.
// It replicates the functionality of scion_tool.py's update_status function.
type StatusHandler struct {
	// StatusPath is the path to the agent-info.json file.
	StatusPath string

	mu sync.Mutex
}

// NewStatusHandler creates a new status handler.
func NewStatusHandler() *StatusHandler {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/scion"
	}
	return &StatusHandler{
		StatusPath: filepath.Join(home, "agent-info.json"),
	}
}

// isStickyStatus returns true if the given status is a "sticky" value that
// should resist being overwritten by normal event-driven updates.
func isStickyStatus(status string) bool {
	switch status {
	case string(hooks.StateWaitingForInput), string(hooks.StateCompleted), string(hooks.StateLimitsExceeded):
		return true
	}
	return false
}

// Handle processes an event and updates the agent status.
func (h *StatusHandler) Handle(event *hooks.Event) error {
	state := h.eventToState(event)
	if state == "" {
		return nil // Event doesn't trigger a state change
	}

	// New work events (prompt-submit, agent-start, session-start): always
	// update status unconditionally — clears any sticky state.
	if isNewWorkEvent(event.Name) {
		return h.UpdateStatus(state)
	}

	// Tool-start events require special handling for sticky states.
	if event.Name == hooks.EventToolStart {
		// Claude-specific: ExitPlanMode and AskUserQuestion set WAITING_FOR_INPUT (sticky).
		if event.Dialect == "claude" && (event.Data.ToolName == "ExitPlanMode" || event.Data.ToolName == "AskUserQuestion") {
			return h.UpdateStatus(hooks.StateWaitingForInput)
		}

		// Tool-start clears WAITING_FOR_INPUT (user has responded) but
		// preserves COMPLETED (tools may fire after task_completed as wrap-up).
		return h.updateStatusIfNotSticky(state, true)
	}

	// Notification event: set WAITING_FOR_INPUT directly (sticky).
	if event.Name == hooks.EventNotification {
		return h.UpdateStatus(hooks.StateWaitingForInput)
	}

	// All other events (tool-end, agent-end, model-end, etc.): update status
	// only if current status is not sticky.
	return h.updateStatusIfNotSticky(state, false)
}

// UpdateStatus writes the status to the agent-info.json file atomically.
func (h *StatusHandler) UpdateStatus(status hooks.AgentState) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Read existing data preserving all fields
	info := h.readAgentInfoMap()

	// Update the status field
	if status == "" {
		delete(info, "status")
	} else {
		info["status"] = string(status)
	}

	// Remove legacy sessionStatus field if present
	delete(info, "sessionStatus")

	return h.writeAgentInfoLocked(info)
}

// updateStatusIfNotSticky updates the status only if the current status is not
// sticky. If clearWaiting is true, WAITING_FOR_INPUT is also cleared (treated
// as non-sticky for tool-start events where the user has responded).
func (h *StatusHandler) updateStatusIfNotSticky(status hooks.AgentState, clearWaiting bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	info := h.readAgentInfoMap()

	currentStatus, _ := info["status"].(string)

	if isStickyStatus(currentStatus) {
		if clearWaiting && currentStatus == string(hooks.StateWaitingForInput) {
			// WAITING_FOR_INPUT is cleared by tool-start (user has responded)
			info["status"] = string(status)
			delete(info, "sessionStatus")
			return h.writeAgentInfoLocked(info)
		}
		return nil // Status is sticky, don't overwrite
	}

	info["status"] = string(status)
	delete(info, "sessionStatus")
	return h.writeAgentInfoLocked(info)
}

// readAgentInfoMap reads agent-info.json into a generic map, preserving all fields.
// Caller must hold h.mu.
func (h *StatusHandler) readAgentInfoMap() map[string]interface{} {
	info := make(map[string]interface{})
	if data, err := os.ReadFile(h.StatusPath); err == nil {
		_ = json.Unmarshal(data, &info)
	}
	return info
}

// writeAgentInfoLocked writes the agent info map to disk atomically.
// Caller must hold h.mu.
func (h *StatusHandler) writeAgentInfoLocked(info map[string]interface{}) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling status: %w", err)
	}

	dir := filepath.Dir(h.StatusPath)
	tmpFile, err := os.CreateTemp(dir, "agent-info-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, h.StatusPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic rename: %w", err)
	}

	return nil
}

// isNewWorkEvent returns true for events that indicate new work is starting.
// These events unconditionally update status, clearing any sticky state.
func isNewWorkEvent(name string) bool {
	switch name {
	case hooks.EventPromptSubmit, hooks.EventAgentStart, hooks.EventSessionStart:
		return true
	}
	return false
}

// eventToState maps normalized events to agent states.
func (h *StatusHandler) eventToState(event *hooks.Event) hooks.AgentState {
	switch event.Name {
	case hooks.EventSessionStart:
		return hooks.StateStarting

	case hooks.EventPreStart:
		return hooks.StateInitializing

	case hooks.EventPostStart:
		return hooks.StateIdle

	case hooks.EventPreStop:
		return hooks.StateShuttingDown

	case hooks.EventPromptSubmit, hooks.EventAgentStart:
		return hooks.StateThinking

	case hooks.EventModelStart:
		return hooks.StateThinking

	case hooks.EventModelEnd:
		return hooks.StateIdle

	case hooks.EventToolStart:
		// Include tool name in state if available
		if event.Data.ToolName != "" {
			// Return a dynamic state - caller should handle formatting
			return hooks.StateExecuting
		}
		return hooks.StateExecuting

	case hooks.EventToolEnd, hooks.EventAgentEnd:
		return hooks.StateIdle

	case hooks.EventNotification:
		return hooks.StateWaitingForInput

	case hooks.EventSessionEnd:
		return hooks.StateExited

	default:
		return "" // No state change
	}
}

// GetFormattedState returns the state with tool name if applicable.
func (h *StatusHandler) GetFormattedState(event *hooks.Event) string {
	state := h.eventToState(event)
	if state == hooks.StateExecuting && event.Data.ToolName != "" {
		return fmt.Sprintf("%s (%s)", state, event.Data.ToolName)
	}
	return string(state)
}
