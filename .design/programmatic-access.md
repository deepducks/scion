# Programmatic API & Event-Driven Triggers

## 1. Overview

The current Scion architecture is primarily designed for a human user interacting via a CLI on their local machine. To scale Scion to team workflows, we need to support **automation triggers** (CI/CD pipelines) and **interactive bots** (Slack/Discord).

This design document proposes decoupling the core agent lifecycle logic from the CLI implementation, exposing a stable Go API (`pkg/agent`), and defining the architecture for external projects (`scion-slack`, `scion-ci`) to consume this API.

## 2. Problem Statement

Currently, Scion's logic is embedded in `cmd/` packages (e.g., `cmd/start.go`, `cmd/common.go`), making it difficult to import and reuse in other Go applications.

**Key Limitations:**
*   **CLI Coupling**: Logic relies on global flags (`startCmd.Flags()`) and direct stdout printing.
*   **Filesystem Assumptions**: Hardcoded reliance on local `.scion` directories and `os.Getwd()` for context.
*   **Lack of Feedback Loop**: External programs cannot easily subscribe to agent events (errors, "waiting for input", completion) without parsing logs or polling files.

## 3. Core Structural Changes

We will refactor the codebase to separate the **User Interface (CLI)** from the **Business Logic (Core)**.

### 3.1. New Package Structure

We will introduce `pkg/agent` (or expand `pkg/api`) to house the core logic.

```go
// pkg/agent/manager.go

type Manager interface {
    // Start launches a new agent with the given configuration
    Start(ctx context.Context, opts StartOptions) (*Agent, error)

    // Stop terminates an agent
    Stop(ctx context.Context, agentID string) error

    // List returns active agents
    List(ctx context.Context, filter Filter) ([]*Agent, error)

    // Watch returns a channel of status updates for an agent
    Watch(ctx context.Context, agentID string) (<-chan StatusEvent, error)
}

type StartOptions struct {
    Name        string
    Task        string
    Template    string
    Image       string
    GrovePath   string // Path to workspace/config context
    Env         map[string]string
    Detached    bool
    // ... other config overrides
}
```

### 3.2. Refactoring `cmd/`

The `cmd/` packages will become thin wrappers around `pkg/agent`.

*   `cmd/start.go` -> Parses flags -> Calls `agent.Manager.Start()`.
*   `cmd/list.go` -> Calls `agent.Manager.List()` -> Formats output.

### 3.3. Status & Event Stream

To support remote bots (Slack), we need a way to push updates.

*   **Current**: Agents write to `.gemini-status.json`.
*   **Proposed**: The `Manager` will implement a `Watch` method that polls the status file (or connects to the runtime event stream) and emits Go events.
*   **Events**:
    *   `StatusChanged` (Starting -> Thinking -> Waiting)
    *   `OutputReceived` (Stream of logs/thoughts)
    *   `ErrorOccurred`

## 4. Supporting Projects (External)

These projects will reside in separate repositories (or separate modules) but depend on `scion/pkg/...`.

### 4.1. Scion Slack Bot (`scion-slack`)

A long-running service that listens to chat events and manages agents.

**Architecture:**
1.  **Listener**: Receives slash commands (`/scion fix this`) or mentions.
2.  **Context Resolution**: Maps a Slack Channel ID to a Scion Grove (likely a persistent directory on the bot's server/PVC).
3.  **Execution**:
    *   Bot calls `manager.Start(opts)`.
    *   Bot starts a goroutine to `Watch()` the agent.
4.  **Feedback**:
    *   When `Watch()` emits `Status=WaitingForInput`, the bot posts a message with buttons (Approve/Reject).
    *   User clicks button -> Bot writes to Agent's input pipe/file.

**Implementation Strategy:**
*   Use `slack-go/slack`.
*   Mount a Persistent Volume (PVC) at `/data` to serve as the "Grove".
*   Each Slack thread could be a separate Agent or Grove context.

### 4.2. CI/CD Automation (`scion-action`)

A GitHub Action / GitLab Step to autonomously fix or review code.

**Workflow:**
1.  **Trigger**: Pull Request opened.
2.  **Environment**: The Action container runs `scion`.
3.  **Context**: The repo is checked out by the CI runner.
4.  **Execution**:
    *   `scion` is invoked programmatically (or via CLI) to "Review this PR".
    *   Agent generates comments or a fix commit.
5.  **Output**: structured results (JSON) parsed by the Action to create PR comments.

## 5. Implementation Plan

### Phase 1: Core Refactoring (Internal)
1.  Create `pkg/agent` package.
2.  Move `ProvisionAgent`, `RunAgent`, `GetAgent` from `cmd/common.go` to `pkg/agent`.
3.  Convert function signatures to use `StartOptions` struct instead of loose arguments.
4.  Update `cmd/*` to use the new package.

### Phase 2: Programmatic Features
1.  Implement `Watch()` in `pkg/agent/manager.go` (file polling or runtime events).
2.  Add `WriteInput(input string)` to `pkg/agent` to allow programmatic replies to prompts without `scion attach`.

### Phase 3: External Proof of Concept
1.  Create `examples/simple-bot`: A minimal Go program that starts an agent and prints status updates to stdout without using Cobra.

## 6. Challenges & Considerations

*   **Authentication**:
    *   CLI relies on user's `gcloud` login or local keys.
    *   Bots/CI need **Service Account** support or API Key injection via Env Vars (`GOOGLE_API_KEY`, `GITHUB_TOKEN`).
    *   *Solution*: Ensure `StartOptions` accepts explicit Auth credentials to inject into the container.
*   **Concurrency**:
    *   A Slack bot might manage 100 agents.
    *   The `Manager` needs to be thread-safe.
*   **Persistence**:
    *   If the Bot container restarts, it needs to "adopt" running agents.
    *   `runtime.List()` already supports this (reconciliation).
