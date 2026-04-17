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
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/wsprotocol"
)

// workflowRunRequest is the JSON body sent by the hub when dispatching a run.
type workflowRunRequest struct {
	RunID          string `json:"runId"`
	GroveID        string `json:"groveId"`
	SourceYAML     string `json:"sourceYaml"`
	InputsJSON     string `json:"inputsJson"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// activeWorkflowRun tracks a workflow run that is currently executing.
type activeWorkflowRun struct {
	runID    string
	groveID  string
	cancel   context.CancelFunc
	startedAt time.Time
}

// WorkflowExecutor executes duckflux workflow runs on this broker.
//
// Each run is executed by writing the workflow source YAML and inputs JSON to
// a temporary directory on the broker host, then invoking `quack run` with the
// appropriate flags. Stdout/stderr output is streamed back to the Hub as
// workflow_log events; on exit a workflow_output event is sent.
type WorkflowExecutor struct {
	brokerID string
	log      *slog.Logger

	// getControlChannel returns the control channel client for the connection
	// identified by connectionName (may be empty for the default connection).
	// Returns nil when no channel is available.
	getControlChannel func(connectionName string) *ControlChannelClient

	mu       sync.Mutex
	runs     map[string]*activeWorkflowRun
}

// NewWorkflowExecutor creates a WorkflowExecutor.
//
// getControlChannel is a function that returns a *ControlChannelClient for
// sending events back to the Hub. It receives the connection name embedded in
// the tunneled request header, or empty string for the default connection.
func NewWorkflowExecutor(
	brokerID string,
	getControlChannel func(connectionName string) *ControlChannelClient,
	log *slog.Logger,
) *WorkflowExecutor {
	return &WorkflowExecutor{
		brokerID:          brokerID,
		log:               log,
		getControlChannel: getControlChannel,
		runs:              make(map[string]*activeWorkflowRun),
	}
}

// HandleCreateWorkflowRun handles POST /api/v1/workflow-runs.
// It validates the request, registers the run, and starts execution
// asynchronously so the 202 can be returned immediately.
func (e *WorkflowExecutor) HandleCreateWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowed(w)
		return
	}

	var req workflowRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		BadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if req.RunID == "" {
		BadRequest(w, "runId is required")
		return
	}
	if strings.TrimSpace(req.SourceYAML) == "" {
		BadRequest(w, "sourceYaml is required")
		return
	}

	// Capture the connection name so the async goroutine can find the right
	// control channel even after the request context is gone.
	connName := r.Header.Get("X-Scion-Hub-Connection")

	e.mu.Lock()
	if _, exists := e.runs[req.RunID]; exists {
		e.mu.Unlock()
		// Idempotent: already accepted this run.
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "runId": req.RunID})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &activeWorkflowRun{
		runID:    req.RunID,
		groveID:  req.GroveID,
		cancel:   cancel,
		startedAt: time.Now(),
	}
	e.runs[req.RunID] = run
	e.mu.Unlock()

	go e.executeRun(ctx, connName, req, cancel)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "runId": req.RunID})
}

// HandleCancelWorkflowRun handles DELETE /api/v1/workflow-runs/{runID}.
func (e *WorkflowExecutor) HandleCancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		MethodNotAllowed(w)
		return
	}

	// Extract runID from the path /api/v1/workflow-runs/{runID}
	runID := strings.TrimPrefix(r.URL.Path, "/api/v1/workflow-runs/")
	runID = strings.TrimSuffix(runID, "/")
	if runID == "" {
		BadRequest(w, "runId missing in path")
		return
	}

	e.mu.Lock()
	run, ok := e.runs[runID]
	e.mu.Unlock()

	if !ok {
		// Not found or already finished — idempotent 404.
		NotFound(w, "workflow run")
		return
	}

	run.cancel()

	writeJSON(w, http.StatusOK, map[string]string{"status": "canceling", "runId": runID})
}

// executeRun performs the actual workflow execution in a goroutine.
func (e *WorkflowExecutor) executeRun(ctx context.Context, connName string, req workflowRunRequest, cancel context.CancelFunc) {
	defer cancel()

	defer func() {
		e.mu.Lock()
		delete(e.runs, req.RunID)
		e.mu.Unlock()
	}()

	log := e.log.With("runID", req.RunID, "groveID", req.GroveID)
	log.Info("WorkflowExecutor: starting run")

	// Emit workflow_status: running
	e.sendStatusEvent(connName, req.RunID, "running")

	// Build a temporary working directory for this run.
	workDir, err := os.MkdirTemp("", "scion-workflow-"+req.RunID+"-")
	if err != nil {
		log.Error("WorkflowExecutor: failed to create workdir", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("internal error: "+err.Error()), "")
		return
	}
	defer os.RemoveAll(workDir)

	// Write source YAML and inputs JSON to files.
	sourceFile := filepath.Join(workDir, "workflow.yaml")
	if err := os.WriteFile(sourceFile, []byte(req.SourceYAML), 0o600); err != nil {
		log.Error("WorkflowExecutor: failed to write source YAML", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("failed to write source: "+err.Error()), "")
		return
	}

	inputsFile := filepath.Join(workDir, "inputs.json")
	inputs := req.InputsJSON
	if inputs == "" {
		inputs = "{}"
	}
	if err := os.WriteFile(inputsFile, []byte(inputs), 0o600); err != nil {
		log.Error("WorkflowExecutor: failed to write inputs JSON", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("failed to write inputs: "+err.Error()), "")
		return
	}

	// Determine timeout.
	timeout := req.TimeoutSeconds
	if timeout <= 0 {
		timeout = 3600
	}
	runCtx, runCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer runCancel()

	// Build the quack command.
	// quack run [--input-file <path>] <workflow-file>
	//   exit 0 = succeeded, 1 = workflow-level failure, 2 = runtime/internal error
	// stdout carries the JSON result on success; stderr carries human-readable diagnostics.
	cmd := exec.CommandContext(runCtx, "quack", "run",
		"--input-file", inputsFile,
		sourceFile,
	)
	cmd.Dir = workDir

	// Capture stdout and stderr via pipes for streaming.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("WorkflowExecutor: failed to create stdout pipe", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("pipe error: "+err.Error()), "")
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Error("WorkflowExecutor: failed to create stderr pipe", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("pipe error: "+err.Error()), "")
		return
	}

	if err := cmd.Start(); err != nil {
		log.Error("WorkflowExecutor: failed to start quack", "error", err)
		e.sendOutputEvent(connName, req.RunID, 2, nil, ptrStr("failed to start quack: "+err.Error()), "")
		return
	}

	// Stream stdout and stderr to hub concurrently.
	var wg sync.WaitGroup
	var stdoutBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutBuf.WriteString(line)
			stdoutBuf.WriteString("\n")
			e.sendLogEvent(connName, req.RunID, "stdout", []byte(line+"\n"))
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			e.sendLogEvent(connName, req.RunID, "stderr", []byte(line+"\n"))
		}
	}()

	wg.Wait()
	cmdErr := cmd.Wait()

	exitCode := 0
	var errStr *string
	var resultJSON *string

	if cmdErr != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			// Context timeout
			exitCode = 2
			errStr = ptrStr("timed_out")
		} else if ctx.Err() != nil {
			// Cancelled by broker (cancel was called)
			exitCode = 2
			errStr = ptrStr("canceled")
		} else if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 2
			errStr = ptrStr("exec error: " + cmdErr.Error())
		}
	} else {
		// Parse JSON result from stdout.
		output := strings.TrimSpace(stdoutBuf.String())
		if output != "" && json.Valid([]byte(output)) {
			resultJSON = &output
		}
	}

	log.Info("WorkflowExecutor: run completed", "exitCode", exitCode)
	e.sendOutputEvent(connName, req.RunID, exitCode, resultJSON, errStr, "")
}

// sendStatusEvent sends a workflow_status event to the Hub.
func (e *WorkflowExecutor) sendStatusEvent(connName, runID, status string) {
	cc := e.getControlChannel(connName)
	if cc == nil {
		e.log.Warn("WorkflowExecutor: no control channel for status event",
			"runID", runID, "status", status)
		return
	}

	payload := wsprotocol.WorkflowStatusPayload{
		RunID:  runID,
		Status: status,
		At:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := cc.SendEvent(wsprotocol.EventWorkflowStatus, payload); err != nil {
		e.log.Warn("WorkflowExecutor: failed to send status event",
			"runID", runID, "status", status, "error", err)
	}
}

// sendLogEvent sends a workflow_log event chunk to the Hub.
func (e *WorkflowExecutor) sendLogEvent(connName, runID, stream string, chunk []byte) {
	cc := e.getControlChannel(connName)
	if cc == nil {
		return
	}

	payload := wsprotocol.WorkflowLogPayload{
		RunID:     runID,
		Stream:    stream,
		Chunk:     chunk,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := cc.SendEvent(wsprotocol.EventWorkflowLog, payload); err != nil {
		e.log.Warn("WorkflowExecutor: failed to send log event",
			"runID", runID, "stream", stream, "error", err)
	}
}

// sendOutputEvent sends the terminal workflow_output event to the Hub.
func (e *WorkflowExecutor) sendOutputEvent(connName, runID string, exitCode int, resultJSON, errStr *string, traceKey string) {
	cc := e.getControlChannel(connName)
	if cc == nil {
		e.log.Warn("WorkflowExecutor: no control channel for output event", "runID", runID)
		return
	}

	payload := wsprotocol.WorkflowOutputPayload{
		RunID:      runID,
		ExitCode:   exitCode,
		ResultJSON: resultJSON,
		Error:      errStr,
		TraceKey:   traceKey,
	}
	if err := cc.SendEvent(wsprotocol.EventWorkflowOutput, payload); err != nil {
		e.log.Warn("WorkflowExecutor: failed to send output event",
			"runID", runID, "exitCode", exitCode, "error", err)
	}
}

// ptrStr returns a pointer to a string.
func ptrStr(s string) *string {
	return &s
}
