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
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/apiclient"
	"github.com/gorilla/websocket"
)

// ============================================================================
// Wire types (mirrors api.WorkflowRun* but defined here for client isolation)
// ============================================================================

// WorkflowRunCreatedBy identifies the creator of a workflow run.
type WorkflowRunCreatedBy struct {
	UserID  *string `json:"userId,omitempty"`
	AgentID *string `json:"agentId,omitempty"`
}

// WorkflowRun is the condensed run record returned in list and create responses.
type WorkflowRun struct {
	ID         string               `json:"id"`
	GroveID    string               `json:"groveId"`
	BrokerID   *string              `json:"brokerId,omitempty"`
	Status     string               `json:"status"`
	TraceURL   *string              `json:"traceUrl,omitempty"`
	StartedAt  *time.Time           `json:"startedAt,omitempty"`
	FinishedAt *time.Time           `json:"finishedAt,omitempty"`
	CreatedAt  time.Time            `json:"createdAt"`
	CreatedBy  WorkflowRunCreatedBy `json:"createdBy"`
}

// WorkflowRunDetail extends WorkflowRun with the heavy optional fields.
type WorkflowRunDetail struct {
	WorkflowRun

	Source *string `json:"source,omitempty"`
	Inputs *string `json:"inputs,omitempty"`
	Result *string `json:"result,omitempty"`
	Error  *string `json:"error,omitempty"`
}

// LogEvent is a single structured log line emitted by the WSS logs endpoint.
type LogEvent struct {
	// Event is non-empty for control messages (e.g. "logs_not_yet_wired", "terminal").
	Event  string `json:"event,omitempty"`
	Phase  string `json:"phase,omitempty"`
	TS     string `json:"ts,omitempty"`
	Stream string `json:"stream,omitempty"` // "stdout" or "stderr"
	Line   string `json:"line,omitempty"`
	Status string `json:"status,omitempty"`
}

// ============================================================================
// Request / response envelopes
// ============================================================================

// CreateWorkflowRunRequest is the request body for CreateWorkflowRun.
type CreateWorkflowRunRequest struct {
	GroveID    string `json:"groveId"`
	SourceYAML string `json:"sourceYaml"`
	Inputs     string `json:"inputs,omitempty"`
}

// ListWorkflowRunsOptions configures workflow run listing.
type ListWorkflowRunsOptions struct {
	Status string
	Page   apiclient.PageOptions
}

// workflowRunResponse is the envelope for create/cancel responses.
type workflowRunResponse struct {
	Run WorkflowRun `json:"run"`
}

// workflowRunDetailResponse is the envelope for get responses.
type workflowRunDetailResponse struct {
	Run WorkflowRunDetail `json:"run"`
}

// workflowRunListResponse is the list response envelope.
type workflowRunListResponse struct {
	Runs       []WorkflowRun `json:"runs"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// ============================================================================
// Client methods
// ============================================================================

func groveWorkflowRunsPath(groveID string) string {
	return fmt.Sprintf("/api/v1/groves/%s/workflows/runs", url.PathEscape(groveID))
}

func workflowRunPath(runID string) string {
	return fmt.Sprintf("/api/v1/workflows/runs/%s", url.PathEscape(runID))
}

// CreateWorkflowRun creates a new workflow run in the given grove.
func (c *client) CreateWorkflowRun(ctx context.Context, req *CreateWorkflowRunRequest) (*WorkflowRun, error) {
	resp, err := c.transport.Post(ctx, groveWorkflowRunsPath(req.GroveID), req, nil)
	if err != nil {
		return nil, err
	}
	env, err := apiclient.DecodeResponse[workflowRunResponse](resp)
	if err != nil {
		return nil, err
	}
	return &env.Run, nil
}

// ListWorkflowRuns returns workflow runs in a grove, with optional status filter.
// Returns (items, nextCursor, error).
func (c *client) ListWorkflowRuns(ctx context.Context, groveID string, opts *ListWorkflowRunsOptions) ([]WorkflowRun, string, error) {
	query := url.Values{}
	if opts != nil {
		if opts.Status != "" {
			query.Set("status", opts.Status)
		}
		opts.Page.ToQuery(query)
	}

	resp, err := c.transport.GetWithQuery(ctx, groveWorkflowRunsPath(groveID), query, nil)
	if err != nil {
		return nil, "", err
	}
	env, err := apiclient.DecodeResponse[workflowRunListResponse](resp)
	if err != nil {
		return nil, "", err
	}
	return env.Runs, env.NextCursor, nil
}

// GetWorkflowRun retrieves a workflow run by ID.
// Pass include fields to expand heavy payload: "source", "inputs", "result".
func (c *client) GetWorkflowRun(ctx context.Context, runID string, include ...string) (*WorkflowRunDetail, error) {
	path := workflowRunPath(runID)
	query := url.Values{}
	if len(include) > 0 {
		query.Set("include", joinInclude(include))
	}
	var resp *http.Response
	var err error
	if len(query) > 0 {
		resp, err = c.transport.GetWithQuery(ctx, path, query, nil)
	} else {
		resp, err = c.transport.Get(ctx, path, nil)
	}
	if err != nil {
		return nil, err
	}
	env, err := apiclient.DecodeResponse[workflowRunDetailResponse](resp)
	if err != nil {
		return nil, err
	}
	return &env.Run, nil
}

// CancelWorkflowRun requests cancellation of a workflow run.
// Returns the (potentially updated) run. Returns an error if the run is already
// in a terminal state (HTTP 409 from the server).
func (c *client) CancelWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	resp, err := c.transport.Post(ctx, workflowRunPath(runID)+"/cancel", nil, nil)
	if err != nil {
		return nil, err
	}
	env, err := apiclient.DecodeResponse[workflowRunResponse](resp)
	if err != nil {
		return nil, err
	}
	return &env.Run, nil
}

// StreamWorkflowRunLogs opens a WebSocket connection to the logs endpoint and
// returns a channel of LogEvents. The channel is closed when the connection
// terminates (server sends a terminal event or closes the socket).
//
// Phase 3b note: the server currently sends a single "logs_not_yet_wired" event
// and closes. Full streaming arrives in Phase 3c.
func (c *client) StreamWorkflowRunLogs(ctx context.Context, runID string) (<-chan LogEvent, error) {
	// Derive the WebSocket URL from the transport's BaseURL.
	base := c.transport.BaseURL
	wsURL, err := httpToWS(base + workflowRunPath(runID) + "/logs")
	if err != nil {
		return nil, fmt.Errorf("building WebSocket URL: %w", err)
	}

	// Build auth header using a dummy request (same mechanism the Transport uses).
	reqHeaders := http.Header{}
	if c.transport.Auth != nil {
		dummyReq, _ := http.NewRequest("GET", wsURL, nil)
		if err := c.transport.Auth.ApplyAuth(dummyReq); err == nil {
			reqHeaders = dummyReq.Header
		}
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, reqHeaders)
	if err != nil {
		return nil, fmt.Errorf("dialing WebSocket: %w", err)
	}

	ch := make(chan LogEvent, 64)
	go func() {
		defer close(ch)
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var evt LogEvent
			if jsonErr := json.Unmarshal(msg, &evt); jsonErr == nil {
				select {
				case ch <- evt:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// httpToWS converts an http(s) URL to a ws(s) URL.
func httpToWS(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		if !strings.HasPrefix(u.Scheme, "ws") {
			u.Scheme = "ws"
		}
	}
	return u.String(), nil
}

func joinInclude(fields []string) string {
	result := ""
	for i, f := range fields {
		if i > 0 {
			result += ","
		}
		result += f
	}
	return result
}
