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

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverGroves_EmptyHome(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	groves, err := DiscoverGroves()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groves) != 0 {
		t.Errorf("expected 0 groves, got %d", len(groves))
	}
}

func TestDiscoverGroves_GlobalOnly(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	globalDir := filepath.Join(tmpHome, ".scion")
	os.MkdirAll(filepath.Join(globalDir, "agents"), 0755)

	groves, err := DiscoverGroves()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groves) != 1 {
		t.Fatalf("expected 1 grove, got %d", len(groves))
	}
	if groves[0].Type != GroveTypeGlobal {
		t.Errorf("expected global type, got %s", groves[0].Type)
	}
	if groves[0].Name != "global" {
		t.Errorf("expected name 'global', got %s", groves[0].Name)
	}
	if groves[0].Status != GroveStatusOK {
		t.Errorf("expected status ok, got %s", groves[0].Status)
	}
}

func TestDiscoverGroves_ExternalGrove(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create global dir
	os.MkdirAll(filepath.Join(tmpHome, ".scion"), 0755)

	// Create an external grove config
	groveConfigDir := filepath.Join(tmpHome, ".scion", "grove-configs", "myproject__abcd1234", ".scion")
	os.MkdirAll(filepath.Join(groveConfigDir, "agents", "agent1"), 0755)

	// Create a workspace directory with a marker file
	workspace := filepath.Join(tmpHome, "projects", "myproject")
	os.MkdirAll(workspace, 0755)

	// Write marker file
	marker := &GroveMarker{
		GroveID:   "abcd1234-0000-0000-0000-000000000000",
		GroveName: "myproject",
		GroveSlug: "myproject",
	}
	WriteGroveMarker(filepath.Join(workspace, DotScion), marker)

	// Write settings with workspace_path
	settingsContent := "workspace_path: " + workspace + "\ngrove_id: abcd1234-0000-0000-0000-000000000000\n"
	os.WriteFile(filepath.Join(groveConfigDir, "settings.yaml"), []byte(settingsContent), 0644)

	groves, err := DiscoverGroves()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find global + external
	if len(groves) != 2 {
		t.Fatalf("expected 2 groves, got %d", len(groves))
	}

	ext := groves[1]
	if ext.Type != GroveTypeExternal {
		t.Errorf("expected external type, got %s", ext.Type)
	}
	if ext.Name != "myproject" {
		t.Errorf("expected name 'myproject', got %s", ext.Name)
	}
	if ext.Status != GroveStatusOK {
		t.Errorf("expected status ok, got %s", ext.Status)
	}
	if ext.AgentCount != 1 {
		t.Errorf("expected 1 agent, got %d", ext.AgentCount)
	}
	if ext.WorkspacePath != workspace {
		t.Errorf("expected workspace %s, got %s", workspace, ext.WorkspacePath)
	}
}

func TestDiscoverGroves_OrphanedExternal(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".scion"), 0755)

	// Create external grove config pointing to a non-existent workspace
	groveConfigDir := filepath.Join(tmpHome, ".scion", "grove-configs", "gone-project__deadbeef", ".scion")
	os.MkdirAll(filepath.Join(groveConfigDir, "agents"), 0755)

	settingsContent := "workspace_path: /nonexistent/workspace\n"
	os.WriteFile(filepath.Join(groveConfigDir, "settings.yaml"), []byte(settingsContent), 0644)

	groves, err := DiscoverGroves()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the external grove
	var ext *GroveInfo
	for i := range groves {
		if groves[i].Type == GroveTypeExternal {
			ext = &groves[i]
			break
		}
	}
	if ext == nil {
		t.Fatal("expected to find external grove")
	}
	if ext.Status != GroveStatusOrphaned {
		t.Errorf("expected status orphaned, got %s", ext.Status)
	}
}

func TestFindOrphanedGroveConfigs(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".scion"), 0755)

	// Create one orphaned grove
	orphanedDir := filepath.Join(tmpHome, ".scion", "grove-configs", "orphan__12345678", ".scion")
	os.MkdirAll(filepath.Join(orphanedDir, "agents"), 0755)
	os.WriteFile(filepath.Join(orphanedDir, "settings.yaml"), []byte("workspace_path: /does/not/exist\n"), 0644)

	orphaned, err := FindOrphanedGroveConfigs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned, got %d", len(orphaned))
	}
	if orphaned[0].Name != "orphan" {
		t.Errorf("expected name 'orphan', got %s", orphaned[0].Name)
	}
}

func TestRemoveGroveConfig(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	configDir := filepath.Join(tmpHome, ".scion", "grove-configs", "test__aabbccdd", ".scion")
	os.MkdirAll(configDir, 0755)
	os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte(""), 0644)

	if err := RemoveGroveConfig(configDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parentDir := filepath.Dir(configDir)
	if _, err := os.Stat(parentDir); !os.IsNotExist(err) {
		t.Errorf("expected directory to be removed, but it still exists")
	}
}

func TestRemoveGroveConfig_SafetyCheck(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Try to remove something outside grove-configs — should fail
	outsideDir := filepath.Join(tmpHome, "projects", "important")
	os.MkdirAll(outsideDir, 0755)

	err := RemoveGroveConfig(outsideDir)
	if err == nil {
		t.Error("expected error when removing path outside grove-configs")
	}
}

func TestReconnectGrove(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	// Create external grove config
	configDir := filepath.Join(tmpHome, ".scion", "grove-configs", "proj__11223344", ".scion")
	os.MkdirAll(configDir, 0755)
	os.WriteFile(filepath.Join(configDir, "settings.yaml"), []byte("workspace_path: /old/path\n"), 0644)

	newPath := filepath.Join(tmpHome, "new-workspace")
	os.MkdirAll(newPath, 0755)

	if err := ReconnectGrove(configDir, newPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify settings updated
	settings, err := LoadSettings(configDir)
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.WorkspacePath != newPath {
		t.Errorf("expected workspace_path %s, got %s", newPath, settings.WorkspacePath)
	}
}

func TestCountAgents(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	os.MkdirAll(filepath.Join(agentsDir, "agent-a"), 0755)
	os.MkdirAll(filepath.Join(agentsDir, "agent-b"), 0755)
	os.MkdirAll(filepath.Join(agentsDir, ".hidden"), 0755)

	count := countAgents(agentsDir)
	if count != 2 {
		t.Errorf("expected 2 agents, got %d", count)
	}
}

func TestCountAgents_NonExistentDir(t *testing.T) {
	count := countAgents("/nonexistent/agents")
	if count != 0 {
		t.Errorf("expected 0 agents, got %d", count)
	}
}

func TestListAgentNames(t *testing.T) {
	tmpDir := t.TempDir()
	agentsDir := filepath.Join(tmpDir, "agents")
	os.MkdirAll(filepath.Join(agentsDir, "agent-a"), 0755)
	os.MkdirAll(filepath.Join(agentsDir, "agent-b"), 0755)
	os.MkdirAll(filepath.Join(agentsDir, ".hidden"), 0755)

	names := ListAgentNames(agentsDir)
	if len(names) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(names))
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["agent-a"] || !nameSet["agent-b"] {
		t.Errorf("expected agent-a and agent-b, got %v", names)
	}
}

func TestListAgentNames_NonExistentDir(t *testing.T) {
	names := ListAgentNames("/nonexistent/agents")
	if names != nil {
		t.Errorf("expected nil, got %v", names)
	}
}

func TestGroveInfo_AgentsDir(t *testing.T) {
	tests := []struct {
		name     string
		grove    GroveInfo
		expected string
	}{
		{
			name: "external grove",
			grove: GroveInfo{
				Type:       GroveTypeExternal,
				ConfigPath: "/home/user/.scion/grove-configs/proj__abc123/.scion",
			},
			expected: "/home/user/.scion/grove-configs/proj__abc123/.scion/agents",
		},
		{
			name: "git grove",
			grove: GroveInfo{
				Type:       GroveTypeGit,
				ConfigPath: "/home/user/.scion/grove-configs/repo__def456/.scion",
			},
			expected: "/home/user/.scion/grove-configs/repo__def456/agents",
		},
		{
			name: "global grove",
			grove: GroveInfo{
				Type:       GroveTypeGlobal,
				ConfigPath: "/home/user/.scion",
			},
			expected: "/home/user/.scion/agents",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.grove.AgentsDir()
			if got != tt.expected {
				t.Errorf("AgentsDir() = %s, want %s", got, tt.expected)
			}
		})
	}
}

func TestDiscoverGroves_StaleExternalAfterMarkerRecreate(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".scion"), 0755)

	workspace := filepath.Join(tmpHome, "projects", "myproject")
	os.MkdirAll(workspace, 0755)

	// Simulate the old grove-config (from a previous init)
	oldConfigDir := filepath.Join(tmpHome, ".scion", "grove-configs", "myproject__aaaaaaaa", ".scion")
	os.MkdirAll(filepath.Join(oldConfigDir, "agents"), 0755)
	os.WriteFile(filepath.Join(oldConfigDir, "settings.yaml"),
		[]byte("workspace_path: "+workspace+"\ngrove_id: aaaaaaaa-0000-0000-0000-000000000000\n"), 0644)

	// Simulate new grove-config (from re-init after marker was deleted)
	newConfigDir := filepath.Join(tmpHome, ".scion", "grove-configs", "myproject__bbbbbbbb", ".scion")
	os.MkdirAll(filepath.Join(newConfigDir, "agents"), 0755)
	os.WriteFile(filepath.Join(newConfigDir, "settings.yaml"),
		[]byte("workspace_path: "+workspace+"\ngrove_id: bbbbbbbb-0000-0000-0000-000000000000\n"), 0644)

	// Workspace marker now points to the new grove-config
	marker := &GroveMarker{
		GroveID:   "bbbbbbbb-0000-0000-0000-000000000000",
		GroveName: "myproject",
		GroveSlug: "myproject",
	}
	WriteGroveMarker(filepath.Join(workspace, DotScion), marker)

	// The old config should be orphaned because the marker resolves to the new config
	orphaned, err := FindOrphanedGroveConfigs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned grove-config, got %d", len(orphaned))
	}
	if orphaned[0].Name != "myproject" {
		t.Errorf("expected orphaned name 'myproject', got %s", orphaned[0].Name)
	}
	// The orphaned one should be the old config
	if orphaned[0].ConfigPath != oldConfigDir {
		t.Errorf("expected orphaned config path %s, got %s", oldConfigDir, orphaned[0].ConfigPath)
	}
}

func TestDiscoverGroves_GitGroveExternal(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	os.MkdirAll(filepath.Join(tmpHome, ".scion"), 0755)

	// Create a git grove external directory (agents only, no .scion subdir)
	groveDir := filepath.Join(tmpHome, ".scion", "grove-configs", "myrepo__aabb1122")
	agentsDir := filepath.Join(groveDir, "agents", "worker1", "home")
	os.MkdirAll(agentsDir, 0755)

	groves, err := DiscoverGroves()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gitGrove *GroveInfo
	for i := range groves {
		if groves[i].Type == GroveTypeGit {
			gitGrove = &groves[i]
			break
		}
	}
	if gitGrove == nil {
		t.Fatal("expected to find git grove")
	}
	if gitGrove.Name != "myrepo" {
		t.Errorf("expected name 'myrepo', got %s", gitGrove.Name)
	}
	if gitGrove.AgentCount != 1 {
		t.Errorf("expected 1 agent, got %d", gitGrove.AgentCount)
	}
}
