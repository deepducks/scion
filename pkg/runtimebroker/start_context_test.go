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
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/scion/pkg/api"
	"github.com/GoogleCloudPlatform/scion/pkg/config"
	"github.com/GoogleCloudPlatform/scion/pkg/runtime"
)

func TestBuildStartContext_BasicFields(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.BrokerID = "broker-1"
	cfg.BrokerName = "test-broker"
	cfg.Debug = true
	cfg.StateDir = t.TempDir()

	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:        "my-agent",
		AgentID:     "uuid-1",
		Slug:        "my-agent-slug",
		GroveID:     "grove-1",
		Attach:      false,
		HTTPRequest: r,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sc.Opts.Name != "my-agent" {
		t.Errorf("expected name 'my-agent', got %q", sc.Opts.Name)
	}
	if !sc.Opts.BrokerMode {
		t.Error("expected BrokerMode to be true")
	}
	if sc.Opts.Detached == nil || !*sc.Opts.Detached {
		t.Error("expected Detached to be true when Attach=false")
	}

	// Verify broker identity env
	if sc.Opts.Env["SCION_BROKER_NAME"] != "test-broker" {
		t.Errorf("expected SCION_BROKER_NAME='test-broker', got %q", sc.Opts.Env["SCION_BROKER_NAME"])
	}
	if sc.Opts.Env["SCION_BROKER_ID"] != "broker-1" {
		t.Errorf("expected SCION_BROKER_ID='broker-1', got %q", sc.Opts.Env["SCION_BROKER_ID"])
	}
	if sc.Opts.Env["SCION_AGENT_ID"] != "uuid-1" {
		t.Errorf("expected SCION_AGENT_ID='uuid-1', got %q", sc.Opts.Env["SCION_AGENT_ID"])
	}
	if sc.Opts.Env["SCION_AGENT_SLUG"] != "my-agent-slug" {
		t.Errorf("expected SCION_AGENT_SLUG='my-agent-slug', got %q", sc.Opts.Env["SCION_AGENT_SLUG"])
	}
	if sc.Opts.Env["SCION_GROVE_ID"] != "grove-1" {
		t.Errorf("expected SCION_GROVE_ID='grove-1', got %q", sc.Opts.Env["SCION_GROVE_ID"])
	}
	if sc.Opts.Env["SCION_DEBUG"] != "1" {
		t.Errorf("expected SCION_DEBUG='1', got %q", sc.Opts.Env["SCION_DEBUG"])
	}
}

func TestBuildStartContext_EnvMerging(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name: "agent-1",
		ResolvedEnv: map[string]string{
			"KEY_A": "from-hub",
			"KEY_B": "from-hub",
		},
		Config: &CreateAgentConfig{
			Env: []string{"KEY_B=from-config", "KEY_C=from-config"},
		},
		HTTPRequest: r,
	})
	if err != nil {
		t.Fatal(err)
	}

	// ResolvedEnv is applied first, Config.Env overrides
	if sc.Opts.Env["KEY_A"] != "from-hub" {
		t.Errorf("expected KEY_A='from-hub', got %q", sc.Opts.Env["KEY_A"])
	}
	if sc.Opts.Env["KEY_B"] != "from-config" {
		t.Errorf("expected KEY_B='from-config' (config overrides hub), got %q", sc.Opts.Env["KEY_B"])
	}
	if sc.Opts.Env["KEY_C"] != "from-config" {
		t.Errorf("expected KEY_C='from-config', got %q", sc.Opts.Env["KEY_C"])
	}
}

func TestBuildStartContext_TelemetryOverride(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name: "agent-1",
		ResolvedEnv: map[string]string{
			"SCION_TELEMETRY_ENABLED": "true",
		},
		HTTPRequest: r,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sc.Opts.TelemetryOverride == nil || !*sc.Opts.TelemetryOverride {
		t.Error("expected TelemetryOverride to be true when SCION_TELEMETRY_ENABLED=true")
	}
}

func TestBuildStartContext_ResolvedSecrets(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	secrets := []api.ResolvedSecret{
		{Name: "API_KEY", Type: "environment", Value: "secret-value"},
	}
	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:            "agent-1",
		ResolvedSecrets: secrets,
		HTTPRequest:     r,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(sc.Opts.ResolvedSecrets) != 1 || sc.Opts.ResolvedSecrets[0].Name != "API_KEY" {
		t.Errorf("expected resolved secrets to be passed through, got %v", sc.Opts.ResolvedSecrets)
	}
}

func TestBuildStartContext_ConfigFields(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name: "agent-1",
		Config: &CreateAgentConfig{
			Template:      "my-template",
			Image:         "my-image:latest",
			HarnessConfig: "claude",
			HarnessAuth:   "api-key",
			Task:          "write tests",
			Workspace:     "/workspace",
			Profile:       "default",
			Branch:        "feature-1",
		},
		HTTPRequest: r,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sc.Opts.Template != "my-template" {
		t.Errorf("expected Template='my-template', got %q", sc.Opts.Template)
	}
	if sc.Opts.Image != "my-image:latest" {
		t.Errorf("expected Image='my-image:latest', got %q", sc.Opts.Image)
	}
	if sc.Opts.HarnessConfig != "claude" {
		t.Errorf("expected HarnessConfig='claude', got %q", sc.Opts.HarnessConfig)
	}
	if sc.Opts.Task != "write tests" {
		t.Errorf("expected Task='write tests', got %q", sc.Opts.Task)
	}
	if sc.TemplateSlug != "my-template" {
		t.Errorf("expected TemplateSlug='my-template', got %q", sc.TemplateSlug)
	}
}

func TestBuildStartContext_GitClone(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	r := httptest.NewRequest("POST", "/api/v1/agents", nil)
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:      "agent-1",
		GrovePath: "/some/path",
		Config: &CreateAgentConfig{
			Branch: "feature-1",
			GitClone: &api.GitCloneConfig{
				URL:    "https://github.com/org/repo.git",
				Branch: "main",
				Depth:  1,
			},
		},
		HTTPRequest: r,
	})
	if err != nil {
		t.Fatal(err)
	}

	if sc.Opts.Env["SCION_GIT_CLONE_URL"] != "https://github.com/org/repo.git" {
		t.Errorf("expected SCION_GIT_CLONE_URL set, got %q", sc.Opts.Env["SCION_GIT_CLONE_URL"])
	}
	if sc.Opts.Env["SCION_GIT_BRANCH"] != "main" {
		t.Errorf("expected SCION_GIT_BRANCH='main', got %q", sc.Opts.Env["SCION_GIT_BRANCH"])
	}
	if sc.Opts.Env["SCION_GIT_DEPTH"] != "1" {
		t.Errorf("expected SCION_GIT_DEPTH='1', got %q", sc.Opts.Env["SCION_GIT_DEPTH"])
	}
	if sc.Opts.Env["SCION_AGENT_BRANCH"] != "feature-1" {
		t.Errorf("expected SCION_AGENT_BRANCH='feature-1', got %q", sc.Opts.Env["SCION_AGENT_BRANCH"])
	}
	// Git clone mode should clear workspace but preserve grove path
	// so ProvisionAgent can resolve the correct agent directory.
	if sc.Opts.Workspace != "" {
		t.Errorf("expected Workspace to be empty in git clone mode, got %q", sc.Opts.Workspace)
	}
	if sc.Opts.GrovePath != "/some/path" {
		t.Errorf("expected GrovePath to be preserved in git clone mode, got %q", sc.Opts.GrovePath)
	}
}

func TestBuildStartContext_NilHTTPRequest(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	// Should not panic with nil HTTPRequest
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name: "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Opts.Name != "agent-1" {
		t.Errorf("expected name 'agent-1', got %q", sc.Opts.Name)
	}
}

func TestBuildStartContext_AttachMode(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:   "agent-1",
		Attach: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Opts.Detached == nil || *sc.Opts.Detached {
		t.Error("expected Detached to be false when Attach=true")
	}
}

func TestBuildStartContext_HubNativeGroveWritesGroveID(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	// Simulate a hub-native grove: GroveSlug set, GrovePath pre-resolved
	// (as the createAgent handler does for env-gather), and GroveID from hub.
	grovesDir := t.TempDir()
	grovePath := filepath.Join(grovesDir, "web-demo")

	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:      "agent-1",
		GroveSlug: "web-demo",
		GrovePath: grovePath,
		GroveID:   "6d868c0f-b862-49e0-a44b-3555a3887ee3",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify .scion dir was created
	scionDir := filepath.Join(grovePath, ".scion")
	if _, err := os.Stat(scionDir); os.IsNotExist(err) {
		t.Fatal(".scion directory was not created")
	}

	// Verify grove-id was written with hub's ID
	groveID, err := config.ReadGroveID(scionDir)
	if err != nil {
		t.Fatalf("failed to read grove-id: %v", err)
	}
	if groveID != "6d868c0f-b862-49e0-a44b-3555a3887ee3" {
		t.Errorf("expected grove-id '6d868c0f-b862-49e0-a44b-3555a3887ee3', got %q", groveID)
	}

	// Verify external grove-configs directories were created
	extAgents, err := config.GetGitGroveExternalAgentsDir(scionDir)
	if err != nil {
		t.Fatalf("failed to get external agents dir: %v", err)
	}
	if extAgents == "" {
		t.Fatal("expected non-empty external agents dir")
	}
	if _, err := os.Stat(extAgents); os.IsNotExist(err) {
		t.Fatalf("external agents dir was not created: %s", extAgents)
	}

	// Verify agent home resolves to external path (not inside workspace)
	agentHome := config.GetAgentHomePath(scionDir, "test-agent")
	if !strings.Contains(agentHome, "grove-configs") {
		t.Errorf("expected agent home under grove-configs, got %q", agentHome)
	}
	if strings.Contains(agentHome, "web-demo") && !strings.Contains(agentHome, "grove-configs") {
		t.Errorf("agent home should NOT be inside workspace dir, got %q", agentHome)
	}

	// GrovePath should be passed through to opts
	if sc.Opts.GrovePath != grovePath {
		t.Errorf("expected GrovePath %q, got %q", grovePath, sc.Opts.GrovePath)
	}
}

func TestBuildStartContext_HubNativeGroveSlugResolution(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	// Simulate: GroveSlug set, GrovePath empty (buildStartContext resolves it),
	// GroveID from hub. This is the path when the handler doesn't pre-resolve.
	t.Setenv("HOME", t.TempDir())

	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:      "agent-1",
		GroveSlug: "my-project",
		GroveID:   "aabbccdd-1234-5678-9012-abcdef123456",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify grove-id was written
	scionDir := filepath.Join(sc.Opts.GrovePath, ".scion")
	groveID, err := config.ReadGroveID(scionDir)
	if err != nil {
		t.Fatalf("failed to read grove-id: %v", err)
	}
	if groveID != "aabbccdd-1234-5678-9012-abcdef123456" {
		t.Errorf("expected grove-id from hub, got %q", groveID)
	}
}

func TestBuildStartContext_HubNativeGrovePreservesExistingGroveID(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	// Pre-create .scion dir with an existing grove-id
	grovePath := filepath.Join(t.TempDir(), "existing-grove")
	scionDir := filepath.Join(grovePath, ".scion")
	if err := os.MkdirAll(scionDir, 0755); err != nil {
		t.Fatal(err)
	}
	existingID := "existing-id-1234-5678"
	if err := config.WriteGroveID(scionDir, existingID); err != nil {
		t.Fatal(err)
	}

	_, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name:      "agent-1",
		GroveSlug: "existing-grove",
		GrovePath: grovePath,
		GroveID:   "new-id-from-hub",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify existing grove-id was NOT overwritten
	groveID, err := config.ReadGroveID(scionDir)
	if err != nil {
		t.Fatalf("failed to read grove-id: %v", err)
	}
	if groveID != existingID {
		t.Errorf("expected existing grove-id %q to be preserved, got %q", existingID, groveID)
	}
}

func TestBuildStartContext_HubEndpoint(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.HubEndpoint = "https://hub.example.com"
	cfg.StateDir = t.TempDir()
	mgr := &envCapturingManager{}
	rt := &runtime.MockRuntime{}
	srv := New(cfg, mgr, rt)

	// Without HTTPRequest, uses resolveHubEndpointForStart path
	sc, err := srv.buildStartContext(context.Background(), startContextInputs{
		Name: "agent-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Opts.Env["SCION_HUB_ENDPOINT"] != "https://hub.example.com" {
		t.Errorf("expected SCION_HUB_ENDPOINT='https://hub.example.com', got %q", sc.Opts.Env["SCION_HUB_ENDPOINT"])
	}
	if sc.Opts.Env["SCION_HUB_URL"] != "https://hub.example.com" {
		t.Errorf("expected SCION_HUB_URL='https://hub.example.com', got %q", sc.Opts.Env["SCION_HUB_URL"])
	}
}
