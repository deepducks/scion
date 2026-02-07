package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/spf13/cobra"
)

var deleteStopped bool

// deleteCmd represents the delete command
var deleteCmd = &cobra.Command{
	Use:               "delete <agent>",
	Aliases:           []string{"rm"},
	Short:             "Delete an agent",
	Long:              `Stop and remove an agent container and its associated files and worktree.`,
	ValidArgsFunction: getAgentNames,
	Args: func(cmd *cobra.Command, args []string) error {
		if deleteStopped {
			if len(args) > 0 {
				return fmt.Errorf("no arguments allowed when using --stopped")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires exactly 1 argument (agent name)")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := config.GetResolvedProjectDir(grovePath)
		if preserveBranch && !util.IsGitRepoDir(projectDir) {
			fmt.Println("Warning: --preserve-branch used outside a git repository; this flag has no effect.")
		}

		// For delete with --stopped, we can't specify a target agent
		// For single agent delete, we pass the target agent name to exclude it from sync requirements
		var targetAgent string
		if !deleteStopped && len(args) > 0 {
			targetAgent = args[0]
		}

		// Check if Hub should be used, excluding the target agent from sync requirements
		hubCtx, err := CheckHubAvailabilityForAgent(grovePath, targetAgent, false)
		if err != nil {
			return err
		}

		if deleteStopped {
			// --stopped flag with Hub is not yet supported
			if hubCtx != nil {
				return fmt.Errorf("--stopped flag is not yet supported when using Hub integration\n\nTo delete stopped agents locally, use: scion --no-hub delete --stopped")
			}

			rt := runtime.GetRuntime(grovePath, profile)
			mgr := agent.NewManager(rt)
			agents, err := mgr.List(context.Background(), nil)
			if err != nil {
				return err
			}

			var deletedCount int
			for _, a := range agents {
				if a.ID == "" {
					continue // No container
				}

				status := strings.ToLower(a.ContainerStatus)
				// Check if running
				if strings.HasPrefix(status, "up") ||
					strings.HasPrefix(status, "running") ||
					strings.HasPrefix(status, "pending") ||
					strings.HasPrefix(status, "restarting") {
					continue
				}

				fmt.Printf("Deleting stopped agent '%s' (status: %s)...\n", a.Name, a.ContainerStatus)

				targetGrovePath := a.GrovePath
				if targetGrovePath == "" {
					targetGrovePath = grovePath
				}

				branchDeleted, err := mgr.Delete(context.Background(), a.Name, true, targetGrovePath, !preserveBranch)
				if err != nil {
					fmt.Printf("Failed to delete agent '%s': %v\n", a.Name, err)
					continue
				}

				if branchDeleted {
					fmt.Printf("Git branch associated with agent '%s' deleted.\n", a.Name)
				}
				fmt.Printf("Agent '%s' deleted.\n", a.Name)
				deletedCount++
			}

			if deletedCount == 0 {
				fmt.Println("No stopped agents found.")
			}
			return nil
		}

		agentName := args[0]

		// Use Hub if available
		if hubCtx != nil {
			return deleteAgentViaHub(hubCtx, agentName)
		}

		// Local mode
		effectiveProfile := profile
		if effectiveProfile == "" {
			effectiveProfile = agent.GetSavedRuntime(agentName, grovePath)
		}

		rt := runtime.GetRuntime(grovePath, effectiveProfile)
		mgr := agent.NewManager(rt)

		fmt.Printf("Deleting agent '%s'...\n", agentName)

		// We check if it exists in List to provide better feedback
		agents, _ := mgr.List(context.Background(), map[string]string{"scion.name": agentName})
		containerFound := false
		for _, a := range agents {
			if a.Name == agentName || a.ID == agentName || strings.TrimPrefix(a.Name, "/") == agentName {
				containerFound = true
				break
			}
		}

		if !containerFound {
			fmt.Println("No container found, removing agent definition...")
		}

		branchDeleted, err := mgr.Delete(context.Background(), agentName, true, grovePath, !preserveBranch)
		if err != nil {
			return err
		}

		if branchDeleted {
			fmt.Printf("Git branch associated with agent '%s' deleted.\n", agentName)
		}

		fmt.Printf("Agent '%s' deleted.\n", agentName)
		return nil
	},
}

func deleteAgentViaHub(hubCtx *HubContext, agentName string) error {
	PrintUsingHub(hubCtx.Endpoint)

	fmt.Printf("Deleting agent '%s'...\n", agentName)

	opts := &hubclient.DeleteAgentOptions{
		DeleteFiles:  true,
		RemoveBranch: !preserveBranch,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use grove-scoped client which supports agent lookup by name/slug
	if err := hubCtx.Client.GroveAgents(hubCtx.GroveID).Delete(ctx, agentName, opts); err != nil {
		return wrapHubError(fmt.Errorf("failed to delete agent via Hub: %w", err))
	}

	fmt.Printf("Agent '%s' deleted via Hub.\n", agentName)
	return nil
}

var preserveBranch bool

func init() {
	rootCmd.AddCommand(deleteCmd)
	deleteCmd.Flags().BoolVarP(&preserveBranch, "preserve-branch", "b", false, "Preserve the git branch associated with the worktree")
	deleteCmd.Flags().BoolVar(&deleteStopped, "stopped", false, "Delete all agents with stopped containers")
}
