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
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/util"
	"gopkg.in/yaml.v3"
)

//go:embed all:embeds/*
var EmbedsFS embed.FS

func GetDefaultSettingsData() ([]byte, error) {
	// Load embedded YAML defaults
	data, err := EmbedsFS.ReadFile("embeds/default_settings.yaml")
	if err != nil {
		return nil, err
	}

	// Detect whether embedded defaults use versioned or legacy format
	version, _ := DetectSettingsFormat(data)
	if version != "" {
		// Versioned format: unmarshal into VersionedSettings, adjust, convert to legacy
		var vs VersionedSettings
		if err := yaml.Unmarshal(data, &vs); err != nil {
			return nil, err
		}

		// Apply OS-specific runtime adjustment for local profile
		if local, ok := vs.Profiles["local"]; ok {
			if runtime.GOOS == "darwin" {
				local.Runtime = "container"
			} else {
				local.Runtime = "docker"
			}
			vs.Profiles["local"] = local
		}

		// Convert to legacy and return JSON for backward compatibility
		legacy := convertVersionedToLegacy(&vs)
		return json.MarshalIndent(legacy, "", "  ")
	}

	// Legacy format: existing behavior
	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	// Apply OS-specific runtime adjustment for local profile
	if local, ok := settings.Profiles["local"]; ok {
		if runtime.GOOS == "darwin" {
			local.Runtime = "container"
		} else {
			local.Runtime = "docker"
		}
		settings.Profiles["local"] = local
	}

	// Return JSON for backward compatibility with callers expecting JSON
	return json.MarshalIndent(settings, "", "  ")
}

// SeedCommonFiles seeds the common files for a harness template.
// It creates the base directory structure and writes only common files
// (.tmux.conf, .zshrc) that are shared across all harnesses.
// Harness-specific file seeding is handled by each harness's SeedTemplateDir().
func SeedCommonFiles(templateDir, configDirName string, force bool) error {
	homeDir := filepath.Join(templateDir, "home")
	// Create directories
	dirs := []string{
		templateDir,
		homeDir,
		filepath.Join(homeDir, ".config", "gcloud"),
	}
	if configDirName != "" {
		dirs = append(dirs, filepath.Join(homeDir, configDirName))
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Helper to read common embedded file
	readCommonEmbed := func(name string) string {
		data, err := EmbedsFS.ReadFile(filepath.Join("embeds", "common", name))
		if err != nil {
			return ""
		}
		return string(data)
	}

	// Seed common template files
	files := []struct {
		path    string
		content string
		mode    os.FileMode
	}{
		{filepath.Join(homeDir, ".tmux.conf"), readCommonEmbed(".tmux.conf"), 0644},
		{filepath.Join(homeDir, ".zshrc"), readCommonEmbed("zshrc"), 0644},
	}

	for _, f := range files {
		if f.content == "" {
			continue
		}
		if force {
			if err := os.WriteFile(f.path, []byte(f.content), f.mode); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
			continue
		}

		if _, err := os.Stat(f.path); os.IsNotExist(err) {
			if err := os.WriteFile(f.path, []byte(f.content), f.mode); err != nil {
				return fmt.Errorf("failed to write file %s: %w", f.path, err)
			}
		}
	}

	return nil
}

// SeedFileFromFS writes a file from an embed.FS to a target path.
// If force is true, the file is always overwritten. Otherwise, it is only
// written if it does not already exist. alwaysOverwrite can be set to true
// for critical config files that should always match embedded defaults.
func SeedFileFromFS(fs embed.FS, basePath, fileName, targetPath string, force, alwaysOverwrite bool) error {
	data, err := fs.ReadFile(filepath.Join(basePath, fileName))
	if err != nil {
		return nil // File not in embeds, skip silently
	}

	if force || alwaysOverwrite {
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}
		return nil
	}

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}
	}
	return nil
}

// GenerateGroveID creates a grove ID based on git context.
// For git repos with remote: normalized remote URL (e.g., github.com/org/repo)
// For git repos without remote: UUID
// For non-git directories: UUID
func GenerateGroveID() string {
	if util.IsGitRepo() {
		remote := util.GetGitRemote()
		if remote != "" {
			return util.NormalizeGitRemote(remote)
		}
	}
	return uuid.New().String()
}

// GenerateGroveIDForDir creates a grove ID based on git context for the specified directory.
func GenerateGroveIDForDir(dir string) string {
	if util.IsGitRepoDir(dir) {
		remote := util.GetGitRemoteDir(dir)
		if remote != "" {
			return util.NormalizeGitRemote(remote)
		}
	}
	return uuid.New().String()
}

// IsInsideGrove returns true if the current working directory or any parent contains a .scion directory.
func IsInsideGrove() bool {
	_, ok := FindProjectRoot()
	return ok
}

// GetEnclosingGrovePath returns the path to the enclosing .scion directory if one exists,
// along with the root directory containing it.
func GetEnclosingGrovePath() (grovePath string, rootDir string, found bool) {
	wd, err := os.Getwd()
	if err != nil {
		return "", "", false
	}

	dir := wd
	for {
		p := filepath.Join(dir, DotScion)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			if abs, err := filepath.EvalSymlinks(p); err == nil {
				return abs, dir, true
			}
			return p, dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir { // Reached filesystem root
			break
		}
		dir = parent
	}
	return "", "", false
}

// SeedAgnosticTemplate seeds the default agnostic template from embedded files.
// It copies scion-agent.yaml, agents.md, and system-prompt.md into the target directory.
func SeedAgnosticTemplate(targetDir string, force bool) error {
	templateBase := "embeds/templates/default"

	entries, err := EmbedsFS.ReadDir(templateBase)
	if err != nil {
		return fmt.Errorf("failed to read embedded template directory: %w", err)
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create template directory %s: %w", targetDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := EmbedsFS.ReadFile(filepath.Join(templateBase, entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", entry.Name(), err)
		}

		targetPath := filepath.Join(targetDir, entry.Name())
		if !force {
			if _, err := os.Stat(targetPath); err == nil {
				continue // File exists and force is false, skip
			}
		}

		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}
	}

	return nil
}

func InitProject(targetDir string, harnesses []api.Harness) error {
	var projectDir string
	var err error

	if targetDir != "" {
		projectDir = targetDir
	} else {
		projectDir, err = GetTargetProjectDir()
		if err != nil {
			return err
		}
	}

	// Create grove-level settings file if it doesn't exist
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("failed to create settings directory: %w", err)
	}
	// Check if any settings file exists (YAML or JSON)
	settingsPath := GetSettingsPath(projectDir)
	if settingsPath == "" {
		// No settings file exists, seed with default YAML settings
		defaultSettings, err := GetDefaultSettingsDataYAML()
		if err != nil {
			// Fall back to JSON defaults
			defaultSettings, err = GetDefaultSettingsData()
			if err != nil {
				return fmt.Errorf("failed to read default settings: %w", err)
			}
		}
		newSettingsPath := filepath.Join(projectDir, "settings.yaml")
		if err := os.WriteFile(newSettingsPath, defaultSettings, 0644); err != nil {
			return fmt.Errorf("failed to seed settings.yaml: %w", err)
		}
	}

	templatesDir := filepath.Join(projectDir, "templates")
	agentsDir := filepath.Join(projectDir, "agents")
	harnessConfigsDir := filepath.Join(projectDir, harnessConfigsDirName)

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	// Seed default agnostic template
	if err := SeedAgnosticTemplate(filepath.Join(templatesDir, "default"), false); err != nil {
		return fmt.Errorf("failed to seed default agnostic template: %w", err)
	}

	for _, h := range harnesses {
		// Seed harness-config directory
		if err := SeedHarnessConfig(filepath.Join(harnessConfigsDir, h.Name()), h, false); err != nil {
			return fmt.Errorf("failed to seed %s harness-config: %w", h.Name(), err)
		}
		// Keep existing template seeding for backward compatibility
		if err := h.SeedTemplateDir(filepath.Join(templatesDir, h.Name()), false); err != nil {
			return fmt.Errorf("failed to seed %s template: %w", h.Name(), err)
		}
	}

	return nil
}

func InitGlobal(harnesses []api.Harness) error {
	globalDir, err := GetGlobalDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(globalDir, 0755); err != nil {
		return fmt.Errorf("failed to create global directory: %w", err)
	}

	// Create global settings file if it doesn't exist
	settingsPath := GetSettingsPath(globalDir)
	if settingsPath == "" {
		// No settings file exists, seed with default YAML settings
		defaultSettings, err := GetDefaultSettingsDataYAML()
		if err != nil {
			// Fall back to JSON defaults
			defaultSettings, err = GetDefaultSettingsData()
			if err != nil {
				return fmt.Errorf("failed to read default settings: %w", err)
			}
		}
		newSettingsPath := filepath.Join(globalDir, "settings.yaml")
		if err := os.WriteFile(newSettingsPath, defaultSettings, 0644); err != nil {
			return fmt.Errorf("failed to seed global settings.yaml: %w", err)
		}
	}

	templatesDir := filepath.Join(globalDir, "templates")
	agentsDir := filepath.Join(globalDir, "agents")
	harnessConfigsDir := filepath.Join(globalDir, harnessConfigsDirName)

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create global agents directory: %w", err)
	}

	// Seed default agnostic template
	if err := SeedAgnosticTemplate(filepath.Join(templatesDir, "default"), false); err != nil {
		return fmt.Errorf("failed to seed global default agnostic template: %w", err)
	}

	for _, h := range harnesses {
		// Seed harness-config directory
		if err := SeedHarnessConfig(filepath.Join(harnessConfigsDir, h.Name()), h, false); err != nil {
			return fmt.Errorf("failed to seed global %s harness-config: %w", h.Name(), err)
		}
		// Keep existing template seeding for backward compatibility
		if err := h.SeedTemplateDir(filepath.Join(templatesDir, h.Name()), false); err != nil {
			return fmt.Errorf("failed to seed global %s template: %w", h.Name(), err)
		}
	}

	return nil
}
