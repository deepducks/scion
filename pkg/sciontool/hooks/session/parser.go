/*
Copyright 2025 The Scion Authors.
*/

// Package session provides utilities for parsing agent session files.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionMetrics contains aggregated metrics from a session.
type SessionMetrics struct {
	// TokensInput is the total number of input tokens used.
	TokensInput int `json:"tokens_input"`
	// TokensOutput is the total number of output tokens generated.
	TokensOutput int `json:"tokens_output"`
	// TokensCached is the number of tokens served from cache.
	TokensCached int `json:"tokens_cached"`
	// ToolCalls maps tool names to their usage statistics.
	ToolCalls map[string]ToolStats `json:"tool_calls"`
	// Model is the model identifier used in the session.
	Model string `json:"model"`
	// Duration is the total session duration.
	Duration time.Duration `json:"duration"`
	// TurnCount is the number of conversation turns.
	TurnCount int `json:"turn_count"`
}

// ToolStats contains statistics for a single tool.
type ToolStats struct {
	// Calls is the total number of invocations.
	Calls int `json:"calls"`
	// Success is the number of successful invocations.
	Success int `json:"success"`
	// Errors is the number of failed invocations.
	Errors int `json:"errors"`
}

// geminiSession represents the structure of a Gemini CLI session file.
type geminiSession struct {
	SessionID   string               `json:"sessionId"`
	StartTime   string               `json:"startTime"`
	EndTime     string               `json:"endTime"`
	Model       string               `json:"model"`
	Turns       []geminiTurn         `json:"turns"`
	TokenUsage  geminiTokenUsage     `json:"tokenUsage"`
	ToolResults []geminiToolResult   `json:"toolResults"`
}

// geminiTurn represents a single turn in the conversation.
type geminiTurn struct {
	Role         string            `json:"role"`
	Content      string            `json:"content"`
	Model        string            `json:"model"`
	TokenUsage   *geminiTokenUsage `json:"tokenUsage,omitempty"`
	ToolCalls    []geminiToolCall  `json:"toolCalls,omitempty"`
}

// geminiTokenUsage represents token usage statistics.
type geminiTokenUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	CachedTokens int `json:"cachedTokens"`
}

// geminiToolCall represents a tool invocation.
type geminiToolCall struct {
	Name   string      `json:"name"`
	Input  interface{} `json:"input"`
	Output interface{} `json:"output"`
}

// geminiToolResult represents a tool execution result.
type geminiToolResult struct {
	ToolName string `json:"toolName"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// ParseSessionFile parses a Gemini CLI session file and extracts metrics.
func ParseSessionFile(path string) (*SessionMetrics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}

	var session geminiSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parsing session JSON: %w", err)
	}

	return extractMetrics(&session)
}

// extractMetrics extracts aggregated metrics from a parsed session.
func extractMetrics(session *geminiSession) (*SessionMetrics, error) {
	metrics := &SessionMetrics{
		ToolCalls: make(map[string]ToolStats),
		Model:     session.Model,
		TurnCount: len(session.Turns),
	}

	// Aggregate token usage from session-level stats
	metrics.TokensInput = session.TokenUsage.InputTokens
	metrics.TokensOutput = session.TokenUsage.OutputTokens
	metrics.TokensCached = session.TokenUsage.CachedTokens

	// If no session-level stats, aggregate from turns
	if metrics.TokensInput == 0 && metrics.TokensOutput == 0 {
		for _, turn := range session.Turns {
			if turn.TokenUsage != nil {
				metrics.TokensInput += turn.TokenUsage.InputTokens
				metrics.TokensOutput += turn.TokenUsage.OutputTokens
				metrics.TokensCached += turn.TokenUsage.CachedTokens
			}

			// Get model from first turn if not set at session level
			if metrics.Model == "" && turn.Model != "" {
				metrics.Model = turn.Model
			}
		}
	}

	// Aggregate tool call stats from turns
	for _, turn := range session.Turns {
		for _, tc := range turn.ToolCalls {
			stats := metrics.ToolCalls[tc.Name]
			stats.Calls++
			// Assume success unless we find an error in ToolResults
			stats.Success++
			metrics.ToolCalls[tc.Name] = stats
		}
	}

	// Update tool stats with error information from ToolResults
	for _, tr := range session.ToolResults {
		stats := metrics.ToolCalls[tr.ToolName]
		if !tr.Success {
			// Move from success to error count
			if stats.Success > 0 {
				stats.Success--
			}
			stats.Errors++
		}
		metrics.ToolCalls[tr.ToolName] = stats
	}

	// Calculate duration from timestamps
	if session.StartTime != "" && session.EndTime != "" {
		startTime, err := parseTime(session.StartTime)
		if err == nil {
			endTime, err := parseTime(session.EndTime)
			if err == nil {
				metrics.Duration = endTime.Sub(startTime)
			}
		}
	}

	return metrics, nil
}

// parseTime parses a timestamp string in various formats.
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// FindLatestSession finds the most recently modified session file in a directory.
func FindLatestSession(sessionDir string) (string, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return "", fmt.Errorf("reading session directory: %w", err)
	}

	var sessionFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "session-") && strings.HasSuffix(e.Name(), ".json") {
			sessionFiles = append(sessionFiles, e)
		}
	}

	if len(sessionFiles) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionDir)
	}

	// Sort by modification time (newest first)
	sort.Slice(sessionFiles, func(i, j int) bool {
		iInfo, _ := sessionFiles[i].Info()
		jInfo, _ := sessionFiles[j].Info()
		if iInfo == nil || jInfo == nil {
			return false
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return filepath.Join(sessionDir, sessionFiles[0].Name()), nil
}

// DefaultSessionDir returns the default Gemini session directory.
func DefaultSessionDir() string {
	// Priority 1: Check if /home/scion exists (standard agent home)
	if _, err := os.Stat("/home/scion"); err == nil {
		return filepath.Join("/home/scion", ".gemini", "sessions")
	}
	// Priority 2: Use HOME env var
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/scion"
	}
	return filepath.Join(home, ".gemini", "sessions")
}

// ParseLatestSession finds and parses the most recent session file.
func ParseLatestSession(sessionDir string) (*SessionMetrics, error) {
	if sessionDir == "" {
		sessionDir = DefaultSessionDir()
	}

	path, err := FindLatestSession(sessionDir)
	if err != nil {
		return nil, err
	}

	return ParseSessionFile(path)
}
