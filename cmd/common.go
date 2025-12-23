package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/util"
)

func DeleteAgentFiles(agentName string) error {
	var agentsDirs []string
	if projectDir, err := config.GetResolvedProjectDir(grovePath); err == nil {
		agentsDirs = append(agentsDirs, filepath.Join(projectDir, "agents"))
	}
	// Also check global just in case
	if globalDir, err := config.GetGlobalAgentsDir(); err == nil {
		agentsDirs = append(agentsDirs, globalDir)
	}

	for _, dir := range agentsDirs {
		agentDir := filepath.Join(dir, agentName)
		if _, err := os.Stat(agentDir); err != nil {
			continue
		}

		agentWorkspace := filepath.Join(agentDir, "workspace")
		// Check if it's a worktree before trying to remove it
		if _, err := os.Stat(filepath.Join(agentWorkspace, ".git")); err == nil {
			fmt.Printf("Removing git worktree for agent '%s'...\n", agentName)
			if err := util.RemoveWorktree(agentWorkspace); err != nil {
				fmt.Printf("Warning: failed to remove worktree at %s: %v\n", agentWorkspace, err)
			}
		}

		// Also ensure the agent directory is cleaned up
		fmt.Printf("Removing agent directory for '%s'...\n", agentName)
		if err := os.RemoveAll(agentDir); err != nil {
			return fmt.Errorf("failed to remove agent directory: %w", err)
		}
	}
	return nil
}

func ProvisionAgent(agentName string, templateName string, agentImage string, grovePath string, optionalStatus string) (string, string, []*config.Template, error) {
	// 1. Prepare agent directories
	projectDir, err := config.GetResolvedProjectDir(grovePath)
	if err != nil {
		return "", "", nil, err
	}

	groveName := config.GetGroveName(projectDir)

	// Verify .gitignore if in a repo
	if util.IsGitRepo() {
		// Find the projectDir relative to repo root if possible
		root, err := util.RepoRoot()
		if err == nil {
			rel, err := filepath.Rel(root, projectDir)
			if err == nil && !strings.HasPrefix(rel, "..") {
				agentsPath := filepath.Join(rel, "agents")
				if !util.IsIgnored(agentsPath + "/") {
					return "", "", nil, fmt.Errorf("security error: '%s/' must be in .gitignore when using a project-local grove", agentsPath)
				}
			}
		}
	}
	agentsDir := filepath.Join(projectDir, "agents")

	agentDir := filepath.Join(agentsDir, agentName)
	agentHome := filepath.Join(agentDir, "home")
	agentWorkspace := filepath.Join(agentDir, "workspace")

	if err := os.MkdirAll(agentHome, 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create agent home: %w", err)
	}

	// Create empty prompt.md in agent root
	promptFile := filepath.Join(agentDir, "prompt.md")
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		if err := os.WriteFile(promptFile, []byte(""), 0644); err != nil {
			return "", "", nil, fmt.Errorf("failed to create prompt.md: %w", err)
		}
	}

	if util.IsGitRepo() {
		fmt.Printf("Creating git worktree for agent '%s'...\n", agentName)
		// Remove existing workspace dir if it exists to allow worktree add
		os.RemoveAll(agentWorkspace)
		if err := util.CreateWorktree(agentWorkspace, agentName); err != nil {
			return "", "", nil, fmt.Errorf("failed to create git worktree: %w", err)
		}
	} else {
		if err := os.MkdirAll(agentWorkspace, 0755); err != nil {
			return "", "", nil, fmt.Errorf("failed to create agent workspace: %w", err)
		}
	}

	// 2. Load and copy templates
	chain, err := config.GetTemplateChain(templateName)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to load template: %w", err)
	}

	var finalScionCfg *config.ScionConfig

	for _, tpl := range chain {
		fmt.Printf("Applying template: %s\n", tpl.Name)
		if err := util.CopyDir(tpl.Path, agentHome); err != nil {
			return "", "", nil, fmt.Errorf("failed to copy template %s: %w", tpl.Name, err)
		}

		// Load scion.json from this template to see if it specifies an image
		tplCfg, err := tpl.LoadConfig()
		if err == nil {
			finalScionCfg = tplCfg
		}
	}

	// Update agent-specific scion.json
	if finalScionCfg == nil {
		finalScionCfg = &config.ScionConfig{}
	}
	finalScionCfg.Template = templateName
	finalScionCfg.Agent = &config.AgentConfig{
		Grove: groveName,
		Name:  agentName,
	}
	if optionalStatus != "" {
		finalScionCfg.Agent.Status = optionalStatus
	}
	if agentImage != "" {
		finalScionCfg.Image = agentImage
	}
	agentCfgData, _ := json.MarshalIndent(finalScionCfg, "", "  ")
	os.WriteFile(filepath.Join(agentHome, "scion.json"), agentCfgData, 0644)

	return agentHome, agentWorkspace, chain, nil
}