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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentStatusUpdate_Authorization(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove
	grove := &store.Grove{
		ID:   "grove-1",
		Name: "Test Grove",
		Slug: "test-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create two agents
	agent1 := &store.Agent{
		ID:      "agent-1",
		Slug: "agent-1-slug",
		Name:    "Agent 1",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent1))

	agent2 := &store.Agent{
		ID:      "agent-2",
		Slug: "agent-2-slug",
		Name:    "Agent 2",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent2))

	// Get agent token service
	tokenSvc := srv.GetAgentTokenService()
	require.NotNil(t, tokenSvc)

	// Generate token for agent 1
	token1, err := tokenSvc.GenerateAgentToken(agent1.ID, grove.ID, []AgentTokenScope{ScopeAgentStatusUpdate})
	require.NoError(t, err)

	t.Run("Agent 1 can update its own status", func(t *testing.T) {
		status := store.AgentStatusUpdate{
			Status:  "idle",
			Message: "Waiting for user input",
		}
		body, _ := json.Marshal(status)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/status", bytes.NewReader(body))
		req.Header.Set("X-Scion-Agent-Token", token1)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		// Verify update in store
		updated, err := s.GetAgent(ctx, agent1.ID)
		require.NoError(t, err)
		assert.Equal(t, "idle", updated.Status)
		assert.Equal(t, "Waiting for user input", updated.Message)
	})

	t.Run("Agent 1 cannot update Agent 2's status", func(t *testing.T) {
		status := store.AgentStatusUpdate{
			Status: "error",
		}
		body, _ := json.Marshal(status)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-2/status", bytes.NewReader(body))
		req.Header.Set("X-Scion-Agent-Token", token1)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Agent 1 cannot perform lifecycle actions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/stop", nil)
		req.Header.Set("X-Scion-Agent-Token", token1)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("User can update agent status", func(t *testing.T) {
		status := store.AgentStatusUpdate{
			Status: "running",
		}
		body, _ := json.Marshal(status)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/status", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testDevToken)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		updated, err := s.GetAgent(ctx, agent1.ID)
		require.NoError(t, err)
		assert.Equal(t, "running", updated.Status)
	})
}

func TestAgentStatusUpdate_Heartbeat(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove
	grove := &store.Grove{
		ID:   "grove-h",
		Name: "Heartbeat Grove",
		Slug: "heartbeat-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create an agent
	agent := &store.Agent{
		ID:      "agent-h",
		Slug: "agent-h-slug",
		Name:    "Agent Heartbeat",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	// Record initial update time
	initial, err := s.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	initialTime := initial.LastSeen

	// Small delay to ensure timestamp changes
	time.Sleep(10 * time.Millisecond)

	// Send heartbeat
	status := store.AgentStatusUpdate{
		Status:    store.AgentStatusRunning,
		Heartbeat: true,
	}
	body, _ := json.Marshal(status)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-h/status", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testDevToken)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify update in store
	updated, err := s.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.True(t, updated.LastSeen.After(initialTime), "LastSeen should be updated")
}

// setupOfflineBrokerAgent creates a grove, an offline broker, and an agent assigned to that broker.
func setupOfflineBrokerAgent(t *testing.T, s store.Store, suffix string) (*store.Grove, *store.RuntimeBroker, *store.Agent) {
	t.Helper()
	ctx := context.Background()

	grove := &store.Grove{
		ID:   fmt.Sprintf("grove-offline-%s", suffix),
		Name: fmt.Sprintf("Offline Grove %s", suffix),
		Slug: fmt.Sprintf("offline-grove-%s", suffix),
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	broker := &store.RuntimeBroker{
		ID:     fmt.Sprintf("broker-offline-%s", suffix),
		Name:   fmt.Sprintf("Offline Broker %s", suffix),
		Slug:   fmt.Sprintf("offline-broker-%s", suffix),
		Status: store.BrokerStatusOffline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	agent := &store.Agent{
		ID:              fmt.Sprintf("agent-offline-%s", suffix),
		Slug:         fmt.Sprintf("agent-offline-%s-slug", suffix),
		Name:            fmt.Sprintf("Agent Offline %s", suffix),
		GroveID:         grove.ID,
		RuntimeBrokerID: broker.ID,
		Status:          store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	return grove, broker, agent
}

func TestDeleteAgent_BrokerOffline(t *testing.T) {
	srv, s := testServer(t)

	_, _, agent := setupOfflineBrokerAgent(t, s, "del")

	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/agents/"+agent.ID, nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Verify agent was NOT deleted
	ctx := context.Background()
	_, err := s.GetAgent(ctx, agent.ID)
	assert.NoError(t, err, "agent should still exist when broker is offline")
}

func TestDeleteAgent_NoBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	grove := &store.Grove{
		ID:   "grove-nobroker",
		Name: "No Broker Grove",
		Slug: "no-broker-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	agent := &store.Agent{
		ID:      "agent-nobroker",
		Slug: "agent-nobroker-slug",
		Name:    "Agent No Broker",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
		// No RuntimeBrokerID set
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	rec := doRequest(t, srv, http.MethodDelete, "/api/v1/agents/"+agent.ID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Verify agent was deleted
	_, err := s.GetAgent(ctx, agent.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestAgentLifecycle_BrokerOffline(t *testing.T) {
	srv, s := testServer(t)

	_, _, agent := setupOfflineBrokerAgent(t, s, "lc")

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents/"+agent.ID+"/start", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Verify the error code
	var errResp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	assert.Equal(t, ErrCodeRuntimeBrokerUnavail, errResp.Error.Code)
}

// ============================================================================
// Agent-as-Caller Tests (Sub-Agent Creation & Lifecycle)
// ============================================================================

func TestAgentCreateAgent_WithScope(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove
	grove := &store.Grove{
		ID:   "grove-parent",
		Name: "Parent Grove",
		Slug: "parent-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create a runtime broker and provider for the grove
	broker := &store.RuntimeBroker{
		ID:     "broker-parent",
		Name:   "Parent Broker",
		Slug:   "parent-broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	contrib := &store.GroveProvider{
		GroveID:    grove.ID,
		BrokerID:   broker.ID,
		BrokerName: broker.Name,
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, contrib))

	// Update grove default broker
	grove.DefaultRuntimeBrokerID = broker.ID
	require.NoError(t, s.UpdateGrove(ctx, grove))

	// Create the calling agent
	callingAgent := &store.Agent{
		ID:      "agent-caller",
		Slug:    "agent-caller",
		Name:    "Calling Agent",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, callingAgent))

	tokenSvc := srv.GetAgentTokenService()
	require.NotNil(t, tokenSvc)

	t.Run("Agent with grove:agent:create scope can create agent in same grove", func(t *testing.T) {
		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
			ScopeAgentCreate,
		})
		require.NoError(t, err)

		body, _ := json.Marshal(CreateAgentRequest{
			Name:    "Sub Agent",
			GroveID: grove.ID,
			Task:    "do something",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(body))
		req.Header.Set("X-Scion-Agent-Token", token)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		var resp CreateAgentResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotNil(t, resp.Agent)
		assert.Equal(t, "sub-agent", resp.Agent.Slug)
		assert.Equal(t, callingAgent.ID, resp.Agent.CreatedBy)
		// Verify CreatorName is the calling agent's name
		require.NotNil(t, resp.Agent.AppliedConfig)
		assert.Equal(t, callingAgent.Name, resp.Agent.AppliedConfig.CreatorName)
	})

	t.Run("Agent with grove:agent:create scope rejected for different grove", func(t *testing.T) {
		// Create another grove
		otherGrove := &store.Grove{
			ID:   "grove-other",
			Name: "Other Grove",
			Slug: "other-grove",
		}
		require.NoError(t, s.CreateGrove(ctx, otherGrove))

		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
			ScopeAgentCreate,
		})
		require.NoError(t, err)

		body, _ := json.Marshal(CreateAgentRequest{
			Name:    "Cross Grove Agent",
			GroveID: otherGrove.ID,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(body))
		req.Header.Set("X-Scion-Agent-Token", token)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Agent without grove:agent:create scope is rejected", func(t *testing.T) {
		// Token with only status update scope (no create scope)
		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
		})
		require.NoError(t, err)

		body, _ := json.Marshal(CreateAgentRequest{
			Name:    "Unauthorized Sub",
			GroveID: grove.ID,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", bytes.NewReader(body))
		req.Header.Set("X-Scion-Agent-Token", token)
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestAgentLifecycle_WithScope(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove
	grove := &store.Grove{
		ID:   "grove-lc",
		Name: "Lifecycle Grove",
		Slug: "lifecycle-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create the calling agent
	callingAgent := &store.Agent{
		ID:      "agent-lc-caller",
		Slug:    "agent-lc-caller",
		Name:    "Lifecycle Caller",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, callingAgent))

	// Create a target agent in the same grove
	targetAgent := &store.Agent{
		ID:      "agent-lc-target",
		Slug:    "agent-lc-target",
		Name:    "Lifecycle Target",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, targetAgent))

	tokenSvc := srv.GetAgentTokenService()
	require.NotNil(t, tokenSvc)

	t.Run("Agent with grove:agent:lifecycle scope can perform lifecycle actions in same grove", func(t *testing.T) {
		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
			ScopeAgentLifecycle,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+targetAgent.ID+"/stop", nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		// May return 200 or 500 (no dispatcher), but not 403 - the auth check passes
		assert.NotEqual(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Agent with grove:agent:lifecycle scope rejected for cross-grove lifecycle", func(t *testing.T) {
		// Create another grove and agent
		otherGrove := &store.Grove{
			ID:   "grove-lc-other",
			Name: "Other LC Grove",
			Slug: "other-lc-grove",
		}
		require.NoError(t, s.CreateGrove(ctx, otherGrove))

		otherAgent := &store.Agent{
			ID:      "agent-lc-other",
			Slug:    "agent-lc-other",
			Name:    "Other LC Agent",
			GroveID: otherGrove.ID,
			Status:  store.AgentStatusRunning,
		}
		require.NoError(t, s.CreateAgent(ctx, otherAgent))

		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
			ScopeAgentLifecycle,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+otherAgent.ID+"/stop", nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("Agent without lifecycle scope cannot perform lifecycle actions", func(t *testing.T) {
		// Token with only status update scope (existing behavior)
		token, err := tokenSvc.GenerateAgentToken(callingAgent.ID, grove.ID, []AgentTokenScope{
			ScopeAgentStatusUpdate,
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+targetAgent.ID+"/stop", nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestAgentGetAgent_GroveIsolation(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create two groves
	grove1 := &store.Grove{
		ID:   "grove-get1",
		Name: "Get Grove 1",
		Slug: "get-grove-1",
	}
	require.NoError(t, s.CreateGrove(ctx, grove1))

	grove2 := &store.Grove{
		ID:   "grove-get2",
		Name: "Get Grove 2",
		Slug: "get-grove-2",
	}
	require.NoError(t, s.CreateGrove(ctx, grove2))

	// Create agents in each grove
	agent1 := &store.Agent{
		ID:      "agent-get-caller",
		Slug:    "agent-get-caller",
		Name:    "Get Caller",
		GroveID: grove1.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent1))

	agent2SameGrove := &store.Agent{
		ID:      "agent-get-same",
		Slug:    "agent-get-same",
		Name:    "Same Grove Agent",
		GroveID: grove1.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent2SameGrove))

	agentOtherGrove := &store.Agent{
		ID:      "agent-get-other",
		Slug:    "agent-get-other",
		Name:    "Other Grove Agent",
		GroveID: grove2.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agentOtherGrove))

	tokenSvc := srv.GetAgentTokenService()
	require.NotNil(t, tokenSvc)

	token, err := tokenSvc.GenerateAgentToken(agent1.ID, grove1.ID, []AgentTokenScope{ScopeAgentStatusUpdate})
	require.NoError(t, err)

	t.Run("Agent can GET details of agents in same grove", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agent2SameGrove.ID, nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("Agent cannot GET details of agents in different grove", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentOtherGrove.ID, nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("Agent cannot access workspace operations", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agent2SameGrove.ID+"/workspace", nil)
		req.Header.Set("X-Scion-Agent-Token", token)

		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestDeleteGroveAgent_BrokerOffline(t *testing.T) {
	srv, s := testServer(t)

	grove, _, agent := setupOfflineBrokerAgent(t, s, "gdel")

	rec := doRequest(t, srv, http.MethodDelete,
		fmt.Sprintf("/api/v1/groves/%s/agents/%s", grove.ID, agent.ID), nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// Verify agent was NOT deleted
	ctx := context.Background()
	_, err := s.GetAgent(ctx, agent.ID)
	assert.NoError(t, err, "agent should still exist when broker is offline")
}

// createAgentDispatcher is a mock dispatcher for createAgent handler tests.
// It allows controlling the status that DispatchAgentCreate reports back.
type createAgentDispatcher struct {
	createStatus string // status to set on agent during DispatchAgentCreate
	deleteCalled bool
}

func (d *createAgentDispatcher) DispatchAgentCreate(_ context.Context, agent *store.Agent) error {
	if d.createStatus != "" {
		agent.Status = d.createStatus
	}
	return nil
}
func (d *createAgentDispatcher) DispatchAgentProvision(_ context.Context, agent *store.Agent) error {
	agent.Status = store.AgentStatusCreated
	return nil
}
func (d *createAgentDispatcher) DispatchAgentStart(_ context.Context, _ *store.Agent, _ string) error {
	return nil
}
func (d *createAgentDispatcher) DispatchAgentStop(_ context.Context, _ *store.Agent) error {
	return nil
}
func (d *createAgentDispatcher) DispatchAgentRestart(_ context.Context, _ *store.Agent) error {
	return nil
}
func (d *createAgentDispatcher) DispatchAgentDelete(_ context.Context, _ *store.Agent, _, _ bool) error {
	d.deleteCalled = true
	return nil
}
func (d *createAgentDispatcher) DispatchAgentMessage(_ context.Context, _ *store.Agent, _ string, _ bool) error {
	return nil
}
func (d *createAgentDispatcher) DispatchCheckAgentPrompt(_ context.Context, _ *store.Agent) (bool, error) {
	return false, nil
}
func (d *createAgentDispatcher) DispatchAgentCreateWithGather(_ context.Context, agent *store.Agent) (*RemoteEnvRequirementsResponse, error) {
	return nil, d.DispatchAgentCreate(context.Background(), agent)
}
func (d *createAgentDispatcher) DispatchFinalizeEnv(_ context.Context, _ *store.Agent, _ map[string]string) error {
	return nil
}

// setupCreateAgentServer creates a test server with a dispatcher and a grove+broker ready for agent creation.
func setupCreateAgentServer(t *testing.T, disp AgentDispatcher) (*Server, store.Store, *store.Grove) {
	t.Helper()
	srv, s := testServer(t)
	ctx := context.Background()

	grove := &store.Grove{
		ID:   "grove-create",
		Name: "Create Test Grove",
		Slug: "create-test-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	broker := &store.RuntimeBroker{
		ID:     "broker-create",
		Name:   "Create Test Broker",
		Slug:   "create-test-broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	provider := &store.GroveProvider{
		GroveID:    grove.ID,
		BrokerID:   broker.ID,
		BrokerName: broker.Name,
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	grove.DefaultRuntimeBrokerID = broker.ID
	require.NoError(t, s.UpdateGrove(ctx, grove))

	srv.SetDispatcher(disp)
	return srv, s, grove
}

func TestCreateAgent_BrokerStatusPreserved(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create an agent with a task — should dispatch and preserve broker-reported "running" status
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "status-test",
		GroveID: grove.ID,
		Task:    "do something",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// The response should reflect the broker-reported status, not hardcoded "provisioning"
	assert.Equal(t, store.AgentStatusRunning, resp.Agent.Status,
		"agent status should reflect broker response, not hardcoded provisioning")

	// Verify persisted status in store
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	assert.Equal(t, store.AgentStatusRunning, persisted.Status,
		"persisted agent status should match broker response")
}

func TestCreateAgent_FallbackToProvisioningWhenNoBrokerStatus(t *testing.T) {
	// Dispatcher that doesn't set a status (leaves it as "pending")
	disp := &createAgentDispatcher{createStatus: ""}
	srv, _, grove := setupCreateAgentServer(t, disp)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "fallback-test",
		GroveID: grove.ID,
		Task:    "do something",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// When broker doesn't report a status, should fall back to "provisioning"
	assert.Equal(t, store.AgentStatusProvisioning, resp.Agent.Status,
		"agent status should fall back to provisioning when broker doesn't report status")
}

func TestCreateAgent_StartsWithoutTask(t *testing.T) {
	// When ProvisionOnly is false (scion start), the agent should be started
	// even if no task is provided — the template may have a built-in prompt.
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, _, grove := setupCreateAgentServer(t, disp)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "no-task-agent",
		GroveID: grove.ID,
		// No Task, no Attach — should still start (not provision-only)
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// Should be running, not "created" (which would mean provision-only was used)
	assert.Equal(t, store.AgentStatusRunning, resp.Agent.Status,
		"agent should be started (running) even without a task when ProvisionOnly is false")
}

func TestCreateAgent_ProvisionOnlyStaysCreated(t *testing.T) {
	// When ProvisionOnly is true (scion create), the agent should not start.
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, _, grove := setupCreateAgentServer(t, disp)

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:          "provision-only-agent",
		GroveID:       grove.ID,
		Task:          "some task",
		ProvisionOnly: true,
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	assert.Equal(t, store.AgentStatusCreated, resp.Agent.Status,
		"agent should stay in created status when ProvisionOnly is true")
}

func TestCreateAgent_RestartFromProvisioningStatus(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Pre-create an agent stuck in "provisioning" status (simulating Bug 1)
	stuckAgent := &store.Agent{
		ID:              "agent-stuck-prov",
		Slug:            "stuck-agent",
		Name:            "stuck-agent",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-create",
		Status:          store.AgentStatusProvisioning,
	}
	require.NoError(t, s.CreateAgent(ctx, stuckAgent))

	// Try to start the same agent name — should succeed by re-starting, not 409
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "stuck-agent",
		GroveID: grove.ID,
		Task:    "retry task",
	})

	assert.Equal(t, http.StatusOK, rec.Code,
		"re-starting an agent stuck in provisioning should succeed (200), not conflict (409)")

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)
	assert.Equal(t, store.AgentStatusRunning, resp.Agent.Status)
}

func TestCreateAgent_RestartFromPendingStatus(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Pre-create an agent in "pending" status
	pendingAgent := &store.Agent{
		ID:              "agent-pending",
		Slug:            "pending-agent",
		Name:            "pending-agent",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-create",
		Status:          store.AgentStatusPending,
	}
	require.NoError(t, s.CreateAgent(ctx, pendingAgent))

	// Try to start the same agent name — should succeed
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "pending-agent",
		GroveID: grove.ID,
		Task:    "retry task",
	})

	assert.Equal(t, http.StatusOK, rec.Code,
		"re-starting an agent in pending status should succeed")
}

func TestCreateAgent_RecreateFromRunningStatus(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Pre-create an agent in "running" status (stale — container may have died)
	runningAgent := &store.Agent{
		ID:              "agent-running-stale",
		Slug:            "running-agent",
		Name:            "running-agent",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-create",
		Status:          store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, runningAgent))

	// Start with the same name — should delete old agent and create new one
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "running-agent",
		GroveID: grove.ID,
		Task:    "new task",
	})

	require.Equal(t, http.StatusCreated, rec.Code,
		"re-creating agent from running status should succeed with 201")

	// Old agent should be deleted
	_, err := s.GetAgent(ctx, "agent-running-stale")
	assert.ErrorIs(t, err, store.ErrNotFound, "old agent should be deleted")

	// Dispatcher should have been asked to delete
	assert.True(t, disp.deleteCalled, "dispatcher should have been asked to delete old agent")

	// New agent should exist
	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)
	assert.NotEqual(t, "agent-running-stale", resp.Agent.ID, "new agent should have a different ID")
	assert.Equal(t, store.AgentStatusRunning, resp.Agent.Status)
}

func TestCreateAgent_RecreateFromErrorStatus(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Pre-create an agent in "error" status
	errorAgent := &store.Agent{
		ID:              "agent-errored",
		Slug:            "error-agent",
		Name:            "error-agent",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-create",
		Status:          store.AgentStatusError,
	}
	require.NoError(t, s.CreateAgent(ctx, errorAgent))

	// Start with the same name — should delete and recreate
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "error-agent",
		GroveID: grove.ID,
		Task:    "retry after error",
	})

	require.Equal(t, http.StatusCreated, rec.Code,
		"re-creating agent from error status should succeed with 201")

	// Old agent should be deleted
	_, err := s.GetAgent(ctx, "agent-errored")
	assert.ErrorIs(t, err, store.ErrNotFound, "old errored agent should be deleted")
}

func TestCreateAgent_RecreateFromStoppedStatus(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Pre-create an agent in "stopped" status
	stoppedAgent := &store.Agent{
		ID:              "agent-stopped",
		Slug:            "stopped-agent",
		Name:            "stopped-agent",
		GroveID:         grove.ID,
		RuntimeBrokerID: "broker-create",
		Status:          store.AgentStatusStopped,
	}
	require.NoError(t, s.CreateAgent(ctx, stoppedAgent))

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "stopped-agent",
		GroveID: grove.ID,
		Task:    "restart after stop",
	})

	require.Equal(t, http.StatusCreated, rec.Code,
		"re-creating agent from stopped status should succeed with 201")

	_, err := s.GetAgent(ctx, "agent-stopped")
	assert.ErrorIs(t, err, store.ErrNotFound, "old stopped agent should be deleted")
}

// TestAgentCreate_LocalTemplateWithLocalBroker tests that agent creation succeeds
// when a template is not found on the Hub but the target broker has local filesystem
// access (LocalPath is set), allowing the template to be resolved locally by the broker.
func TestAgentCreate_LocalTemplateWithLocalBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a runtime broker
	broker := &store.RuntimeBroker{
		ID:     "broker_local_tpl",
		Slug:   "local-tpl-broker",
		Name:   "Local Template Broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a grove with default runtime broker
	grove := &store.Grove{
		ID:                     "grove_local_tpl",
		Slug:                   "local-tpl-grove",
		Name:                   "Local Template Grove",
		GitRemote:              "github.com/test/local-tpl",
		DefaultRuntimeBrokerID: broker.ID,
		Created:                time.Now(),
		Updated:                time.Now(),
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Register the broker as a provider WITH a local path
	provider := &store.GroveProvider{
		GroveID:    grove.ID,
		BrokerID:   broker.ID,
		BrokerName: broker.Name,
		LocalPath:  "/home/user/project/.scion",
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	// Create agent with a template name that does NOT exist on the Hub.
	// Because the broker has a LocalPath, this should succeed.
	body := map[string]interface{}{
		"name":     "Local Template Agent",
		"groveId":  grove.ID,
		"template": "my-local-template",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)

	assert.Equal(t, http.StatusCreated, rec.Code, "expected 201 when broker has local access, got %d: %s", rec.Code, rec.Body.String())

	var resp CreateAgentResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotNil(t, resp.Agent)
	assert.Equal(t, "local-template-agent", resp.Agent.Slug)
	assert.Equal(t, "my-local-template", resp.Agent.Template)
	// The harness should fall back to the template name when resolvedTemplate is nil
	require.NotNil(t, resp.Agent.AppliedConfig)
	assert.Equal(t, "my-local-template", resp.Agent.AppliedConfig.Harness)
	// TemplateID and TemplateHash should be empty since template was not resolved on Hub
	assert.Empty(t, resp.Agent.AppliedConfig.TemplateID)
	assert.Empty(t, resp.Agent.AppliedConfig.TemplateHash)
}

// TestAgentCreate_LocalTemplateWithRemoteBroker tests that agent creation returns
// NotFound when a template is not on the Hub and the broker does NOT have local access.
func TestAgentCreate_LocalTemplateWithRemoteBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a runtime broker
	broker := &store.RuntimeBroker{
		ID:     "broker_remote_tpl",
		Slug:   "remote-tpl-broker",
		Name:   "Remote Template Broker",
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a grove
	grove := &store.Grove{
		ID:                     "grove_remote_tpl",
		Slug:                   "remote-tpl-grove",
		Name:                   "Remote Template Grove",
		GitRemote:              "github.com/test/remote-tpl",
		DefaultRuntimeBrokerID: broker.ID,
		Created:                time.Now(),
		Updated:                time.Now(),
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Register the broker as a provider WITHOUT a local path
	provider := &store.GroveProvider{
		GroveID:    grove.ID,
		BrokerID:   broker.ID,
		BrokerName: broker.Name,
		Status:     store.BrokerStatusOnline,
		// Note: LocalPath is NOT set — broker has no local access
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	// Create agent with a template name that does NOT exist on the Hub.
	// Without local access, this should fail with NotFound.
	body := map[string]interface{}{
		"name":     "Remote Template Agent",
		"groveId":  grove.ID,
		"template": "nonexistent-template",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)

	assert.Equal(t, http.StatusNotFound, rec.Code, "expected 404 when template not on Hub and broker has no local access")
}

// TestAgentCreate_LocalTemplateNoBroker tests that agent creation fails when a
// template is not on the Hub and there is no runtime broker assigned. The error
// occurs because no broker is available (before template resolution is reached).
func TestAgentCreate_LocalTemplateNoBroker(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove WITHOUT a default runtime broker
	grove := &store.Grove{
		ID:        "grove_no_broker_tpl",
		Slug:      "no-broker-tpl-grove",
		Name:      "No Broker Template Grove",
		GitRemote: "github.com/test/no-broker-tpl",
		Created:   time.Now(),
		Updated:   time.Now(),
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Create agent with a template name that does NOT exist on the Hub.
	// Without any broker, this should fail (422 validation error for missing broker).
	body := map[string]interface{}{
		"name":     "No Broker Agent",
		"groveId":  grove.ID,
		"template": "nonexistent-template",
	}

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)

	// Expect a client error — the broker resolution fails before template resolution
	assert.True(t, rec.Code >= 400 && rec.Code < 500, "expected client error when no broker assigned, got %d", rec.Code)
}

func TestCreateAgent_CreatorName_UserEmail(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Use dev auth token (which creates a DevUser with email "dev@localhost")
	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "user-created-agent",
		GroveID: grove.ID,
		Task:    "do something",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// Verify the agent's AppliedConfig.CreatorName is the user's email
	require.NotNil(t, resp.Agent.AppliedConfig)
	assert.Equal(t, "dev@localhost", resp.Agent.AppliedConfig.CreatorName,
		"CreatorName should be the user's email when a user creates an agent")

	// Also verify it's persisted in the store
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig)
	assert.Equal(t, "dev@localhost", persisted.AppliedConfig.CreatorName)
}

func TestListAgents_ServerTimeIncluded(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove and agent
	grove := &store.Grove{
		ID:   "grove-servertime",
		Name: "ServerTime Grove",
		Slug: "servertime-grove",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	agent := &store.Agent{
		ID:      "agent-servertime",
		Slug:    "agent-servertime-slug",
		Name:    "ServerTime Agent",
		GroveID: grove.ID,
		Status:  store.AgentStatusRunning,
	}
	require.NoError(t, s.CreateAgent(ctx, agent))

	before := time.Now().UTC()

	// List agents
	rec := doRequest(t, srv, http.MethodGet, "/api/v1/agents?groveId="+grove.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	after := time.Now().UTC()

	var resp ListAgentsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// ServerTime should be non-zero and between before/after
	assert.False(t, resp.ServerTime.IsZero(), "ServerTime should be non-zero")
	assert.True(t, !resp.ServerTime.Before(before.Add(-time.Second)),
		"ServerTime %v should not be before request start %v", resp.ServerTime, before)
	assert.True(t, !resp.ServerTime.After(after.Add(time.Second)),
		"ServerTime %v should not be after request end %v", resp.ServerTime, after)
}

func TestListGroveAgents_ServerTimeIncluded(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a grove
	grove := &store.Grove{
		ID:   "grove-servertime-g",
		Name: "ServerTime Grove G",
		Slug: "servertime-grove-g",
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	before := time.Now().UTC()

	// List grove agents
	rec := doRequest(t, srv, http.MethodGet, fmt.Sprintf("/api/v1/groves/%s/agents", grove.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)

	after := time.Now().UTC()

	var resp ListAgentsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.False(t, resp.ServerTime.IsZero(), "ServerTime should be non-zero in grove-scoped list")
	assert.True(t, !resp.ServerTime.Before(before.Add(-time.Second)),
		"ServerTime should not be before request start")
	assert.True(t, !resp.ServerTime.After(after.Add(time.Second)),
		"ServerTime should not be after request end")
}

// TestCreateGroveAgent_BrokerStatusPreserved tests that the grove-scoped agent creation
// endpoint (/api/v1/groves/{groveId}/agents) preserves the status set by the broker's
// response rather than unconditionally overwriting it with "provisioning".
func TestCreateGroveAgent_BrokerStatusPreserved(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create agent via the grove-scoped endpoint (this is the path the CLI uses)
	rec := doRequest(t, srv, http.MethodPost,
		fmt.Sprintf("/api/v1/groves/%s/agents", grove.ID),
		CreateAgentRequest{
			Name: "grove-status-test",
			Task: "do something",
		})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// The response should reflect the broker-reported status, not hardcoded "provisioning"
	assert.Equal(t, store.AgentStatusRunning, resp.Agent.Status,
		"grove-scoped agent status should reflect broker response, not hardcoded provisioning")

	// Verify persisted status in store
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	assert.Equal(t, store.AgentStatusRunning, persisted.Status,
		"persisted agent status should match broker response")
}

// TestCreateGroveAgent_FallbackToProvisioningWhenNoBrokerStatus tests that the grove-scoped
// endpoint falls back to "provisioning" when the broker doesn't report a status.
func TestCreateGroveAgent_FallbackToProvisioningWhenNoBrokerStatus(t *testing.T) {
	// Dispatcher that doesn't set a status (leaves it as "pending")
	disp := &createAgentDispatcher{createStatus: ""}
	srv, _, grove := setupCreateAgentServer(t, disp)

	rec := doRequest(t, srv, http.MethodPost,
		fmt.Sprintf("/api/v1/groves/%s/agents", grove.ID),
		CreateAgentRequest{
			Name: "grove-fallback-test",
			Task: "do something",
		})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// When broker doesn't report a status, should fall back to "provisioning"
	assert.Equal(t, store.AgentStatusProvisioning, resp.Agent.Status,
		"agent status should fall back to provisioning when broker doesn't report status")
}

func TestCreateAgent_GitAnchoredGrovePopulatesGitClone(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, _ := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create a grove with GitRemote and labels
	gitGrove := &store.Grove{
		ID:        "grove-git",
		Name:      "Git Grove",
		Slug:      "git-grove",
		GitRemote: "github.com/example/myrepo",
		Labels: map[string]string{
			"scion.dev/clone-url":      "https://github.com/example/myrepo.git",
			"scion.dev/default-branch": "develop",
		},
		DefaultRuntimeBrokerID: "broker-create",
	}
	require.NoError(t, s.CreateGrove(ctx, gitGrove))

	// Add grove provider
	provider := &store.GroveProvider{
		GroveID:    gitGrove.ID,
		BrokerID:   "broker-create",
		BrokerName: "Create Test Broker",
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "git-agent",
		GroveID: gitGrove.ID,
		Task:    "implement feature",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// Verify the agent was created — check that AppliedConfig.GitClone was populated
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	require.NotNil(t, persisted.AppliedConfig.GitClone, "GitClone should be populated for git-anchored grove")
	assert.Equal(t, "https://github.com/example/myrepo.git", persisted.AppliedConfig.GitClone.URL)
	assert.Equal(t, "develop", persisted.AppliedConfig.GitClone.Branch)
	assert.Equal(t, 1, persisted.AppliedConfig.GitClone.Depth)
}

func TestCreateAgent_NonGitGroveNoGitClone(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "non-git-agent",
		GroveID: grove.ID,
		Task:    "do something",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	assert.Nil(t, persisted.AppliedConfig.GitClone,
		"GitClone should be nil for non-git-anchored grove")
}

func TestCreateGroveAgent_GitAnchoredGrovePopulatesGitClone(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, _ := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create a grove with GitRemote and labels
	gitGrove := &store.Grove{
		ID:        "grove-git-scoped",
		Name:      "Git Grove Scoped",
		Slug:      "git-grove-scoped",
		GitRemote: "github.com/example/myrepo",
		Labels: map[string]string{
			"scion.dev/clone-url":      "https://github.com/example/myrepo.git",
			"scion.dev/default-branch": "develop",
		},
		DefaultRuntimeBrokerID: "broker-create",
	}
	require.NoError(t, s.CreateGrove(ctx, gitGrove))

	// Add grove provider
	provider := &store.GroveProvider{
		GroveID:    gitGrove.ID,
		BrokerID:   "broker-create",
		BrokerName: "Create Test Broker",
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	rec := doRequest(t, srv, http.MethodPost,
		fmt.Sprintf("/api/v1/groves/%s/agents", gitGrove.ID),
		CreateAgentRequest{
			Name: "git-agent-scoped",
			Task: "implement feature",
		})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	// Verify the agent was created — check that AppliedConfig.GitClone was populated
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	require.NotNil(t, persisted.AppliedConfig.GitClone, "GitClone should be populated for git-anchored grove")
	assert.Equal(t, "https://github.com/example/myrepo.git", persisted.AppliedConfig.GitClone.URL)
	assert.Equal(t, "develop", persisted.AppliedConfig.GitClone.Branch)
	assert.Equal(t, 1, persisted.AppliedConfig.GitClone.Depth)
}

func TestCreateGroveAgent_NonGitGroveNoGitClone(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	rec := doRequest(t, srv, http.MethodPost,
		fmt.Sprintf("/api/v1/groves/%s/agents", grove.ID),
		CreateAgentRequest{
			Name: "non-git-agent-scoped",
			Task: "do something",
		})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	assert.Nil(t, persisted.AppliedConfig.GitClone,
		"GitClone should be nil for non-git-anchored grove")
}

func TestCreateAgent_GitGroveCloneURLFallback(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, _ := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create a grove with GitRemote but WITHOUT the scion.dev/clone-url label.
	// The URL should be constructed from gitRemote as "https://<gitRemote>.git".
	gitGrove := &store.Grove{
		ID:        "grove-git-fallback-url",
		Name:      "Git Grove Fallback URL",
		Slug:      "git-grove-fallback-url",
		GitRemote: "github.com/example/fallback-repo",
		Labels: map[string]string{
			"scion.dev/default-branch": "develop",
		},
		DefaultRuntimeBrokerID: "broker-create",
	}
	require.NoError(t, s.CreateGrove(ctx, gitGrove))

	provider := &store.GroveProvider{
		GroveID:    gitGrove.ID,
		BrokerID:   "broker-create",
		BrokerName: "Create Test Broker",
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "fallback-url-agent",
		GroveID: gitGrove.ID,
		Task:    "test fallback",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	require.NotNil(t, persisted.AppliedConfig.GitClone, "GitClone should be populated")

	// clone-url label is missing, so URL should be constructed from GitRemote
	assert.Equal(t, "https://github.com/example/fallback-repo.git", persisted.AppliedConfig.GitClone.URL,
		"clone URL should be constructed from gitRemote when scion.dev/clone-url label is absent")
	assert.Equal(t, "develop", persisted.AppliedConfig.GitClone.Branch)
	assert.Equal(t, 1, persisted.AppliedConfig.GitClone.Depth)
}

func TestCreateAgent_GitGroveDefaultBranchFallback(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, _ := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	// Create a grove with GitRemote and clone-url label but WITHOUT default-branch.
	// The branch should default to "main".
	gitGrove := &store.Grove{
		ID:        "grove-git-fallback-branch",
		Name:      "Git Grove Fallback Branch",
		Slug:      "git-grove-fallback-branch",
		GitRemote: "github.com/example/branch-repo",
		Labels: map[string]string{
			"scion.dev/clone-url": "https://github.com/example/branch-repo.git",
		},
		DefaultRuntimeBrokerID: "broker-create",
	}
	require.NoError(t, s.CreateGrove(ctx, gitGrove))

	provider := &store.GroveProvider{
		GroveID:    gitGrove.ID,
		BrokerID:   "broker-create",
		BrokerName: "Create Test Broker",
		Status:     store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveProvider(ctx, provider))

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "fallback-branch-agent",
		GroveID: gitGrove.ID,
		Task:    "test branch fallback",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)

	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig, "AppliedConfig should be set")
	require.NotNil(t, persisted.AppliedConfig.GitClone, "GitClone should be populated")

	assert.Equal(t, "https://github.com/example/branch-repo.git", persisted.AppliedConfig.GitClone.URL)
	// default-branch label is missing, so branch should default to "main"
	assert.Equal(t, "main", persisted.AppliedConfig.GitClone.Branch,
		"branch should default to 'main' when scion.dev/default-branch label is absent")
	assert.Equal(t, 1, persisted.AppliedConfig.GitClone.Depth)
}

func TestCreateAgent_ProfileStoredInAppliedConfig(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "profiled-agent",
		GroveID: grove.ID,
		Profile: "custom-profile",
		Task:    "do something",
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)
	require.NotNil(t, resp.Agent.AppliedConfig)
	assert.Equal(t, "custom-profile", resp.Agent.AppliedConfig.Profile,
		"Profile should be stored in AppliedConfig")

	// Verify it's persisted in the store
	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig)
	assert.Equal(t, "custom-profile", persisted.AppliedConfig.Profile)
}

func TestCreateAgent_ProfileStoredWithConfigOverride(t *testing.T) {
	disp := &createAgentDispatcher{createStatus: store.AgentStatusRunning}
	srv, s, grove := setupCreateAgentServer(t, disp)
	ctx := context.Background()

	rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", CreateAgentRequest{
		Name:    "profiled-agent-with-config",
		GroveID: grove.ID,
		Profile: "other-profile",
		Task:    "do something",
		Config:  &AgentConfigOverride{Image: "custom-image:latest"},
	})

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp CreateAgentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Agent)
	require.NotNil(t, resp.Agent.AppliedConfig)
	assert.Equal(t, "other-profile", resp.Agent.AppliedConfig.Profile,
		"Profile should be stored even when config override is present")
	assert.Equal(t, "custom-image:latest", resp.Agent.AppliedConfig.Image)

	persisted, err := s.GetAgent(ctx, resp.Agent.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.AppliedConfig)
	assert.Equal(t, "other-profile", persisted.AppliedConfig.Profile)
}
