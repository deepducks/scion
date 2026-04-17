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

package hub

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/api"
	"github.com/GoogleCloudPlatform/scion/pkg/store"
	"github.com/gorilla/websocket"
)

// maxWorkflowSourceSize caps the inline sourceYaml at 256 KB.
const maxWorkflowSourceSize = 256 * 1024

// workflowLogsUpgrader configures the WebSocket upgrader for the logs endpoint.
var workflowLogsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Auth is checked before the upgrade.
		return true
	},
}

// handleGroveWorkflowRuns routes requests under
// /api/v1/groves/{groveID}/workflows/runs[/{runSubPath}]
func (s *Server) handleGroveWorkflowRuns(w http.ResponseWriter, r *http.Request, groveID, runSubPath string) {
	// Require authenticated identity.
	identity := GetIdentityFromContext(r.Context())
	if identity == nil {
		Unauthorized(w)
		return
	}

	// Agent-identity grove isolation (Phase 4: agent-created runs).
	if agentIdent := GetAgentIdentityFromContext(r.Context()); agentIdent != nil {
		if agentIdent.GroveID() != groveID {
			Forbidden(w)
			return
		}
	}

	if runSubPath == "" {
		// Collection endpoint: POST (create) or GET (list).
		switch r.Method {
		case http.MethodPost:
			s.createWorkflowRun(w, r, groveID)
		case http.MethodGet:
			s.listWorkflowRuns(w, r, groveID)
		default:
			MethodNotAllowed(w)
		}
		return
	}

	// Individual run: runSubPath may be "{runID}" or "{runID}/logs".
	// We don't use the groveID-scoped path for individual run operations
	// (those live under /api/v1/workflows/runs/{runID}), so 404 here.
	NotFound(w, "workflow run resource")
}

// handleWorkflowRunRoutes routes requests under
// /api/v1/workflows/runs[/{runID}[/{action}]]
func (s *Server) handleWorkflowRunRoutes(w http.ResponseWriter, r *http.Request) {
	// Require authenticated identity.
	identity := GetIdentityFromContext(r.Context())
	if identity == nil {
		Unauthorized(w)
		return
	}

	// Parse path: /api/v1/workflows/runs/{runID}[/{action}]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/runs")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		NotFound(w, "workflow run")
		return
	}

	parts := strings.SplitN(path, "/", 2)
	runID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			MethodNotAllowed(w)
			return
		}
		s.getWorkflowRun(w, r, runID)
	case "cancel":
		if r.Method != http.MethodPost {
			MethodNotAllowed(w)
			return
		}
		s.cancelWorkflowRun(w, r, runID)
	case "logs":
		s.streamWorkflowRunLogs(w, r, runID)
	default:
		NotFound(w, "workflow run action")
	}
}

// createWorkflowRun handles POST /api/v1/groves/{groveID}/workflows/runs
func (s *Server) createWorkflowRun(w http.ResponseWriter, r *http.Request, groveID string) {
	ctx := r.Context()

	// Enforce request body size limit.
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkflowSourceSize+4096)

	var req api.WorkflowRunCreateRequest
	if err := readJSON(r, &req); err != nil {
		BadRequest(w, "invalid request body: "+err.Error())
		return
	}

	// Validate required fields.
	if strings.TrimSpace(req.SourceYAML) == "" {
		ValidationError(w, "sourceYaml is required and must not be empty", nil)
		return
	}
	if len(req.SourceYAML) > maxWorkflowSourceSize {
		writeError(w, http.StatusRequestEntityTooLarge, "workflow_source_too_large",
			"sourceYaml exceeds the maximum allowed size (256 KB)", nil)
		return
	}

	// Verify the grove exists.
	grove, err := s.store.GetGrove(ctx, groveID)
	if err != nil {
		if err == store.ErrNotFound {
			NotFound(w, "Grove")
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}
	_ = grove // future: use grove for broker selection

	// Authorization: user must have write access to the grove.
	userIdent := GetUserIdentityFromContext(ctx)
	if userIdent == nil {
		// Only user-created runs supported in Phase 3b.
		Forbidden(w)
		return
	}
	decision := s.authzService.CheckAccess(ctx, userIdent, Resource{
		Type:       "workflow_run",
		ParentType: "grove",
		ParentID:   groveID,
		OwnerID:    grove.CreatedBy,
	}, ActionCreate)
	if !decision.Allowed {
		Forbidden(w)
		return
	}

	// Build inputs JSON.
	inputsJSON := req.Inputs
	if inputsJSON == "" {
		inputsJSON = "{}"
	} else if !json.Valid([]byte(inputsJSON)) {
		ValidationError(w, "inputs must be valid JSON", nil)
		return
	}

	// Stamp the creator.
	userIDStr := userIdent.ID()
	run := &store.WorkflowRun{
		ID:              api.NewUUID(),
		GroveID:         groveID,
		SourceYaml:      req.SourceYAML,
		InputsJSON:      inputsJSON,
		Status:          store.WorkflowRunStatusQueued,
		CreatedByUserID: &userIDStr,
	}

	if err := s.store.CreateWorkflowRun(ctx, run); err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	// Re-fetch to populate timestamps.
	created, err := s.store.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		// Created OK but can't re-fetch — return what we have.
		writeJSON(w, http.StatusCreated, api.WorkflowRunResponse{Run: toWorkflowRunSummary(run)})
		return
	}

	writeJSON(w, http.StatusCreated, api.WorkflowRunResponse{Run: toWorkflowRunSummary(created)})
}

// listWorkflowRuns handles GET /api/v1/groves/{groveID}/workflows/runs
func (s *Server) listWorkflowRuns(w http.ResponseWriter, r *http.Request, groveID string) {
	ctx := r.Context()
	query := r.URL.Query()

	limit := 50
	if l := query.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = n
		}
	}

	filter := store.WorkflowRunFilter{
		GroveID: groveID,
		Status:  query.Get("status"),
	}

	result, err := s.store.ListWorkflowRuns(ctx, store.WorkflowRunListOptions{
		Filter: filter,
		Limit:  limit,
		Cursor: query.Get("cursor"),
	})
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	summaries := make([]api.WorkflowRunSummary, len(result.Items))
	for i := range result.Items {
		summaries[i] = toWorkflowRunSummary(&result.Items[i])
	}

	writeJSON(w, http.StatusOK, api.WorkflowRunListResponse{
		Runs:       summaries,
		NextCursor: result.NextCursor,
	})
}

// getWorkflowRun handles GET /api/v1/workflows/runs/{runID}
func (s *Server) getWorkflowRun(w http.ResponseWriter, r *http.Request, runID string) {
	ctx := r.Context()

	run, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "workflow_run_not_found", "Workflow run not found", nil)
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	// Parse include= query parameter for heavy fields.
	includeStr := r.URL.Query().Get("include")
	includes := map[string]bool{}
	for _, field := range strings.Split(includeStr, ",") {
		includes[strings.TrimSpace(field)] = true
	}

	detail := api.WorkflowRunDetail{
		WorkflowRunSummary: toWorkflowRunSummary(run),
	}
	if includes["source"] {
		detail.Source = &run.SourceYaml
	}
	if includes["inputs"] {
		detail.Inputs = &run.InputsJSON
	}
	if includes["result"] && run.ResultJSON != nil {
		detail.Result = run.ResultJSON
	}
	if run.ErrorMessage != nil {
		detail.Error = run.ErrorMessage
	}

	writeJSON(w, http.StatusOK, api.WorkflowRunDetailResponse{Run: detail})
}

// cancelWorkflowRun handles POST /api/v1/workflows/runs/{runID}/cancel
func (s *Server) cancelWorkflowRun(w http.ResponseWriter, r *http.Request, runID string) {
	ctx := r.Context()

	updated, err := s.store.CancelWorkflowRun(ctx, runID)
	if err != nil {
		switch err {
		case store.ErrNotFound:
			writeError(w, http.StatusNotFound, "workflow_run_not_found", "Workflow run not found", nil)
		case store.ErrVersionConflict:
			// Already terminal: per design Section 3.5, terminal → 409.
			writeError(w, http.StatusConflict, "workflow_run_terminal",
				"Workflow run is already in a terminal state and cannot be canceled", nil)
		default:
			writeErrorFromErr(w, err, "")
		}
		return
	}

	writeJSON(w, http.StatusAccepted, api.WorkflowRunResponse{Run: toWorkflowRunSummary(updated)})
}

// streamWorkflowRunLogs handles GET (WSS upgrade) /api/v1/workflows/runs/{runID}/logs
// Phase 3b stub: accept the upgrade, emit a single "not yet wired" event, then close.
func (s *Server) streamWorkflowRunLogs(w http.ResponseWriter, r *http.Request, runID string) {
	// Verify the run exists before upgrading.
	ctx := r.Context()
	if _, err := s.store.GetWorkflowRun(ctx, runID); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "workflow_run_not_found", "Workflow run not found", nil)
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	conn, err := workflowLogsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade failure is already reported by the upgrader.
		return
	}
	defer conn.Close()

	stub := map[string]interface{}{
		"event": "logs_not_yet_wired",
		"phase": "3c",
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	msg, _ := json.Marshal(stub)
	_ = conn.WriteMessage(websocket.TextMessage, msg)
	_ = conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "logs wiring arrives in Phase 3c"))
}

// toWorkflowRunSummary converts a store.WorkflowRun to the wire summary type.
func toWorkflowRunSummary(r *store.WorkflowRun) api.WorkflowRunSummary {
	s := api.WorkflowRunSummary{
		ID:         r.ID,
		GroveID:    r.GroveID,
		BrokerID:   r.BrokerID,
		Status:     r.Status,
		TraceURL:   r.TraceURL,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		CreatedAt:  r.Created,
		CreatedBy: api.WorkflowRunCreatedBy{
			UserID:  r.CreatedByUserID,
			AgentID: r.CreatedByAgentID,
		},
	}
	return s
}
