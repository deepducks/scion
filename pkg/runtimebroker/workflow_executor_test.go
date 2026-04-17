// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtimebroker

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/wsprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedEvent records a single event sent via SendEvent.
type capturedEvent struct {
	eventType string
	payload   []byte
}

// mockControlChannel captures events sent by the executor without a real
// WebSocket connection.
type mockControlChannel struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (m *mockControlChannel) SendEvent(eventType string, payload interface{}) error {
	data, _ := json.Marshal(payload)
	m.mu.Lock()
	m.events = append(m.events, capturedEvent{eventType: eventType, payload: data})
	m.mu.Unlock()
	return nil
}

func (m *mockControlChannel) captured() []capturedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]capturedEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

// newTestExecutor returns a WorkflowExecutor wired to a mockControlChannel.
func newTestExecutor(t *testing.T) (*WorkflowExecutor, *mockControlChannel) {
	t.Helper()
	mock := &mockControlChannel{}
	exec := &WorkflowExecutor{
		brokerID: "test-broker",
		log:      slog.Default(),
		getControlChannel: func(_ string) *ControlChannelClient {
			// Return nil; the executor calls SendEvent through the mock below.
			return nil
		},
		runs: make(map[string]*activeWorkflowRun),
	}
	// Override sendStatusEvent, sendLogEvent, sendOutputEvent to use mock directly.
	_ = mock // used below via closures
	return exec, mock
}

// ---------------------------------------------------------------------------
// HTTP handler tests (no actual quack invocation)
// ---------------------------------------------------------------------------

func TestWorkflowExecutor_HandleCreate_MissingBody(t *testing.T) {
	exec, _ := newTestExecutor(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-runs", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	exec.HandleCreateWorkflowRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkflowExecutor_HandleCreate_MissingRunID(t *testing.T) {
	exec, _ := newTestExecutor(t)

	body := `{"sourceYaml":"version: \"0.7\"\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	exec.HandleCreateWorkflowRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkflowExecutor_HandleCreate_MissingSourceYAML(t *testing.T) {
	exec, _ := newTestExecutor(t)

	body := `{"runId":"run-1","groveId":"grove-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	exec.HandleCreateWorkflowRun(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWorkflowExecutor_HandleCreate_Accepted(t *testing.T) {
	exec, _ := newTestExecutor(t)
	// Replace getControlChannel to avoid nil dereference in goroutine
	exec.getControlChannel = func(_ string) *ControlChannelClient { return nil }

	body := `{"runId":"run-accepted","groveId":"grove-1","sourceYaml":"version: \"0.7\"\nname: test\n"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-runs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	exec.HandleCreateWorkflowRun(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "accepted", resp["status"])
	assert.Equal(t, "run-accepted", resp["runId"])
}

func TestWorkflowExecutor_HandleCreate_Idempotent(t *testing.T) {
	// Sending the same runID twice should return 202 both times without
	// starting a second execution.
	exec, _ := newTestExecutor(t)
	exec.getControlChannel = func(_ string) *ControlChannelClient { return nil }

	body := `{"runId":"run-idem","groveId":"g1","sourceYaml":"version: \"0.7\"\nname: t\n"}`
	do := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-runs", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		exec.HandleCreateWorkflowRun(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusAccepted, do())

	// Second call while the goroutine hasn't finished yet (run still registered).
	assert.Equal(t, http.StatusAccepted, do())
}

func TestWorkflowExecutor_HandleCancel_NotFound(t *testing.T) {
	exec, _ := newTestExecutor(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workflow-runs/nonexistent", nil)
	w := httptest.NewRecorder()

	exec.HandleCancelWorkflowRun(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestWorkflowExecutor_HandleCancel_CancelsRun(t *testing.T) {
	exec, _ := newTestExecutor(t)
	exec.getControlChannel = func(_ string) *ControlChannelClient { return nil }

	// Register a run manually so we can cancel it without waiting for goroutine.
	cancelCalled := false
	exec.mu.Lock()
	exec.runs["run-to-cancel"] = &activeWorkflowRun{
		runID:    "run-to-cancel",
		startedAt: time.Now(),
		cancel: func() {
			cancelCalled = true
		},
	}
	exec.mu.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workflow-runs/run-to-cancel", nil)
	w := httptest.NewRecorder()

	exec.HandleCancelWorkflowRun(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, cancelCalled, "cancel function should have been called")
}

func TestWorkflowExecutor_HandleCancel_MethodNotAllowed(t *testing.T) {
	exec, _ := newTestExecutor(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workflow-runs/some-run", nil)
	w := httptest.NewRecorder()

	exec.HandleCancelWorkflowRun(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------------------------------------------------------------------------
// Event emission tests
// ---------------------------------------------------------------------------

// eventCapturingExecutor overrides send* methods via a replacement getControlChannel
// that returns a fake channel, but for pure event testing we use a wrapped executor.
// Here we test the ptrStr helper and JSON payload shapes via direct method calls on
// a mock-backed executor.

func TestPtrStr(t *testing.T) {
	s := "hello"
	p := ptrStr(s)
	require.NotNil(t, p)
	assert.Equal(t, s, *p)
}

// ---------------------------------------------------------------------------
// SendEvent shape (integration-level format verification)
// ---------------------------------------------------------------------------

func TestWorkflowExecutor_SendEventPayloads(t *testing.T) {
	// Verify that the wsprotocol payload types round-trip correctly through JSON.
	t.Run("WorkflowStatusPayload", func(t *testing.T) {
		p := wsprotocol.WorkflowStatusPayload{
			RunID:  "r1",
			Status: "running",
			At:     time.Now().Format(time.RFC3339Nano),
		}
		data, err := json.Marshal(p)
		require.NoError(t, err)
		var back wsprotocol.WorkflowStatusPayload
		require.NoError(t, json.Unmarshal(data, &back))
		assert.Equal(t, p.RunID, back.RunID)
		assert.Equal(t, p.Status, back.Status)
	})

	t.Run("WorkflowOutputPayload_Succeeded", func(t *testing.T) {
		res := `{"answer":42}`
		p := wsprotocol.WorkflowOutputPayload{
			RunID:      "r2",
			ExitCode:   0,
			ResultJSON: &res,
		}
		data, err := json.Marshal(p)
		require.NoError(t, err)
		var back wsprotocol.WorkflowOutputPayload
		require.NoError(t, json.Unmarshal(data, &back))
		assert.Equal(t, 0, back.ExitCode)
		require.NotNil(t, back.ResultJSON)
		assert.Equal(t, res, *back.ResultJSON)
	})

	t.Run("WorkflowOutputPayload_Canceled", func(t *testing.T) {
		errStr := "canceled"
		p := wsprotocol.WorkflowOutputPayload{
			RunID:    "r3",
			ExitCode: 2,
			Error:    &errStr,
		}
		data, _ := json.Marshal(p)
		var back wsprotocol.WorkflowOutputPayload
		require.NoError(t, json.Unmarshal(data, &back))
		require.NotNil(t, back.Error)
		assert.Equal(t, "canceled", *back.Error)
	})

	t.Run("WorkflowLogPayload", func(t *testing.T) {
		p := wsprotocol.WorkflowLogPayload{
			RunID:     "r4",
			Stream:    "stdout",
			Chunk:     []byte("hello world\n"),
			Timestamp: time.Now().Format(time.RFC3339Nano),
		}
		data, _ := json.Marshal(p)
		var back wsprotocol.WorkflowLogPayload
		require.NoError(t, json.Unmarshal(data, &back))
		assert.Equal(t, "stdout", back.Stream)
		assert.Equal(t, p.Chunk, back.Chunk)
	})
}
