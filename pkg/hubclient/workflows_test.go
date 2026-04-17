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

package hubclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sampleRun returns a minimal WorkflowRun fixture.
func sampleRun(id, groveID, status string) WorkflowRun {
	return WorkflowRun{
		ID:        id,
		GroveID:   groveID,
		Status:    status,
		CreatedAt: time.Now().UTC(),
		CreatedBy: WorkflowRunCreatedBy{},
	}
}

func TestCreateWorkflowRun_RequestShape(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/groves/grove-abc/workflows/runs" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		run := sampleRun("run-001", "grove-abc", "queued")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(workflowRunResponse{Run: run})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	req := &CreateWorkflowRunRequest{
		GroveID:    "grove-abc",
		SourceYAML: "version: \"0.7\"\nname: hello\n",
		Inputs:     `{"name": "world"}`,
	}
	run, err := c.CreateWorkflowRun(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.ID != "run-001" {
		t.Errorf("expected run ID 'run-001', got %q", run.ID)
	}
	if run.Status != "queued" {
		t.Errorf("expected status 'queued', got %q", run.Status)
	}
	// Verify request shape.
	if capturedBody["sourceYaml"] == nil {
		t.Errorf("expected sourceYaml in request body, got %v", capturedBody)
	}
	if capturedBody["inputs"] != `{"name": "world"}` {
		t.Errorf("unexpected inputs: %v", capturedBody["inputs"])
	}
}

func TestListWorkflowRuns_RequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/groves/grove-xyz/workflows/runs" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "queued" {
			t.Errorf("expected status=queued, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workflowRunListResponse{
			Runs: []WorkflowRun{
				sampleRun("run-1", "grove-xyz", "queued"),
				sampleRun("run-2", "grove-xyz", "queued"),
			},
			NextCursor: "cursor-abc",
		})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	runs, next, err := c.ListWorkflowRuns(context.Background(), "grove-xyz", &ListWorkflowRunsOptions{
		Status: "queued",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
	if next != "cursor-abc" {
		t.Errorf("expected next cursor 'cursor-abc', got %q", next)
	}
}

func TestGetWorkflowRun_RequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workflows/runs/run-999" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workflowRunDetailResponse{
			Run: WorkflowRunDetail{
				WorkflowRun: sampleRun("run-999", "grove-x", "succeeded"),
			},
		})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	run, err := c.GetWorkflowRun(context.Background(), "run-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.ID != "run-999" {
		t.Errorf("expected run ID 'run-999', got %q", run.ID)
	}
}

func TestGetWorkflowRun_WithInclude(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		src := "version: \"0.7\"\n"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workflowRunDetailResponse{
			Run: WorkflowRunDetail{
				WorkflowRun: sampleRun("run-123", "grove-x", "succeeded"),
				Source:      &src,
			},
		})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	run, err := c.GetWorkflowRun(context.Background(), "run-123", "source", "result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedQuery, "include=source") {
		t.Errorf("expected include=source in query, got %q", capturedQuery)
	}
	if run.Source == nil {
		t.Error("expected source to be populated")
	}
}

func TestCancelWorkflowRun_RequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/workflows/runs/run-555/cancel" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(workflowRunResponse{
			Run: sampleRun("run-555", "grove-x", "canceled"),
		})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	run, err := c.CancelWorkflowRun(context.Background(), "run-555")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != "canceled" {
		t.Errorf("expected status 'canceled', got %q", run.Status)
	}
}

func TestCancelWorkflowRun_ConflictFallbackDecodedAsError(t *testing.T) {
	// The hub normally returns 200 with the current run on terminal cancel
	// (idempotent per design doc Section 3.5). This test exercises the
	// defensive fallback: if the server ever returns 409 (e.g. transient
	// coordination error), the client decodes it as an error rather than
	// crashing or silently returning a partial run.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    "workflow_run_terminal",
				"message": "Workflow run is already in a terminal state",
			},
		})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	_, err := c.CancelWorkflowRun(context.Background(), "run-finished")
	if err == nil {
		t.Fatal("expected error from 409 response, got nil")
	}
}

func TestWorkflowRunClient_NilOpts(t *testing.T) {
	// Verify that nil opts doesn't panic for ListWorkflowRuns.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(workflowRunListResponse{Runs: []WorkflowRun{}})
	}))
	defer server.Close()

	c, _ := New(server.URL)
	runs, _, err := c.ListWorkflowRuns(context.Background(), "grove-nil", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected empty list, got %d", len(runs))
	}
}
