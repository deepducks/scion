# Workflows: First-Class Hub Entity for Deterministic Flows

## Status
**Design** | April 2026

## Problem

Scion's Hub today models one primary compute entity: the **Agent**, a long-lived LLM harness container provisioned via a Runtime Broker, backed by a git worktree and a tmux session. This model fits interactive, open-ended tasks, but it is a poor fit for a second class of work the platform is increasingly being asked to host:

- **Deterministic, short-lived flows**: a sequence of HTTP calls, shell commands, and fan-out/fan-in steps that terminates with a result. No conversation, no worktree, no tmux, no human attachment.
- **Scheduled or event-driven invocations**: "every morning at 09:00 UTC, run the nightly-report flow" or "when an agent finishes task X, run flow Y to post results to Slack."
- **Agent-initiated side effects**: an agent executing a task decides it needs to run a small, well-defined procedure (call an API, fan out to sub-tasks, synthesize a structured result) and wants a sandboxed, auditable way to do so without re-provisioning a full agent.

Today, the only way to express these on Scion is to bend the agent model: provision a harness, shell into it, run a script. That carries worktree cost, harness cost, PTY cost, JWT scope bloat, and does not produce a first-class record of "this flow ran, here is its result, here is its trace."

duckflux, the YAML-based workflow DSL, is the right tool for these flows. It ships a runtime (`quack`) that accepts a workflow file, an inputs envelope, and a trace directory, and emits a JSON result on stdout plus structured traces on disk. The runtime is self-contained: `exec`, `http`, `loop`, `parallel`, CEL evaluation, and `retry` are all owned by duckflux. Scion does not need to re-implement any of this.

This document specifies how to integrate duckflux into Scion's hosted architecture as a peer to the agent entity, reusing the existing Hub, broker, and auth stack without duplicating execution semantics in Go.

### Goals

1. **Workflow as a distinct Hub entity.** A new `WorkflowRun` table and lifecycle, not a variant of the existing agent schema.
2. **One Scion-native CLI verb: `scion workflow run`.** Dispatches locally (subprocess to `quack`) or remotely (Hub + broker + ephemeral container).
3. **Reuse broker and runtime abstractions.** The broker provisions an ephemeral container from the default agent image (where `quack` is baked in), runs the workflow, streams logs, and persists results. No worktree, no tmux, no harness.
4. **Stable Scion&#x2194;quack contract.** Subprocess stdio, documented flags, documented exit codes. Scion never parses partial quack internals.
5. **Forward compatibility with scheduling and MCP.** Design must support a future `scion schedule` event kind `workflow.run` and a future `workflow_run` MCP tool exposed to agents, without rework of the core entity or dispatch path.

### Non-Goals (This Iteration)

- **WorkflowDefinition registry**: catalog of named, versioned workflow YAMLs with server-side storage and reuse across runs. v1 ships with inline `source_yaml` only. Registry is a follow-up entity (see Section 13).
- **Workflow versioning and inter-run dependencies**: no DAGs of workflow runs, no "run B after A succeeds." duckflux already supports sub-workflows via its own semantics; this is not a Hub concern.
- **Hub-level retry policy**: duckflux has `retry` at the step level per its SPEC v0.7. Scion does not re-implement retry above the runtime.
- **Scheduled workflows**: deferred to Phase 4 (see Section 10).
- **MCP tool for agents**: deferred to Phase 4 (see Section 11).
- **Live step-event streaming**: today `quack run` emits only the final stdout result plus trace files. Real-time per-step progress requires a future `--progress-events` flag on quack. v1 streams stdout and tails the trace directory; step-level events are a future enhancement.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────┐
│                          Scion Hub                               │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                  WorkflowRun Dispatcher                  │    │
│  │                                                          │    │
│  │  ┌────────────────┐     ┌────────────────────────────┐   │    │
│  │  │ POST /runs     │     │ Control Channel emitter    │   │    │
│  │  │ (HTTP handler) │────▶│ (run_workflow command)     │   │    │
│  │  └────────────────┘     └──────────┬─────────────────┘   │    │
│  │                                    │                     │    │
│  └────────────────────────────────────┼─────────────────────┘    │
│                                       │                          │
│  ┌─────────────────────┐  ┌───────────┴────────────┐             │
│  │  ent/WorkflowRun    │  │  Blob storage (traces) │             │
│  └─────────────────────┘  └────────────────────────┘             │
└───────────────────────────┬──────────────────────────────────────┘
                            │  WSS Control Channel
                            ▼
┌──────────────────────────────────────────────────────────────────┐
│                     Runtime Broker                               │
│                                                                  │
│   ┌────────────────────────────────────────────────────────┐     │
│   │  WorkflowRun Executor (thin provisioning path)         │     │
│   │                                                        │     │
│   │  - pkg/runtime.Create(ephemeral, default image)        │     │
│   │  - write /workflow/source.yaml                         │     │
│   │  - write /workflow/inputs.json                         │     │
│   │  - exec: quack run /workflow/source.yaml               │     │
│   │          --input-file /workflow/inputs.json            │     │
│   │          --trace-dir /workflow/trace/                  │     │
│   │  - stream stdout/stderr back over stream frames        │     │
│   │  - on exit: upload trace dir, post result/status       │     │
│   └────────────────────────────────────────────────────────┘     │
└───────────────────────────┬──────────────────────────────────────┘
                            │ docker / podman / k8s
                            ▼
┌──────────────────────────────────────────────────────────────────┐
│                   Ephemeral Runner Container                     │
│                                                                  │
│          default agent image (quack baked into /usr/local/bin)   │
│                                                                  │
│          quack (standalone Bun binary)                           │
│          └─ exec / http / loop / parallel / CEL / retry          │
└──────────────────────────────────────────────────────────────────┘
```

The key separation: **Scion owns state, dispatch, authorization, and isolation; duckflux owns workflow execution semantics.** Nothing that duckflux already does (exec, http, loop, CEL, retry) is re-implemented in Go. The Go side invokes `quack` as a subprocess inside an isolated container and persists the outcome.

The ephemeral runner is a pared-down variant of the agent container:
- Same base image (`scion/default-agent:latest`).
- No git worktree mount (a workflow does not need the project tree; if it does, it explicitly declares volumes via the broker command payload).
- No tmux, no harness, no PTY.
- One foreground process: `quack run ...`.
- Container is deleted once the run reaches a terminal state and the trace directory is uploaded.

---

## Detailed Design

### 1. Entity Model: `WorkflowRun`

**New ent schema:** `pkg/hub/ent/schema/workflowrun.go`

```go
// WorkflowRun represents a single invocation of a duckflux workflow,
// dispatched by the Hub and executed in an ephemeral container on a broker.
type WorkflowRun struct {
    ent.Schema
}

func (WorkflowRun) Fields() []ent.Field {
    return []ent.Field{
        field.String("id").Unique(),                     // UUID
        field.String("grove_id"),                        // FK → groves.id
        field.String("broker_id").Optional().Nillable(), // FK → runtime_brokers.id (nil until dispatched)

        field.Text("source_yaml"),                       // Inline workflow YAML (v1 only supports inline)
        field.Text("inputs_json").Default("{}"),         // JSON envelope passed to quack

        field.Enum("status").Values(
            "queued", "provisioning", "running",
            "succeeded", "failed", "canceled", "timed_out",
        ).Default("queued"),

        field.Text("result_json").Optional(),            // Final stdout JSON from quack (on succeeded)
        field.Text("error").Optional(),                  // Human-readable error (on failed/timed_out)
        field.String("trace_url").Optional(),            // Signed URL / blob key for uploaded trace dir

        field.Int32("exit_code").Optional().Nillable(),  // quack exit code (0/1/2)

        field.Time("started_at").Optional().Nillable(),  // Set on status → running
        field.Time("finished_at").Optional().Nillable(), // Set on any terminal status

        field.String("created_by_user_id").Optional(),   // User OAuth principal
        field.String("created_by_agent_id").Optional(),  // Agent JWT principal (Phase 4)

        field.Time("created_at").Default(time.Now),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

func (WorkflowRun) Edges() []ent.Edge {
    return []ent.Edge{
        edge.From("grove", Grove.Type).
            Ref("workflow_runs").Unique().Required().Field("grove_id"),
        edge.From("broker", RuntimeBroker.Type).
            Ref("workflow_runs").Unique().Field("broker_id"),
    }
}

func (WorkflowRun) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("grove_id", "status"),
        index.Fields("broker_id", "status"),
        index.Fields("created_at"),
    }
}
```

**Notes on field choices:**

- `source_yaml` is stored inline. This keeps v1 schema-free on the definition side. Large YAML is rare (workflow files typically &lt; 50 KB); if this becomes a storage concern, the field can be migrated to a blob reference later without changing the external API shape.
- `result_json` is stored inline. Per Scion's existing artifact convention, if a single run emits a very large result (&gt; 256 KB), the broker should write it to blob storage and store a reference here instead. v1 writes inline up to a configurable cap and spills to blob above it; the field is still `result_json`, but the client always reads via the Hub which resolves blob references transparently.
- `trace_url` points to the uploaded trace directory (signed URL for users, blob key for server-side consumers). Uploaded after the run terminates.
- `broker_id` is nullable because a run sits in `queued` before the dispatcher picks a broker.
- Exactly one of `created_by_user_id` or `created_by_agent_id` is set. This is enforced at the handler level, not by the schema, since both columns are logically an oneOf.

**WorkflowDefinition is explicitly out of scope for v1.** When the registry lands, it will be a separate schema (`workflowdefinition.go`) referenced from `WorkflowRun` via an optional `definition_id` field plus a `definition_version` string. The inline `source_yaml` path stays supported as the escape hatch for ad-hoc runs.

### 2. Lifecycle

```
            ┌─────────┐
            │ queued  │
            └────┬────┘
                 │ Hub picks broker, sends run_workflow command
                 ▼
         ┌──────────────┐
         │ provisioning │
         └──────┬───────┘
                │ Broker acks, ephemeral container created
                ▼
            ┌─────────┐
            │ running │
            └────┬────┘
                 │
         ┌───────┼─────────────────┬───────────────┐
         ▼       ▼                 ▼               ▼
   ┌──────────┐ ┌────────┐  ┌────────────┐  ┌────────────┐
   │succeeded │ │ failed │  │  canceled  │  │ timed_out  │
   │ (exit 0) │ │(exit 1 │  │(user/agent │  │(run > max) │
   │          │ │ or 2)  │  │ requested) │  │            │
   └──────────┘ └────────┘  └────────────┘  └────────────┘
```

#### Transitions

| From           | To              | Trigger                                               |
|----------------|-----------------|-------------------------------------------------------|
| `queued`       | `provisioning`  | Hub dispatcher sends `run_workflow` command to broker |
| `queued`       | `canceled`      | `POST /runs/:id/cancel` before dispatch               |
| `provisioning` | `running`       | Broker acks container up, `quack` process spawned     |
| `provisioning` | `failed`        | Container creation failed; broker reports error       |
| `running`      | `succeeded`     | `quack` exits 0                                       |
| `running`      | `failed`        | `quack` exits 1 (usage) or 2 (workflow failure)       |
| `running`      | `canceled`      | `POST /runs/:id/cancel`; broker sends SIGTERM to container |
| `running`      | `timed_out`     | Hub-side deadline exceeded; broker asked to kill run  |
| any terminal   | (terminal)      | no-op, idempotent                                     |

**Who triggers transitions:**
- `queued → provisioning`: Hub's `WorkflowRunDispatcher`, immediately after `POST /runs` commits the row (unless queueing is enabled for capacity reasons).
- `provisioning → running`: Broker emits `workflow_status` event over the Control Channel when the container starts and `quack` is exec'd.
- `running → succeeded | failed`: Broker on process exit.
- `running → canceled`: Hub receives cancel request, sends `cancel_workflow` command to broker. Broker terminates the container and reports back.
- `running → timed_out`: Hub applies a per-run deadline (default 1 hour, configurable per run). If still in `running` when deadline passes, Hub sends `cancel_workflow` with reason `timeout`, and the resulting terminal state is recorded as `timed_out` rather than `canceled`.

All terminal transitions set `finished_at`. `started_at` is set on `provisioning → running`. Idempotency is enforced by a CAS-style update keyed on the prior status.

### 3. API Surface (Hub)

All endpoints live under `/api/v1/workflows/...`, mirroring the grove/agent/broker conventions in `hub-api.md` (versioned path, bearer token, JSON bodies, consistent error envelope).

#### 3.1 Create Run

```
POST /api/v1/workflows/runs
```

**Request Body:**
```json
{
  "groveId": "string",
  "sourceYaml": "string",
  "inputs": {},
  "timeoutSeconds": 3600,
  "brokerId": "string"
}
```

- `groveId` (required): grove the run belongs to. The dispatcher selects a broker from this grove's providers.
- `sourceYaml` (required, v1): the workflow YAML, inline. Max size enforced by handler (default 256 KB).
- `inputs` (optional): arbitrary JSON envelope. Serialized as-is to `inputs_json` and mounted into the container as `/workflow/inputs.json`.
- `timeoutSeconds` (optional, default 3600, max 21600): Hub-side deadline. Independent of any `timeout` the workflow itself sets per step.
- `brokerId` (optional): pin to a specific broker. If omitted, the Hub selects an online provider for the grove.

**Response:** `201 Created`
```json
{
  "run": {
    "id": "run-abc123",
    "groveId": "grove-xyz",
    "brokerId": null,
    "status": "queued",
    "createdAt": "2026-04-17T10:00:00Z",
    "createdBy": {
      "userId": "user-456",
      "agentId": null
    }
  }
}
```

The `sourceYaml`, `inputs`, `resultJson`, and `error` fields are **not** returned in list/create responses to keep wire payloads small. They are fetched via `GET /runs/:id` with explicit query parameters (Section 3.3).

#### 3.2 List Runs

```
GET /api/v1/workflows/runs
```

**Query Parameters:**

| Parameter     | Type   | Description                                   |
|---------------|--------|-----------------------------------------------|
| `groveId`     | string | Filter by grove (required unless admin)       |
| `status`      | string | Filter by status (repeatable)                 |
| `brokerId`    | string | Filter by broker                              |
| `createdBy`   | string | Filter by creator user ID                     |
| `since`       | string | RFC3339, only runs created at or after        |
| `limit`       | int    | Max results (default 50, max 200)             |
| `cursor`      | string | Pagination cursor                             |

**Response:**
```json
{
  "runs": [
    {
      "id": "run-abc123",
      "groveId": "grove-xyz",
      "brokerId": "broker-01",
      "status": "succeeded",
      "exitCode": 0,
      "traceUrl": "https://.../traces/run-abc123/",
      "startedAt": "2026-04-17T10:00:05Z",
      "finishedAt": "2026-04-17T10:00:42Z",
      "createdAt": "2026-04-17T10:00:00Z"
    }
  ],
  "nextCursor": "...",
  "totalCount": 17
}
```

#### 3.3 Get Run

```
GET /api/v1/workflows/runs/{runId}
```

**Query Parameters:**

| Parameter     | Type | Description                                                   |
|---------------|------|---------------------------------------------------------------|
| `include`     | csv  | Expand heavy fields: `source`, `inputs`, `result`. Off by default. |

**Response (with `include=source,inputs,result`):**
```json
{
  "run": {
    "id": "run-abc123",
    "groveId": "grove-xyz",
    "brokerId": "broker-01",
    "status": "succeeded",
    "exitCode": 0,
    "source": "version: 0.7\nname: hello\n...",
    "inputs": { "name": "world" },
    "result": { "message": "hello world" },
    "error": null,
    "traceUrl": "https://.../traces/run-abc123/",
    "startedAt": "2026-04-17T10:00:05Z",
    "finishedAt": "2026-04-17T10:00:42Z",
    "createdAt": "2026-04-17T10:00:00Z",
    "createdBy": { "userId": "user-456", "agentId": null }
  }
}
```

#### 3.4 Stream Logs

```
WS /api/v1/workflows/runs/{runId}/logs
```

Upgrades to a WebSocket. Uses the same `ticket` / `token` authentication pattern as agent PTY (see `hub-api.md` Section 8.1). The Hub bridges the broker's stream frames for this run's stdout/stderr into the client WebSocket as line-delimited JSON events:

```json
{ "ts": "2026-04-17T10:00:05.100Z", "stream": "stdout", "line": "..." }
{ "ts": "2026-04-17T10:00:05.120Z", "stream": "stderr", "line": "..." }
```

When the run reaches a terminal state, the Hub sends a final control message and closes the socket:

```json
{ "ts": "2026-04-17T10:00:42.050Z", "event": "terminal", "status": "succeeded", "exitCode": 0 }
```

Clients that connect after the run has terminated receive a replay from the uploaded trace directory (best effort) followed by the terminal event. v1 ships live-tail for in-flight runs and a single-shot replay for completed runs; it does not attempt full WSS-based historical log browsing (use `traceUrl`).

#### 3.5 Cancel Run

```
POST /api/v1/workflows/runs/{runId}/cancel
```

**Request Body:**
```json
{ "reason": "string" }
```

**Response:** `202 Accepted`
```json
{
  "run": { "id": "run-abc123", "status": "canceled" }
}
```

Semantics: flips `queued` runs to `canceled` synchronously. For `provisioning` or `running` runs, the Hub sends `cancel_workflow` over the Control Channel and returns `202`; the final status is confirmed asynchronously via the event stream and the eventual row update. Idempotent: cancelling a terminal run is a no-op and returns the current state with `200 OK`.

#### 3.6 Error Codes

Standard Hub error envelope (see `hub-api.md` Section 9). Workflow-specific error codes:

| HTTP | Code                          | Meaning                                                  |
|------|-------------------------------|----------------------------------------------------------|
| 400  | `workflow_source_invalid`     | `sourceYaml` failed parse or basic shape validation      |
| 413  | `workflow_source_too_large`   | `sourceYaml` exceeds size cap                            |
| 404  | `workflow_run_not_found`      | Unknown run ID                                           |
| 409  | `workflow_run_terminal`       | Cancel requested on terminal run (returned as 200, not error; listed here for contrast) |
| 422  | `workflow_grove_no_provider`  | No online broker can serve the grove                     |
| 502  | `workflow_broker_error`       | Broker rejected or failed the dispatch                   |

### 4. Dispatcher and Broker Protocol

#### 4.1 Hub-Side Dispatcher

**New component:** `pkg/hub/workflow_dispatcher.go`

The dispatcher observes inserts into `workflow_runs` with `status=queued` and moves them to `provisioning` by sending a command to a broker. Implementation sketch:

```go
// WorkflowRunDispatcher is a thin analog of the existing AgentDispatcher.
type WorkflowRunDispatcher struct {
    store    store.Store
    channels ControlChannelRouter
    blob     BlobUploader
}

func (d *WorkflowRunDispatcher) Dispatch(ctx context.Context, runID string) error {
    run, err := d.store.GetWorkflowRun(ctx, runID)
    if err != nil { return err }

    brokerID, err := d.selectBroker(ctx, run)
    if err != nil { return err }

    if err := d.store.TransitionWorkflowRun(ctx, runID, "queued", "provisioning", brokerID); err != nil {
        return err
    }

    cmd := ControlChannelCommand{
        Type:    "command",
        Command: "run_workflow",
        ID:      uuid.New().String(),
        Payload: RunWorkflowPayload{
            RunID:       run.ID,
            GroveID:     run.GroveID,
            SourceYAML:  run.SourceYAML,
            InputsJSON:  run.InputsJSON,
            TimeoutSecs: run.TimeoutSeconds,
            TraceUpload: TraceUploadCoords{ /* signed URL or direct blob coords */ },
        },
    }
    return d.channels.Send(brokerID, cmd)
}
```

Broker selection in v1: pick any `online` provider of the grove with capacity. Configurable placement (by labels, by resource class) is deferred.

The dispatcher does **not** maintain in-memory state about in-flight runs. All state lives in `workflow_runs`. Hub restarts recover by scanning for runs in non-terminal states and reconciling:
- `queued`: re-dispatch.
- `provisioning`: query the broker's last-known state via an `inspect_workflow` control-channel query; if the broker no longer knows the run, mark `failed` with `error=hub_restart_lost_state`.
- `running`: same inspect query; if the run is still alive on the broker, observe its exit; if not, `failed`.

#### 4.2 New Control-Channel Commands

Added to the command enum in Section 11.3 of `hub-api.md`:

| Command            | Direction     | Description                                           |
|--------------------|---------------|-------------------------------------------------------|
| `run_workflow`     | Hub → Broker  | Create ephemeral container, run `quack`, stream out.  |
| `cancel_workflow`  | Hub → Broker  | SIGTERM the container for a given run.                |
| `inspect_workflow` | Hub → Broker  | Return current known state for a run ID (recovery).   |

Added to the event enum in Section 11.5:

| Event              | Direction     | Description                                           |
|--------------------|---------------|-------------------------------------------------------|
| `workflow_status`  | Broker → Hub  | Lifecycle state change for a run.                     |
| `workflow_output`  | Broker → Hub  | Terminal event: exit code, result, trace upload key.  |

`workflow_status` payload:
```json
{
  "runId": "run-abc123",
  "status": "running",
  "at": "2026-04-17T10:00:05.100Z"
}
```

`workflow_output` payload:
```json
{
  "runId": "run-abc123",
  "exitCode": 0,
  "resultJson": "{\"message\":\"hello world\"}",
  "error": null,
  "traceKey": "traces/run-abc123.tar.zst"
}
```

Log frames reuse the existing `stream` multiplexing (Section 11.6), with a stream ID allocated by the broker and advertised in the first `workflow_status` event for the run.

#### 4.3 Broker-Side Executor

**New component:** `pkg/runtimebroker/workflow_executor.go`

On receipt of `run_workflow`:

1. Resolve the agent image the grove is configured to use (same resolution path as the agent provisioner, but skipping template overlays — workflows use the grove's default image unchanged).
2. Call a new **thin-provisioning** helper in `pkg/runtime/`:
   ```go
   // RunEphemeral creates a short-lived container with no worktree mount,
   // no tmux, no harness. The caller specifies an exec command and receives
   // a handle to stream stdout/stderr and wait on exit.
   func (r *Runtime) RunEphemeral(ctx, EphemeralSpec) (EphemeralHandle, error)
   ```
   This is distinct from `pkg/agent/provision.go`, which builds a full agent with worktree, sciontool sidecar, and harness. The two share the runtime abstraction (`pkg/runtime/`) but diverge in what gets mounted and what gets exec'd.
3. Write `source.yaml`, `inputs.json` to a fresh tmpfs mount at `/workflow/` inside the container. `trace/` is an empty directory at the same mount.
4. Exec inside the container:
   ```
   quack run /workflow/source.yaml \
     --input-file /workflow/inputs.json \
     --trace-dir /workflow/trace/ \
     --trace-format json
   ```
5. Stream stdout and stderr over the control channel as stream frames, one stream per (runID, channel).
6. On process exit: collect stdout in full (the terminal result JSON), tarball `/workflow/trace/`, upload via the signed URL provided in the command payload, and emit `workflow_output`.
7. Delete the container.

**Reuse vs. new:**

| Concern                        | Reused from agent path                | New for workflows                              |
|--------------------------------|---------------------------------------|------------------------------------------------|
| Container runtime abstraction  | `pkg/runtime/` (docker/apple/k8s)     | —                                              |
| Image resolution               | Grove default image lookup            | —                                              |
| Control channel stream framing | `pkg/runtimebroker/channel.go`        | `run_workflow` / `cancel_workflow` handlers    |
| Auth injection                 | —                                     | None in v1 (workflow gets no secrets unless explicitly mounted via a future field) |
| Worktree / git                 | —                                     | **Skipped.** Workflow does not mount the repo. |
| tmux / harness / sciontool     | —                                     | **Skipped.** No interactive session.           |
| Blob upload                    | Signed-URL pattern from workspace sync | Trace directory upload                         |

The "thin provisioning" path is deliberately narrow: the workflow executor should be one file in `pkg/runtimebroker/` that reads the run command, exec's `quack`, streams, and posts the result. Anything more elaborate (env propagation, secret materialization) is added later when the use case shows up.

### 5. Scion &#x2194; quack Contract

The contract between Scion and `quack` is intentionally minimal and stable. This section specifies the exact interface the broker relies on.

**Invocation:**

```
quack run <workflow-file> [flags...]
```

**Flags used by Scion (from `runtime-js/packages/runner/src/run.ts` and `main.ts`):**

| Flag              | Meaning                                         | Scion usage                                                             |
|-------------------|-------------------------------------------------|-------------------------------------------------------------------------|
| `--input k=v`     | Override one input, repeatable                  | Not used by the broker (CLI-side sugar; broker always uses input-file). |
| `--input-file F`  | Read input envelope from JSON file              | Always used: `/workflow/inputs.json`.                                   |
| `--cwd DIR`       | Working directory for exec participants         | Set to `/workflow/`; workflows that need repo state get an explicit mount. |
| `--trace-dir DIR` | Emit structured traces to this directory        | Always used: `/workflow/trace/`.                                        |
| `--trace-format`  | `json` &#124; `txt` &#124; `sqlite`             | v1 uses `json`; Hub stores the directory tarball as-is.                 |
| `--event-backend` | `memory` &#124; `nats` &#124; `redis`           | v1 uses `memory`; hosted event backends are not wired through in v1.    |
| `--quiet`         | Suppress info logs on stderr                    | v1 does not set this; broker captures stderr verbatim.                  |
| `--verbose`       | Extra diagnostics                               | Set when the run was created with `"debug": true` (future field).        |

**Stdin:** the broker does not write to stdin. `quack run` accepts JSON on stdin as a lowest-precedence inputs source (see `run.ts` lines 84-102); Scion uses `--input-file` exclusively to keep the contract explicit.

**Stdout:** a single JSON document (the resolved workflow output) or a plain string, depending on the workflow's output type. The broker captures it verbatim and stores it in `result_json`. If the workflow produces no output, stdout is empty, and `result_json` is `null`.

**Stderr:** free-form log lines from duckflux. The broker streams them as `stream=stderr` frames and does not attempt to parse them.

**Exit codes (from `run.ts` return values):**

| Code | Meaning                                                                                       | Scion mapping        |
|------|-----------------------------------------------------------------------------------------------|----------------------|
| 0    | Workflow completed successfully.                                                              | status=`succeeded`   |
| 1    | CLI/usage error: missing file, unreadable input file, unknown event backend, etc.             | status=`failed`, error="cli_error: ..." |
| 2    | Workflow executed but ended with `success=false` (e.g., a step failed and retry was exhausted). | status=`failed`, error="workflow_failed" |

**Environment variables:** the broker does not propagate Scion-specific env into the `quack` process by default. A future `env` field on `WorkflowRun` can opt into explicit environment injection, subject to the same resolved-env scoping that agents use.

**Working directory:** `/workflow/`. The workflow's own `exec` steps can `cd` or use absolute paths as needed.

**Stability guarantee:** this surface is the integration point. If duckflux changes any of the flags, stdout format, or exit codes listed above in a way that is not additive, the Scion broker breaks. To defend against this, the default agent image pins an exact `QUACK_VERSION` at build time, and Scion's release notes document the supported range.

### 6. Authentication and Authorization

#### 6.1 User-Created Runs

Users creating runs via `scion workflow run --hub` or the web UI authenticate through the existing Hub OAuth stack (`auth-overview.md`). The standard middleware applies: bearer token or session cookie, user identity resolved to a user ID, role/visibility checks on the target grove.

A user can create a run in a grove if they have write access to that grove (same check as agent creation). The `createdBy.userId` is stamped onto the run.

#### 6.2 Agent-Initiated Runs (Phase 4 Stub)

Agents are authenticated with scoped JWTs (see `auth-overview.md` Table, row "Agent"). To allow an agent to create a workflow run, the JWT must carry a new claim:

```json
{
  "sub": "agent-789",
  "grove": "grove-xyz",
  "scope": ["agent:status", "workflow:run"]
}
```

**Minting site (decided):** extend the existing bootstrap-token exchange (`POST /api/v1/auth/agent-token-exchange`, `hub-api.md` Section 10.4). The `workflow:run` claim is added to `scope` at mint time based on an opt-in flag on the template or grove policy. Rationale: a new endpoint would be auth surface sprawl for what is essentially another scoped claim on the same token; reuse keeps the agent's JWT lifecycle uniform. **Default:** opt-in only. Templates and groves do not gain the claim unless explicitly configured. This avoids silent privilege expansion as the workflow feature rolls out.

**Scope model:** `workflow:run` grants the bearer permission to create workflow runs in the grove named by the JWT's `grove` claim. The grove must match the run's `groveId` at the `POST /runs` handler, or the request is rejected with `403 forbidden`. Agents cannot create runs in other groves, cannot cancel runs they did not create, and cannot read `source_yaml` for runs they did not create. Read access to the agent's own runs is allowed.

The `createdBy.agentId` is stamped onto the run when the request carries an agent JWT. Either `createdBy.userId` or `createdBy.agentId` is populated, never both, never neither.

#### 6.3 Broker Authorization

Brokers authorize to the Hub via HMAC as today (`auth/runtime-broker-auth.md`). `workflow_status` and `workflow_output` events carry the broker ID in the stream framing; the Hub verifies the broker owns the run before applying the update.

---

## Scheduling Integration (Forward-Looking, Phase 4)

The existing scheduler (`scion/.design/hosted/scheduler.md`) models two timer categories: recurring handlers (1-minute root ticker) and one-shot events (`time.AfterFunc` per event, persisted in `scheduled_events`). Today the event handlers cover `message` and `status_update`.

Phase 4 extends the one-shot and recurring handler registry with a new event kind: `workflow.run`. The `scheduled_events.payload` for this kind is:

```json
{
  "groveId": "string",
  "sourceYaml": "string",
  "inputs": {},
  "timeoutSeconds": 3600
}
```

On fire, the scheduler's `executeEvent` dispatches to a `handleWorkflowRunEvent` handler that performs the same work as `POST /api/v1/workflows/runs`: validates the grove, picks a broker, inserts a `WorkflowRun` row, and invokes the dispatcher. The principal for scheduled runs is the user who created the schedule (their ID is carried in the existing `created_by` column of `scheduled_events`).

Recurring workflow schedules (cron-like) depend on the `User-Submitted Cron Schedules` future work listed in `scheduler.md`. When that lands, the cron-evaluated handler invokes the same `handleWorkflowRunEvent` path, fanning each firing into a new `WorkflowRun`.

CLI surface (phase 4):

```
scion schedule create --workflow flow.duck.yaml --grove G --at "2026-05-01T09:00:00Z"
scion schedule create --workflow flow.duck.yaml --grove G --cron "0 9 * * *"
```

No changes to the `WorkflowRun` entity are required; schedule-originated runs are indistinguishable from user-originated runs except that `createdBy.userId` is the schedule's owner and the run carries an annotation linking back to the scheduling row (via labels / annotations when that field is added, or via a future `origin` field).

---

## MCP Tool for Agents (Forward-Looking, Phase 4)

Agents running inside Scion containers already have an MCP discovery mechanism: `sciontool` runs as a sidecar, exposes an MCP server on a Unix socket, and the harness (Claude Code, Gemini CLI) is configured at template-hydration time to connect to it. This is pre-existing Scion machinery and is not changed by this design.

Phase 4 adds two tools to sciontool's MCP server:

**Tool: `workflow_run`**

```json
{
  "name": "workflow_run",
  "description": "Run a duckflux workflow in the current grove and optionally wait for the result.",
  "inputSchema": {
    "type": "object",
    "required": ["source"],
    "properties": {
      "source":        { "type": "string", "description": "Workflow YAML (inline)." },
      "inputs":        { "type": "object", "description": "Input envelope." },
      "wait":          { "type": "boolean", "default": true },
      "timeoutSecs":   { "type": "integer", "default": 3600, "minimum": 10, "maximum": 21600 }
    }
  }
}
```

Implementation: sciontool calls `POST /api/v1/workflows/runs` with the agent JWT (which must carry `scope=["workflow:run"]`). If `wait=true`, sciontool then connects to the `/logs` WSS and blocks until the terminal event, returning the final result JSON. If `wait=false`, sciontool returns `{ runId }` immediately.

**Tool: `workflow_status`**

```json
{
  "name": "workflow_status",
  "inputSchema": {
    "type": "object",
    "required": ["runId"],
    "properties": { "runId": { "type": "string" } }
  }
}
```

Implementation: calls `GET /api/v1/workflows/runs/{runId}?include=result` and returns the response.

The harness discovers these tools automatically via MCP's standard `list_tools` handshake; no harness-side changes are required.

---

## Observability

**Traces.** `quack run` emits structured traces to `--trace-dir` as JSON files. The broker tars the directory after the run terminates and uploads it to blob storage using the same signed-URL pattern already used for workspace sync (`hosted-architecture.md` Section 4.2). The resulting blob key is stored as `trace_url` on the run. Users retrieve traces via:

```
GET /api/v1/workflows/runs/{runId}/trace
```

which 302-redirects to a short-lived signed URL for the trace bundle. The web UI (future) can unpack and render traces; v1 ships the raw bundle.

**Metrics.** The broker emits OTel metrics alongside existing agent metrics:

- `scion_workflow_runs_total{status, grove_id}` (counter)
- `scion_workflow_run_duration_seconds{status, grove_id}` (histogram, measured from `started_at` to `finished_at`)
- `scion_workflow_run_queue_wait_seconds{grove_id}` (histogram, measured from `created_at` to `started_at`)
- `scion_workflow_run_in_flight{broker_id}` (gauge)

These feed the existing OTel pipeline documented in `hosted-architecture.md` Section 6. No new collectors are required.

**Logs.** Live tail over the WSS endpoint (Section 3.4). Historical logs are served from the trace bundle.

**Audit.** Every `POST /runs` and `POST /runs/:id/cancel` emits an audit record (user ID or agent ID, grove ID, run ID, action, timestamp) via the existing audit pipeline.

---

## CLI Surface Summary

```
scion workflow run <file.duck.yaml>               # local subprocess to quack (Phase 1)
scion workflow run <file.duck.yaml> --hub         # dispatch via Hub (Phase 3)
scion workflow validate <file.duck.yaml>          # delegates to `quack validate`
scion workflow list [--grove G] [--status ...]    # list recent runs
scion workflow get <run-id> [--show-source]       # fetch run details
scion workflow logs <run-id> [-f]                 # stream logs
scion workflow cancel <run-id> [--reason "..."]
```

All commands share Scion's existing OAuth + grove-resolution plumbing. The `--hub` flag is the switch between local subprocess dispatch and Hub dispatch.

**Default policy (decided):** mirror the agent dispatch default. Hub dispatch when the resolved grove has a hub endpoint configured; local dispatch otherwise (no grove context, or a local-only grove). An explicit `--local` flag always forces local subprocess dispatch regardless of grove configuration, and `--hub` always forces Hub dispatch (failing with a clear error if no hub endpoint is resolvable). Rationale: consistency with the agent mental model ("workflows run under the same conditions as agents"); devs without a hub still get zero-config local runs.

---

## Non-Goals / Out of Scope for v1

Explicit list, some repeated from the intro for clarity:

1. **WorkflowDefinition registry.** No named, versioned, reusable workflow catalog. Inline `source_yaml` only.
2. **Workflow versioning.** Each run carries its own full source. No `definition_id` + version.
3. **Inter-run dependencies.** No DAGs of runs, no "run B after A." duckflux's own sub-workflow feature handles composition inside a workflow.
4. **Hub-level retry.** duckflux SPEC v0.7 provides `retry` at the step level; Scion does not wrap runs in additional retry logic.
5. **Scheduled workflows.** Phase 4.
6. **MCP tool.** Phase 4.
7. **Live per-step progress events.** Phase-4b candidate, gated on a future `--progress-events` flag on `quack`. v1 ships stdout/stderr tailing and post-mortem trace access only.
8. **Repo-aware workflows.** No automatic mounting of the grove's worktree into the ephemeral container. If a workflow needs repo state, it must either fetch it via its own `exec`/`http` steps or wait for a future `workspace: true` field on the run spec.
9. **Per-run secret injection.** No `secrets: [...]` field on `WorkflowRun`. All workflow inputs are non-secret values in `inputs_json`. Secret handling is deferred and will reuse the existing grove/broker secret-scope model when it lands.
10. **Local-broker-only groves.** If a grove has zero online providers, `POST /runs` returns `422 workflow_grove_no_provider`. No hub-side queue that waits for a broker to come online.

---

## Risks and Open Questions

### R1. ent migration coordination

Adding `workflow_runs` requires a schema migration. Several other ent-touching designs are in flight (agent state refactor, template decoupling, secret ID refactor). The migration must be sequenced against those to avoid conflicting incremental migrations. **Mitigation:** coordinate with the in-flight PRs before merging; run `go generate ./pkg/hub/ent/...` on top of the latest main and regenerate if there's a gap. **Open:** is there a migration-ordering convention in Scion's repo, or is it first-merge-wins?

### R2. Ephemeral container lifecycle vs. agent cleanup paths

The broker's existing cleanup logic assumes agents with worktrees, tmux, and sciontool. Workflow containers have none of these. If a broker crashes mid-run, the existing reaper must not try to tmux-attach or worktree-teardown the corpse.

**Decision:** separate reaper path. Workflow containers are owned by a dedicated `WorkflowContainerReaper` that reacts to terminal `WorkflowRun` states (`succeeded`, `failed`, `canceled`, `timed_out`) and removes the container immediately. The agent reaper path explicitly ignores containers labeled `scion.scion/kind=workflow-run`. Rationale: agent cleanup was designed for a semi-manual lifecycle (attach, idle timeout, `scion delete`). Sharing infra with an auto-on-terminal lifecycle is a known source of bugs (orphaned containers waiting for an attach that never comes; reaper racing with an interactive session that does not exist). The `pkg/runtime` abstractions (spawn, kill, inspect) are still reused; only the lifecycle policy diverges.

### R3. Step-event streaming granularity

Today `quack run` emits its final result on stdout and writes structured per-step events only to `--trace-dir`. Clients that want live progress (per step, not per stdout line) have no wire to listen on. **Mitigation:** v1 promises stdout tailing only; the design calls out `--progress-events` as a future quack flag emitting NDJSON on a dedicated fd (e.g., fd 3). Broker would capture and forward as its own `workflow_progress` stream. **Open:** do we need this before v1 ships, or can UX get by with post-run trace rendering?

### R4. quack version drift

The contract in Section 5 is stable by convention but not enforced. A breaking change in duckflux (say, stdout format shifts from "final output" to "full `WorkflowResult` envelope") would silently break Scion. **Mitigation:** pin `QUACK_VERSION` in the default agent image via a Dockerfile ARG. Scion's release notes document the supported range. CI smoke-tests the broker path against the pinned version. **Open:** should Scion have a compatibility test in its own CI that exercises quack directly (not via container) to catch drift before image rebuild?

### R5. Trace upload failures

If blob storage is unreachable when the run terminates, the trace cannot be uploaded. **Mitigation:** retry with exponential backoff inside the broker; if the retry window is exhausted, mark the run terminal with `trace_url=null` and surface a Hub-side warning. The run is still `succeeded`/`failed` based on quack's exit code, independent of trace upload. **Open:** do we want a separate `trace_upload_status` enum on the row, or is a null `trace_url` plus an `annotations.trace_upload_error` sufficient?

### R6. Result size inflation

A workflow with a `loop` over a large dataset could easily produce a multi-MB stdout blob. Storing it inline in `result_json` blows up the row. **Mitigation:** enforce a hard cap (default 256 KB), and above the cap, spill the result to blob alongside the trace. The `result_json` column then stores `{ "$ref": "blob://..." }`. The Hub resolves `$ref` transparently on `GET /runs/:id?include=result`. **Open:** do we want the cap configurable per grove, or global?

### R7. Cancellation race with terminal events

A user clicks cancel at the same instant `quack` exits successfully. The broker sees both. The CAS-style state transitions (Section 2) make this safe: whichever update arrives first wins; the loser is dropped. **Open:** should the API return a richer response on `POST /cancel` for a run that raced to success, telling the client which side won?

### R8. Workflow sources with secrets

A user might paste a workflow that contains an inlined API key inside a `headers` block. We store `source_yaml` verbatim; anyone with grove read access can retrieve it. **Mitigation for v1:** document this clearly in the CLI help. Encourage `{{ inputs.token }}` references. Phase 5+: encrypt `source_yaml` at rest, redact on `GET ?include=source` unless the caller is the creator. **Open:** acceptable posture for v1?

### R9. WorkflowRun authorization visibility

Should grove viewers (`visibility=team`) see workflow runs they did not create? Runs are more ephemeral than agents and a team dashboard listing every run is noisier.

**Decision for v1:** match agent visibility semantics. All members of a grove see all runs in that grove (`list`, `get`, `logs`) with full content. Rationale: consistency with the existing mental model; simpler authz surface. Secrets are the concern, not visibility: workflow authors must pass sensitive values via `{{ inputs.token }}` referencing grove-scoped secrets (existing Scion pattern). An inlined API key in `source_yaml` is a user error, covered by R5 and CLI help text. Per-run visibility, redaction, and creator-only fields are refinements that can land in a later phase without schema churn (the visibility field, if introduced, would be additive on `WorkflowRun`).

### R10. Broker selection for multi-broker groves

A grove with three online brokers gets a run. V1 picks "any `online` provider." Without capacity feedback, the same broker can be picked repeatedly. **Mitigation:** round-robin with a simple per-broker in-flight counter, maintained in-memory on the Hub. **Open:** does this survive multi-Hub deployment, or do we wait on the scheduler design's leader-election future work?

---

## Related Documents

- [Scheduler](hosted/scheduler.md) — The last first-class Hub entity added; this design mirrors its structure, API conventions, and lifecycle approach.
- [Hosted Architecture Overview](hosted/hosted-architecture.md) — Overall hub/broker/sciontool model that workflows plug into.
- [Hub API](hosted/hub-api.md) — REST conventions, control channel protocol, WebSocket auth; workflow endpoints extend Sections 3–11.
- [Agent Auth Refactor](agent-auth-refactor.md) — Context for the agent JWT shape that will carry `workflow:run` in Phase 4.
- [Auth Overview](hosted/auth/auth-overview.md) — Authentication contexts (OAuth user, HMAC broker, JWT agent) reused as-is.
- [Runtime Broker Auth](hosted/auth/runtime-broker-auth.md) — HMAC model that the workflow control-channel commands inherit.
- duckflux `spec/SPEC.md` v0.7 — Immutable semantic contract for what a workflow is and how it executes. **Not modified by this design.**
- duckflux `runtime-js/packages/runner/src/main.ts` and `run.ts` — Source of the quack CLI contract documented in Section 5.
- Integration plan: `eu-quero-fazer-uma-frolicking-metcalfe.md` — Phase roadmap this design enables.
