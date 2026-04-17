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

//go:build !no_sqlite

package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/GoogleCloudPlatform/scion/pkg/api"
	"github.com/GoogleCloudPlatform/scion/pkg/ent/entc"
	"github.com/GoogleCloudPlatform/scion/pkg/store"
	"github.com/GoogleCloudPlatform/scion/pkg/store/entadapter"
	"github.com/GoogleCloudPlatform/scion/pkg/store/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServerWithEnt creates a Server backed by a CompositeStore (SQLite + Ent)
// so that WorkflowRun operations (which are Ent-only) are available in tests.
func testServerWithEnt(t *testing.T) (*Server, store.Store) {
	t.Helper()

	base, err := sqlite.New(":memory:")
	if err != nil {
		t.Skip("sqlite not available: " + err.Error())
	}
	require.NoError(t, base.Migrate(context.Background()))

	entClient, err := entc.OpenSQLite("file:" + t.Name() + "?mode=memory&cache=shared")
	require.NoError(t, err)
	require.NoError(t, entc.AutoMigrate(context.Background(), entClient))

	cs := entadapter.NewCompositeStore(base, entClient)
	t.Cleanup(func() { cs.Close() })

	cfg := DefaultServerConfig()
	cfg.DevAuthToken = testDevToken
	srv, err := New(cfg, cs)
	require.NoError(t, err)
	srv.SetHubID("test-hub-id")
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	return srv, cs
}

func setupWorkflowRunTest(t *testing.T) (*Server, store.Store, string) {
	t.Helper()
	srv, s := testServerWithEnt(t)
	ctx := context.Background()

	// Use a valid UUID as the grove ID so the ent adapter can parse it.
	groveID := uuid.New().String()
	grove := &store.Grove{
		ID:   groveID,
		Name: "Workflow Test Grove",
		Slug: "wf-test-" + groveID[:8],
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	return srv, s, groveID
}

func TestWorkflowRun_CreateHappyPath(t *testing.T) {
	srv, _, groveID := setupWorkflowRunTest(t)

	req := api.WorkflowRunCreateRequest{
		GroveID:    groveID,
		SourceYAML: "version: \"0.7\"\nname: hello\nsteps:\n  - name: greet\n    exec: echo hello\n",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves/"+groveID+"/workflows/runs", req)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp api.WorkflowRunResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.NotEmpty(t, resp.Run.ID)
	assert.Equal(t, groveID, resp.Run.GroveID)
	assert.Equal(t, "queued", resp.Run.Status)
	assert.NotZero(t, resp.Run.CreatedAt)
	assert.NotNil(t, resp.Run.CreatedBy.UserID)
}

func TestWorkflowRun_Create_MissingGrove(t *testing.T) {
	srv, _, _ := setupWorkflowRunTest(t)

	req := api.WorkflowRunCreateRequest{
		GroveID:    "nonexistent-grove-id-xxxx",
		SourceYAML: "version: \"0.7\"\nname: hello\n",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves/nonexistent-grove-id-xxxx/workflows/runs", req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWorkflowRun_Create_MissingSourceYAML(t *testing.T) {
	srv, _, groveID := setupWorkflowRunTest(t)

	req := api.WorkflowRunCreateRequest{
		GroveID: groveID,
		// SourceYAML intentionally omitted
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/groves/"+groveID+"/workflows/runs", req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWorkflowRun_Create_RequiresAuth(t *testing.T) {
	srv, _, groveID := setupWorkflowRunTest(t)

	req := api.WorkflowRunCreateRequest{
		GroveID:    groveID,
		SourceYAML: "version: \"0.7\"\nname: hello\n",
	}

	rec := doRequestNoAuth(t, srv, http.MethodPost, "/api/v1/groves/"+groveID+"/workflows/runs", req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWorkflowRun_List(t *testing.T) {
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	// Create a run directly via store.
	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/groves/"+groveID+"/workflows/runs", nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.WorkflowRunListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Len(t, resp.Runs, 1)
	assert.Equal(t, run.ID, resp.Runs[0].ID)
	assert.Equal(t, "queued", resp.Runs[0].Status)
}

func TestWorkflowRun_List_StatusFilter(t *testing.T) {
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	// Create one queued and one succeeded run.
	queued := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: run1\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, queued))

	// The ent store doesn't have a direct "create with succeeded" path;
	// we test status filtering with two queued runs and verify count.
	queued2 := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: run2\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, queued2))

	// List with status=queued — should return both.
	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/groves/"+groveID+"/workflows/runs?status=queued", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp api.WorkflowRunListResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Runs, 2)

	// List with status=succeeded — should return none.
	rec2 := doRequest(t, srv, http.MethodGet,
		"/api/v1/groves/"+groveID+"/workflows/runs?status=succeeded", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp2 api.WorkflowRunListResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp2))
	assert.Empty(t, resp2.Runs)
}

func TestWorkflowRun_Get(t *testing.T) {
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	rec := doRequest(t, srv, http.MethodGet, "/api/v1/workflows/runs/"+run.ID, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.WorkflowRunDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, run.ID, resp.Run.ID)
	assert.Equal(t, groveID, resp.Run.GroveID)
	assert.Nil(t, resp.Run.Source, "source should be omitted without include=source")
}

func TestWorkflowRun_Get_WithIncludeSource(t *testing.T) {
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/workflows/runs/"+run.ID+"?include=source", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp api.WorkflowRunDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.NotNil(t, resp.Run.Source)
	assert.Equal(t, run.SourceYaml, *resp.Run.Source)
}

func TestWorkflowRun_Get_NotFound(t *testing.T) {
	srv, _, _ := setupWorkflowRunTest(t)

	rec := doRequest(t, srv, http.MethodGet,
		"/api/v1/workflows/runs/00000000-0000-0000-0000-000000000099", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWorkflowRun_Cancel_HappyPath(t *testing.T) {
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	rec := doRequest(t, srv, http.MethodPost,
		"/api/v1/workflows/runs/"+run.ID+"/cancel", nil)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var resp api.WorkflowRunResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "canceled", resp.Run.Status)
}

func TestWorkflowRun_Cancel_TerminalRun_IsIdempotent(t *testing.T) {
	// Per design doc Section 3.5, cancel on a terminal run returns 200 OK
	// with the current run state (idempotent no-op).
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	// Cancel once — succeeds with 202.
	rec1 := doRequest(t, srv, http.MethodPost,
		"/api/v1/workflows/runs/"+run.ID+"/cancel", nil)
	assert.Equal(t, http.StatusAccepted, rec1.Code)

	// Cancel again on an already-canceled (terminal) run — idempotent 200 OK.
	rec2 := doRequest(t, srv, http.MethodPost,
		"/api/v1/workflows/runs/"+run.ID+"/cancel", nil)
	assert.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())

	var resp api.WorkflowRunResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp))
	assert.Equal(t, "canceled", resp.Run.Status)
	assert.Equal(t, run.ID, resp.Run.ID)
}

func TestWorkflowRun_Cancel_Idempotent_QueuedToCancel(t *testing.T) {
	// First cancel on a queued run → 202. Second cancel → 200 (idempotent).
	srv, s, groveID := setupWorkflowRunTest(t)
	ctx := context.Background()

	run := &store.WorkflowRun{
		ID:         api.NewUUID(),
		GroveID:    groveID,
		SourceYaml: "version: \"0.7\"\nname: hello\n",
		InputsJSON: "{}",
		Status:     store.WorkflowRunStatusQueued,
	}
	require.NoError(t, s.CreateWorkflowRun(ctx, run))

	// First cancel.
	rec := doRequest(t, srv, http.MethodPost,
		"/api/v1/workflows/runs/"+run.ID+"/cancel", nil)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	// Second cancel — already terminal, idempotent 200 OK.
	rec2 := doRequest(t, srv, http.MethodPost,
		"/api/v1/workflows/runs/"+run.ID+"/cancel", nil)
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp api.WorkflowRunResponse
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp))
	assert.Equal(t, "canceled", resp.Run.Status)
}
