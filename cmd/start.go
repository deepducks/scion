package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
)

var (
	agentName    string
	templateName string
	agentImage   string
	noAuth       bool
	attach       bool
	model        string
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:     "start <agent-name> [task...]",
	Aliases: []string{"run"},
	Short:   "Launch a new scion agent",
	Long: `Provision and launch a new isolated LLM agent to perform a specific task.
The agent will be created from a template and run in a detached container.

The agent-name is required as the first argument. All subsequent arguments 
form the task prompt. If no task arguments are provided, the agent will 
look for a prompt.md file in its root directory.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName = args[0]
		task := strings.Join(args[1:], " ")

		// 0. Check if container already exists
		rt := runtime.GetRuntime()
		agents, err := rt.List(context.Background(), nil)
		if err == nil {
			for _, a := range agents {
				if a.ID == agentName || a.Name == agentName {
					if a.Status == "running" {
						fmt.Printf("Agent container '%s' is already running.\n", agentName)
						if attach {
							fmt.Printf("Attaching to agent '%s'...\n", agentName)
							return rt.Attach(context.Background(), a.ID)
						}
						return nil
					}
					// If it exists but not running, we should probably delete it or error
					// since 'run' with a name usually fails if name is taken.
					// For scion, let's follow the user's lead.
					// If we want to "gracefully handle", maybe we delete and recreate if we're doing a 'start'?
					// Or maybe just try to 'run' and let it fail if that's what's expected.
					// Actually, the user said "start and run are really aliases as we will not be creating an actual runtime container in an unstarted state"
					// This suggests if it exists but is not running, it might be in a weird state.
					fmt.Printf("Agent container '%s' exists but is not running (Status: %s). Re-starting...\n", agentName, a.Status)
					_ = rt.Delete(context.Background(), a.ID)
				}
			}
		}

		projectDir, err := config.GetResolvedProjectDir(grovePath)
		if err != nil {
			return err
		}
		groveName := config.GetGroveName(projectDir)
		agentsDir := filepath.Join(projectDir, "agents")
		agentDir := filepath.Join(agentsDir, agentName)

		promptFile := filepath.Join(agentDir, "prompt.md")
		promptFileContent := ""
		if content, err := os.ReadFile(promptFile); err == nil {
			promptFileContent = strings.TrimSpace(string(content))
		}

		if task == "" && promptFileContent == "" {
			return fmt.Errorf("no task provided: prompt.md is empty and no task arguments were given")
		}

		if task != "" && promptFileContent != "" {
			return fmt.Errorf("task conflict: both prompt.md and command line arguments provide a task")
		}

		if task == "" {
			task = promptFileContent
		} else if promptFileContent == "" {
			// Update prompt.md for posterity if it was empty
			_ = os.WriteFile(promptFile, []byte(task), 0644)
		}

		agentHome := filepath.Join(agentDir, "home")
		agentWorkspace := filepath.Join(agentDir, "workspace")

		var finalScionCfg *config.ScionConfig

		if _, err := os.Stat(agentDir); os.IsNotExist(err) {
			fmt.Printf("Provisioning agent '%s'...\n", agentName)
			var chain []*config.Template
			agentHome, agentWorkspace, chain, err = ProvisionAgent(agentName, templateName, agentImage, grovePath, "")
			if err != nil {
				return err
			}
			// Get the final config from the chain
			for _, tpl := range chain {
				tplCfg, err := tpl.LoadConfig()
				if err == nil {
					finalScionCfg = tplCfg
				}
			}
		} else {
			fmt.Printf("Using existing agent '%s'...\n", agentName)
			// Load from existing agent's scion.json
			tpl := &config.Template{Path: agentHome}
			finalScionCfg, err = tpl.LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load agent config: %w", err)
			}
		}

		fmt.Printf("Starting agent '%s' for task: %s\n", agentName, task)

		// Resolve image
		resolvedImage := ""
		if finalScionCfg != nil && finalScionCfg.Image != "" {
			resolvedImage = finalScionCfg.Image
		}
		// Flag takes ultimate precedence
		if agentImage != "" {
			resolvedImage = agentImage
		}
		if resolvedImage == "" {
			resolvedImage = "gemini-cli-sandbox"
		}

		// 3. Propagate credentials
		var auth config.AuthConfig
		if !noAuth {
			// Load agent settings from the home directory
			agentSettingsPath := filepath.Join(agentHome, ".gemini", "settings.json")
			agentSettings, _ := config.LoadGeminiSettings(agentSettingsPath)
			auth = config.DiscoverAuth(agentSettings)
		}

		// 4. Launch container
		rt = runtime.GetRuntime()

		detached := true
		useTmux := false
		resolvedModel := "flash"
		unixUsername := "node"

		if finalScionCfg != nil {
			detached = finalScionCfg.IsDetached()
			if finalScionCfg.UseTmux {
				useTmux = true
			}
			if finalScionCfg.Model != "" {
				resolvedModel = finalScionCfg.Model
			}
			if finalScionCfg.UnixUsername != "" {
				unixUsername = finalScionCfg.UnixUsername
			}
		}

		// -a flag overrides detached config
		if cmd.Flags().Changed("attach") {
			detached = !attach
		}

		if model != "" {
			resolvedModel = model
		}

		if useTmux {
			tmuxImage := resolvedImage
			if !strings.Contains(tmuxImage, ":") {
				tmuxImage = tmuxImage + ":tmux"
			} else {
				parts := strings.SplitN(resolvedImage, ":", 2)
				tmuxImage = parts[0] + ":tmux"
			}

			exists, err := rt.ImageExists(context.Background(), tmuxImage)
			if err != nil || !exists {
				return fmt.Errorf("tmux support requested but image '%s' not found. Please ensure the image has a :tmux tag.", tmuxImage)
			}
			resolvedImage = tmuxImage
		}

		agentEnv := []string{
			fmt.Sprintf("GEMINI_AGENT_NAME=%s", agentName),
		}
		if !strings.HasPrefix(strings.TrimSpace(config.DefaultSystemPrompt), "# Placeholder") {
			agentEnv = append(agentEnv, fmt.Sprintf("GEMINI_SYSTEM_MD=/home/%s/.gemini/system_prompt.md", unixUsername))
		}

		template := ""
		if finalScionCfg != nil {
			template = finalScionCfg.Template
		}

		runCfg := runtime.RunConfig{
			Name:         agentName,
			Template:     template,
			UnixUsername: unixUsername,
			Image:        resolvedImage,
			HomeDir:      agentHome,
			Workspace:    agentWorkspace,
			Auth:         auth,
			UseTmux:      useTmux,
			Model:        resolvedModel,
			Task:         task,
			Env:          agentEnv,
			Labels: map[string]string{
				"scion.agent":      "true",
				"scion.name":       agentName,
				"scion.grove":      groveName,
				"scion.grove_path": projectDir,
			},
		}

		id, err := rt.Run(context.Background(), runCfg)
		if err != nil {
			return fmt.Errorf("failed to launch container: %w", err)
		}

		if detached {
			fmt.Printf("Agent '%s' launched successfully (ID: %s)\n", agentName, id)
		} else {
			fmt.Printf("Attaching to agent '%s'...\n", agentName)
			return rt.Attach(context.Background(), id)
		}

		return nil
	},
}


func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().StringVarP(&templateName, "type", "t", "default", "Template to use")
	startCmd.Flags().StringVarP(&agentImage, "image", "i", "", "Container image to use (overrides template)")
	startCmd.Flags().BoolVar(&noAuth, "no-auth", false, "Disable authentication propagation")
	startCmd.Flags().BoolVarP(&attach, "attach", "a", false, "Attach to the agent TTY after starting")
	startCmd.Flags().StringVarP(&model, "model", "m", "", "Model to use (overrides template)")
}
			
