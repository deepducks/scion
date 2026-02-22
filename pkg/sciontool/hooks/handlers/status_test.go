/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusHandler_UpdateStatus(t *testing.T) {
	// Create temp dir
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	h := &StatusHandler{
		StatusPath: statusPath,
	}

	// Test updating status
	err := h.UpdateStatus(hooks.StateThinking)
	require.NoError(t, err)

	// Verify file contents
	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "THINKING", info.Status)

	// Test updating to sticky status (WAITING_FOR_INPUT)
	err = h.UpdateStatus(hooks.StateWaitingForInput)
	require.NoError(t, err)

	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status)
}

func TestStatusHandler_Handle(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	h := &StatusHandler{
		StatusPath: statusPath,
	}

	tests := []struct {
		name       string
		event      *hooks.Event
		wantStatus hooks.AgentState
	}{
		{
			name:       "SessionStart sets STARTING",
			event:      &hooks.Event{Name: hooks.EventSessionStart},
			wantStatus: hooks.StateStarting,
		},
		{
			name:       "PreStart sets INITIALIZING",
			event:      &hooks.Event{Name: hooks.EventPreStart},
			wantStatus: hooks.StateInitializing,
		},
		{
			name:       "PostStart sets IDLE",
			event:      &hooks.Event{Name: hooks.EventPostStart},
			wantStatus: hooks.StateIdle,
		},
		{
			name:       "PreStop sets SHUTTING_DOWN",
			event:      &hooks.Event{Name: hooks.EventPreStop},
			wantStatus: hooks.StateShuttingDown,
		},
		{
			name:       "PromptSubmit sets THINKING",
			event:      &hooks.Event{Name: hooks.EventPromptSubmit},
			wantStatus: hooks.StateThinking,
		},
		{
			name:       "ToolStart sets EXECUTING",
			event:      &hooks.Event{Name: hooks.EventToolStart, Data: hooks.EventData{ToolName: "Bash"}},
			wantStatus: hooks.StateExecuting,
		},
		{
			name:       "ToolEnd sets IDLE",
			event:      &hooks.Event{Name: hooks.EventToolEnd},
			wantStatus: hooks.StateIdle,
		},
		{
			name:       "AgentEnd sets IDLE",
			event:      &hooks.Event{Name: hooks.EventAgentEnd},
			wantStatus: hooks.StateIdle,
		},
		{
			name:       "SessionEnd sets EXITED",
			event:      &hooks.Event{Name: hooks.EventSessionEnd},
			wantStatus: hooks.StateExited,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.Handle(tt.event)
			require.NoError(t, err)

			info := readAgentInfo(t, statusPath)
			assert.Equal(t, string(tt.wantStatus), info.Status)
		})
	}
}

func TestStatusHandler_StickyWaitingClearedByToolStart(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	h := &StatusHandler{StatusPath: statusPath}

	// Set status to WAITING_FOR_INPUT (sticky)
	err := h.UpdateStatus(hooks.StateWaitingForInput)
	require.NoError(t, err)

	// Verify it's set
	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status)

	// Tool-start should clear WAITING_FOR_INPUT (user has responded)
	err = h.Handle(&hooks.Event{
		Name: hooks.EventToolStart,
		Data: hooks.EventData{ToolName: "Bash"},
	})
	require.NoError(t, err)

	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "EXECUTING", info.Status, "tool-start should clear WAITING_FOR_INPUT")
}

func TestStatusHandler_StickyCompletedNotClearedByToolStart(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	h := &StatusHandler{StatusPath: statusPath}

	// Set status to COMPLETED (sticky)
	err := h.UpdateStatus(hooks.StateCompleted)
	require.NoError(t, err)

	// Tool-start should NOT clear COMPLETED
	err = h.Handle(&hooks.Event{
		Name: hooks.EventToolStart,
		Data: hooks.EventData{ToolName: "Bash"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "COMPLETED should not be cleared by tool-start")
}

func TestStatusHandler_Handle_ClearsWaitingOnActivity(t *testing.T) {
	activityEvents := []struct {
		name  string
		event *hooks.Event
	}{
		{
			name:  "ToolStart clears waiting",
			event: &hooks.Event{Name: hooks.EventToolStart, Data: hooks.EventData{ToolName: "Bash"}},
		},
		{
			name:  "PromptSubmit clears waiting",
			event: &hooks.Event{Name: hooks.EventPromptSubmit},
		},
		{
			name:  "AgentStart clears waiting",
			event: &hooks.Event{Name: hooks.EventAgentStart},
		},
	}

	for _, tt := range activityEvents {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statusPath := filepath.Join(tmpDir, "agent-info.json")
			h := &StatusHandler{StatusPath: statusPath}

			// Pre-set status to WAITING_FOR_INPUT
			err := h.UpdateStatus(hooks.StateWaitingForInput)
			require.NoError(t, err)

			// Handle the activity event
			err = h.Handle(tt.event)
			require.NoError(t, err)

			info := readAgentInfo(t, statusPath)
			assert.NotEqual(t, "WAITING_FOR_INPUT", info.Status, "WAITING_FOR_INPUT should be cleared")
		})
	}
}

func TestStatusHandler_Handle_DoesNotClearCompletedOnToolStart(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Pre-set status to COMPLETED
	err := h.UpdateStatus(hooks.StateCompleted)
	require.NoError(t, err)

	// Handle a tool-start event — tools may fire after task_completed as wrap-up
	err = h.Handle(&hooks.Event{
		Name: hooks.EventToolStart,
		Data: hooks.EventData{ToolName: "Bash"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "COMPLETED should not be cleared by tool-start")
}

func TestStatusHandler_Handle_DoesNotClearCompletedOnAgentEnd(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Pre-set status to COMPLETED
	err := h.UpdateStatus(hooks.StateCompleted)
	require.NoError(t, err)

	// Handle agent-end events — should not clear COMPLETED
	err = h.Handle(&hooks.Event{Name: hooks.EventAgentEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "COMPLETED should not be cleared by agent-end")

	// Second agent-end (e.g., SubagentStop)
	err = h.Handle(&hooks.Event{Name: hooks.EventAgentEnd})
	require.NoError(t, err)

	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "COMPLETED should survive multiple agent-end events")
}

func TestStatusHandler_Handle_DoesNotClearCompletedOnToolEnd(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Pre-set status to COMPLETED
	err := h.UpdateStatus(hooks.StateCompleted)
	require.NoError(t, err)

	// Handle tool-end event
	err = h.Handle(&hooks.Event{Name: hooks.EventToolEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "COMPLETED should not be cleared by tool-end")
}

func TestStatusHandler_Handle_ClearsCompletedOnNewWork(t *testing.T) {
	newWorkEvents := []struct {
		name  string
		event *hooks.Event
	}{
		{
			name:  "PromptSubmit clears completed",
			event: &hooks.Event{Name: hooks.EventPromptSubmit},
		},
		{
			name:  "AgentStart clears completed",
			event: &hooks.Event{Name: hooks.EventAgentStart},
		},
		{
			name:  "SessionStart clears completed",
			event: &hooks.Event{Name: hooks.EventSessionStart},
		},
	}

	for _, tt := range newWorkEvents {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statusPath := filepath.Join(tmpDir, "agent-info.json")
			h := &StatusHandler{StatusPath: statusPath}

			// Pre-set status to COMPLETED
			err := h.UpdateStatus(hooks.StateCompleted)
			require.NoError(t, err)

			// Handle the new-work event
			err = h.Handle(tt.event)
			require.NoError(t, err)

			info := readAgentInfo(t, statusPath)
			assert.NotEqual(t, "COMPLETED", info.Status, "COMPLETED should be cleared by new work event")
		})
	}
}

func TestStatusHandler_Handle_CompletedLifecycle(t *testing.T) {
	// Simulate the full lifecycle: task completes, wrap-up tools fire,
	// agent stops, then new prompt arrives.
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// 1. Agent completes task
	err := h.UpdateStatus(hooks.StateCompleted)
	require.NoError(t, err)
	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status)

	// 2. Wrap-up tool fires (e.g., TaskUpdate)
	err = h.Handle(&hooks.Event{
		Name: hooks.EventToolStart,
		Data: hooks.EventData{ToolName: "TaskUpdate"},
	})
	require.NoError(t, err)
	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "should survive tool-start")

	// 3. Tool completes
	err = h.Handle(&hooks.Event{Name: hooks.EventToolEnd})
	require.NoError(t, err)
	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "should survive tool-end")

	// 4. Agent turn ends (Stop event)
	err = h.Handle(&hooks.Event{Name: hooks.EventAgentEnd})
	require.NoError(t, err)
	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "should survive agent-end")

	// 5. Another Stop event (SubagentStop)
	err = h.Handle(&hooks.Event{Name: hooks.EventAgentEnd})
	require.NoError(t, err)
	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "COMPLETED", info.Status, "should survive second agent-end")

	// 6. New prompt arrives — COMPLETED should now be cleared
	err = h.Handle(&hooks.Event{Name: hooks.EventPromptSubmit})
	require.NoError(t, err)
	info = readAgentInfo(t, statusPath)
	assert.NotEqual(t, "COMPLETED", info.Status, "should be cleared by new prompt")
	assert.Equal(t, "THINKING", info.Status)
}

func TestStatusHandler_Handle_ToolEndDoesNotClearWaiting(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Pre-set status to WAITING_FOR_INPUT
	err := h.UpdateStatus(hooks.StateWaitingForInput)
	require.NoError(t, err)

	// Handle a tool-end event (should NOT clear)
	err = h.Handle(&hooks.Event{Name: hooks.EventToolEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status, "tool-end should not clear waiting")
}

func TestStatusHandler_Handle_ClaudeExitPlanMode(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Handle ExitPlanMode tool-start from Claude dialect
	err := h.Handle(&hooks.Event{
		Name:    hooks.EventToolStart,
		Dialect: "claude",
		Data:    hooks.EventData{ToolName: "ExitPlanMode"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status)
}

func TestStatusHandler_Handle_ClaudeAskUserQuestion(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Pre-set status to WAITING_FOR_INPUT (simulating sciontool status ask_user)
	err := h.UpdateStatus(hooks.StateWaitingForInput)
	require.NoError(t, err)

	// Handle AskUserQuestion tool-start from Claude dialect
	err = h.Handle(&hooks.Event{
		Name:    hooks.EventToolStart,
		Dialect: "claude",
		Data:    hooks.EventData{ToolName: "AskUserQuestion"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status, "AskUserQuestion should maintain WAITING_FOR_INPUT")
}

func TestStatusHandler_Handle_NonClaudeExitPlanModeIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Handle ExitPlanMode from a non-claude dialect — should NOT set WAITING_FOR_INPUT
	err := h.Handle(&hooks.Event{
		Name:    hooks.EventToolStart,
		Dialect: "gemini",
		Data:    hooks.EventData{ToolName: "ExitPlanMode"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "EXECUTING", info.Status, "non-claude ExitPlanMode should set EXECUTING, not WAITING_FOR_INPUT")
}

func TestStatusHandler_Handle_ClaudeExitPlanModeThenActivity(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// ExitPlanMode sets WAITING_FOR_INPUT
	err := h.Handle(&hooks.Event{
		Name:    hooks.EventToolStart,
		Dialect: "claude",
		Data:    hooks.EventData{ToolName: "ExitPlanMode"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status)

	// Tool-end for ExitPlanMode should NOT clear it (sticky)
	err = h.Handle(&hooks.Event{Name: hooks.EventToolEnd, Dialect: "claude"})
	require.NoError(t, err)

	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status)

	// User approves plan, next tool starts — should clear WAITING_FOR_INPUT
	err = h.Handle(&hooks.Event{
		Name:    hooks.EventToolStart,
		Dialect: "claude",
		Data:    hooks.EventData{ToolName: "Bash"},
	})
	require.NoError(t, err)

	info = readAgentInfo(t, statusPath)
	assert.Equal(t, "EXECUTING", info.Status, "activity after plan approval should clear WAITING_FOR_INPUT")
}

func TestStatusHandler_PreservesExtraFields(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	// Seed agent-info.json with extra fields (as written at provisioning time)
	initial := map[string]interface{}{
		"status":        "running",
		"template":      "my-template",
		"harnessConfig": "claude",
		"runtime":       "docker",
		"grove":         "my-grove",
		"profile":       "default",
		"name":          "agent-1",
	}
	data, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statusPath, data, 0644))

	h := &StatusHandler{StatusPath: statusPath}

	// Update status — this should NOT destroy the extra fields
	err = h.UpdateStatus(hooks.StateThinking)
	require.NoError(t, err)

	result := readAgentInfoMap(t, statusPath)
	assert.Equal(t, "THINKING", result["status"])
	assert.Equal(t, "my-template", result["template"], "template field should be preserved")
	assert.Equal(t, "claude", result["harnessConfig"], "harnessConfig field should be preserved")
	assert.Equal(t, "docker", result["runtime"], "runtime field should be preserved")
	assert.Equal(t, "my-grove", result["grove"], "grove field should be preserved")
	assert.Equal(t, "default", result["profile"], "profile field should be preserved")
	assert.Equal(t, "agent-1", result["name"], "name field should be preserved")

	// Update to WAITING_FOR_INPUT — extra fields should still be there
	err = h.UpdateStatus(hooks.StateWaitingForInput)
	require.NoError(t, err)

	result = readAgentInfoMap(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", result["status"])
	assert.Equal(t, "my-template", result["template"], "template field should survive status update")
	assert.Equal(t, "claude", result["harnessConfig"], "harnessConfig field should survive status update")
}

func TestStatusHandler_RemovesLegacySessionStatus(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")

	// Seed agent-info.json with legacy sessionStatus field
	initial := map[string]interface{}{
		"status":        "running",
		"sessionStatus": "WAITING_FOR_INPUT",
	}
	data, err := json.Marshal(initial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statusPath, data, 0644))

	h := &StatusHandler{StatusPath: statusPath}

	// Any UpdateStatus call should remove the legacy sessionStatus field
	err = h.UpdateStatus(hooks.StateThinking)
	require.NoError(t, err)

	result := readAgentInfoMap(t, statusPath)
	assert.Equal(t, "THINKING", result["status"])
	assert.Nil(t, result["sessionStatus"], "legacy sessionStatus should be removed")
}

func TestStatusHandler_LimitsExceededIsStickyAgainstToolStart(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Set status to LIMITS_EXCEEDED (sticky)
	err := h.UpdateStatus(hooks.StateLimitsExceeded)
	require.NoError(t, err)

	// Tool-start should NOT clear LIMITS_EXCEEDED
	err = h.Handle(&hooks.Event{
		Name: hooks.EventToolStart,
		Data: hooks.EventData{ToolName: "Bash"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "LIMITS_EXCEEDED", info.Status, "LIMITS_EXCEEDED should not be cleared by tool-start")
}

func TestStatusHandler_LimitsExceededIsStickyAgainstToolEnd(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	err := h.UpdateStatus(hooks.StateLimitsExceeded)
	require.NoError(t, err)

	err = h.Handle(&hooks.Event{Name: hooks.EventToolEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "LIMITS_EXCEEDED", info.Status, "LIMITS_EXCEEDED should not be cleared by tool-end")
}

func TestStatusHandler_LimitsExceededIsStickyAgainstAgentEnd(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	err := h.UpdateStatus(hooks.StateLimitsExceeded)
	require.NoError(t, err)

	err = h.Handle(&hooks.Event{Name: hooks.EventAgentEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "LIMITS_EXCEEDED", info.Status, "LIMITS_EXCEEDED should not be cleared by agent-end")
}

func TestStatusHandler_LimitsExceededNotClearedByCompleted(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Set LIMITS_EXCEEDED
	err := h.UpdateStatus(hooks.StateLimitsExceeded)
	require.NoError(t, err)

	// Attempt to set COMPLETED via a non-new-work path (model-end maps to IDLE, not COMPLETED,
	// but UpdateStatus directly can be called by other code)
	// The key test: tool-end/agent-end should not overwrite LIMITS_EXCEEDED
	err = h.Handle(&hooks.Event{Name: hooks.EventModelEnd})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "LIMITS_EXCEEDED", info.Status, "LIMITS_EXCEEDED should not be cleared by model-end")
}

func TestStatusHandler_LimitsExceededClearedByNewWork(t *testing.T) {
	newWorkEvents := []struct {
		name  string
		event *hooks.Event
	}{
		{
			name:  "PromptSubmit clears limits_exceeded",
			event: &hooks.Event{Name: hooks.EventPromptSubmit},
		},
		{
			name:  "AgentStart clears limits_exceeded",
			event: &hooks.Event{Name: hooks.EventAgentStart},
		},
		{
			name:  "SessionStart clears limits_exceeded",
			event: &hooks.Event{Name: hooks.EventSessionStart},
		},
	}

	for _, tt := range newWorkEvents {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			statusPath := filepath.Join(tmpDir, "agent-info.json")
			h := &StatusHandler{StatusPath: statusPath}

			// Pre-set status to LIMITS_EXCEEDED
			err := h.UpdateStatus(hooks.StateLimitsExceeded)
			require.NoError(t, err)

			// Handle the new-work event
			err = h.Handle(tt.event)
			require.NoError(t, err)

			info := readAgentInfo(t, statusPath)
			assert.NotEqual(t, "LIMITS_EXCEEDED", info.Status, "LIMITS_EXCEEDED should be cleared by new work event")
		})
	}
}

func TestStatusHandler_NotificationSetsWaitingForInput(t *testing.T) {
	tmpDir := t.TempDir()
	statusPath := filepath.Join(tmpDir, "agent-info.json")
	h := &StatusHandler{StatusPath: statusPath}

	// Handle notification event
	err := h.Handle(&hooks.Event{
		Name: hooks.EventNotification,
		Data: hooks.EventData{Message: "Please confirm"},
	})
	require.NoError(t, err)

	info := readAgentInfo(t, statusPath)
	assert.Equal(t, "WAITING_FOR_INPUT", info.Status, "notification should set WAITING_FOR_INPUT")
}

// agentInfoFields is a test-only struct for reading status fields from agent-info.json.
type agentInfoFields struct {
	Status string `json:"status,omitempty"`
}

// readAgentInfo is a test helper that reads and parses agent-info.json.
func readAgentInfo(t *testing.T, path string) agentInfoFields {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var info agentInfoFields
	err = json.Unmarshal(data, &info)
	require.NoError(t, err)
	return info
}

// readAgentInfoMap is a test helper that reads agent-info.json as a raw map.
func readAgentInfoMap(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var info map[string]interface{}
	err = json.Unmarshal(data, &info)
	require.NoError(t, err)
	return info
}
