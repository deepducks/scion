/*
Copyright 2025 The Scion Authors.
*/

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptone/scion-agent/pkg/sciontool/log"
)

func TestStatusCommand(t *testing.T) {
	// Create a temp directory for test files
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	tests := []struct {
		name            string
		args            []string
		wantErr         bool
		wantStatus      string
		wantLogContains string
	}{
		{
			name:            "ask_user with message",
			args:            []string{"status", "ask_user", "What should I do?"},
			wantStatus:      "WAITING_FOR_INPUT",
			wantLogContains: "Agent requested input: What should I do?",
		},
		{
			name:            "ask_user with default message",
			args:            []string{"status", "ask_user"},
			wantStatus:      "WAITING_FOR_INPUT",
			wantLogContains: "Agent requested input: Input requested",
		},
		{
			name:            "task_completed with message",
			args:            []string{"status", "task_completed", "Finished the feature"},
			wantStatus:      "COMPLETED",
			wantLogContains: "Agent completed task: Finished the feature",
		},
		{
			name:            "task_completed with default message",
			args:            []string{"status", "task_completed"},
			wantStatus:      "COMPLETED",
			wantLogContains: "Agent completed task: Task completed",
		},
		{
			name:            "ask_user with multi-word message",
			args:            []string{"status", "ask_user", "Which", "option", "do", "you", "prefer?"},
			wantStatus:      "WAITING_FOR_INPUT",
			wantLogContains: "Agent requested input: Which option do you prefer?",
		},
		{
			name:            "limits_exceeded with message",
			args:            []string{"status", "limits_exceeded", "max_turns of 50 exceeded"},
			wantStatus:      "LIMITS_EXCEEDED",
			wantLogContains: "Agent limits exceeded: max_turns of 50 exceeded",
		},
		{
			name:            "limits_exceeded with default message",
			args:            []string{"status", "limits_exceeded"},
			wantStatus:      "LIMITS_EXCEEDED",
			wantLogContains: "Agent limits exceeded: Agent limits exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up files before each test
			statusFile := filepath.Join(tempDir, "agent-info.json")
			logFile := filepath.Join(tempDir, "agent.log")
			os.Remove(statusFile)
			os.Remove(logFile)
			log.SetLogPath(logFile)

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Check the status file was created with correct session status
			data, err := os.ReadFile(statusFile)
			if err != nil {
				t.Fatalf("Failed to read status file: %v", err)
			}

			var info map[string]string
			if err := json.Unmarshal(data, &info); err != nil {
				t.Fatalf("Failed to parse status file: %v", err)
			}

			if got := info["status"]; got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}

			// Check the log file contains the expected message
			logData, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatalf("Failed to read log file: %v", err)
			}

			if !strings.Contains(string(logData), tt.wantLogContains) {
				t.Errorf("log file does not contain %q, got: %s", tt.wantLogContains, logData)
			}
		})
	}
}

func TestStatusCommandUnknownType(t *testing.T) {
	tempDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"status", "unknown_type"})

	_ = rootCmd.Execute()

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("unknown status type")) {
		t.Errorf("expected error message about unknown status type, got: %s", output)
	}
}
