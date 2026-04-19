# Workflows MCP: Structured Tool Surface for Agent-Initiated Runs

## Status
**Proposal** | April 2026

## Problem

The Phase 4b agent-invocation path for workflows ships with a single entry point: the `scion workflow run --via-hub` CLI, invoked from inside the agent container. Since Phase 2b bakes `sciontool` (and, transitively, the `scion` binary entry points for grove-scoped calls) into the default agent image, and Phase 3b+4b gave agents the `workflow:run` JWT scope, this works today. An agent that decides "I need to run a duckflux workflow" can compose one as YAML, write it to a temp file, and shell out through its Bash/Shell tool.

This works, and ships. It does not feel native.

Modern agent harnesses (Claude Code, Gemini CLI, Cursor, Windsurf) are converging on MCP (Model Context Protocol) as the structured interface for tool use. A tool registered via MCP gives the harness:

1. A typed, named tool with a JSON Schema for inputs. The LLM sees "this tool takes `source_yaml` (string, required) and `inputs` (object, optional)", not "this tool is the Bash shell, figure out the right invocation."
2. A typed output. The harness can present `run_id`, `status`, `trace_url` as structured fields, not a blob of stdout the LLM has to re-parse.
3. First-class observability and per-tool authorization (some harnesses gate tool use on user approval prompts, distinct from generic shell).

The equivalent call through the CLI:

```
# shell-tool invocation the harness has to author
cat > /tmp/flow.yaml <<'YAML'
version: 0.7
name: nightly-report
steps:
  - name: fetch
    http:
      url: https://api.example.com/reports/latest
      method: GET
YAML
scion workflow run --via-hub /tmp/flow.yaml \
  --input '{"date":"2026-04-17"}' \
  --format json
```

vs. the MCP-tool invocation:

```jsonc
{
  "tool": "workflow.run",
  "arguments": {
    "source_yaml": "version: 0.7\nname: nightly-report\nsteps:\n  - name: fetch\n    http: { url: 'https://api.example.com/reports/latest', method: GET }\n",
    "inputs": { "date": "2026-04-17" }
  }
}
```

The LLM has to emit less, the failure modes are more constrained, and the output is structured instead of text that has to be re-parsed.

This proposal is explicitly **incremental value**, not a replacement. The CLI path is the general-purpose, runs-anywhere mechanism. Not every harness speaks MCP, and when authors want to wire workflows into a custom shell script, `scion workflow run` is still the right answer. The MCP path is a complementary surface for harnesses that do speak MCP, where the ergonomic gain is real but not life-or-death.

The MCP path was originally planned alongside the CLI path in Phase 4b and was dropped because:

1. Scion has no MCP server infrastructure today. Building one is a substantial side-quest.
2. Claude Code and Gemini CLI already have a shell tool. The "value delta" between "invoke via Bash" and "invoke via a typed MCP tool" is ergonomics, not capability.
3. The CLI path covers 100% of the use cases the MCP path would cover, plus more (non-MCP harnesses, CI, scripted workflows, humans).

This document records what the MCP path would look like so a later PR can pick it up without re-deriving the design. It assumes the Phase 4b CLI path has shipped and is working.

---

## Goals

1. **Expose `workflow.run` and `workflow.status` as MCP tools.** Two tools, tightly scoped. Any harness that speaks MCP can discover and call them.
2. **Stdio-based MCP server embedded in `sciontool`.** No separate process, no separate binary, no new container to manage. The harness spawns `sciontool mcp-server` as a child over stdio and talks JSON-RPC 2.0 to it.
3. **Inherit agent JWT auth from Phase 4b.** The MCP server is not a new authentication context. It reads the same `SCION_HUB_TOKEN` the `scion workflow run --via-hub` CLI reads. The `workflow:run` scope governs both paths identically.
4. **No-regret design.** If MCP the protocol evolves incompatibly, or if the ecosystem consolidates on something else, or if we simply decide the added code is not worth the maintenance cost, the CLI path is untouched and agents keep running workflows exactly as they do in Phase 4b. The MCP server is additive; dropping it is a file-delete plus a harness-config reversal.

## Non-goals

- **MCP resources and prompts.** MCP defines three primitive types: tools, resources, and prompts. v1 of the Scion workflows MCP server implements only tools. Exposing workflow traces as MCP resources (streamable, subscribe-able) is a tempting extension and an explicit non-goal here; see Open Questions.
- **Standalone MCP binary.** No `scion-workflows-mcp` binary. The server lives under `sciontool mcp-server`. Discovery and packaging stay unified.
- **Proxying arbitrary Hub API endpoints as tools.** The MCP server is a workflow-scoped surface, not a generic Hub-API wrapper. If an agent needs to list other agents, read groves, or mutate templates, it uses the CLI or the Hub API directly with its JWT. Scope creep here leads to a "god tool" that ends up being just Bash in JSON clothing.
- **Full coverage of every workflow operation.** v1 covers `workflow.run` and `workflow.status`. `workflow.cancel`, `workflow.logs` (streaming), and any future `workflow.list` are deferred. Partly because the exact shape of each is still under discussion (is cancel an agent action at all?), partly because each new tool is an incremental decision the implementer can make once the core is landing.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                      Scion Agent Container                           │
│                                                                      │
│   ┌────────────────────────────────────────────────────────────┐     │
│   │ Harness process (Claude Code / Gemini CLI / ...)           │     │
│   │                                                            │     │
│   │   MCP client (built into the harness)                      │     │
│   │       │                                                    │     │
│   │       │  stdio (JSON-RPC 2.0)                              │     │
│   │       ▼                                                    │     │
│   └───┬────────────────────────────────────────────────────────┘     │
│       │                                                              │
│       │  spawn: sciontool mcp-server                                 │
│       │                                                              │
│   ┌───▼────────────────────────────────────────────────────────┐     │
│   │ sciontool mcp-server  (pkg/sciontool/mcp/)                 │     │
│   │                                                            │     │
│   │   - initialize / tools/list / tools/call / shutdown        │     │
│   │   - reads SCION_HUB_TOKEN (agent JWT, Phase 4b)            │     │
│   │   - reads SCION_HUB_ENDPOINT                               │     │
│   │                                                            │     │
│   │   On tools/call "workflow.run":                            │     │
│   │     → POST /api/v1/workflows/runs  (sciontool hub client)  │     │
│   │     → if wait=true: poll or stream until terminal          │     │
│   │                                                            │     │
│   │   On tools/call "workflow.status":                         │     │
│   │     → GET /api/v1/workflows/runs/{runId}?include=result    │     │
│   └───┬────────────────────────────────────────────────────────┘     │
│       │                                                              │
│       │  HTTPS (agent JWT)                                           │
│       ▼                                                              │
│  ─── container boundary ────────────────────────────────────────────│
│       │                                                              │
└───────┼──────────────────────────────────────────────────────────────┘
        │
        ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Scion Hub                                                           │
│                                                                      │
│  /api/v1/workflows/runs   (existing endpoints from workflows.md §3)  │
│                                                                      │
│  → WorkflowRunDispatcher → broker → ephemeral quack container        │
└──────────────────────────────────────────────────────────────────────┘
```

How the MCP path coexists with the CLI path:

```
CLI path (Phase 4b, ships today):
  harness → Bash tool → scion workflow run --via-hub → hubclient → Hub

MCP path (this proposal, future PR):
  harness → MCP client → sciontool mcp-server (stdio) → hubclient → Hub
```

Both converge on `pkg/hubclient` and hit the same `POST /api/v1/workflows/runs`. There is no second code path in the Hub, and there is no second code path in the broker. The only new surface is between the harness and sciontool, and it is pure JSON-RPC transport plus argument marshaling.

---

## Tool Specifications

Both tools are exposed via the standard MCP `tools/list` response. The schemas are copy-paste-able; the implementer can feed them directly into whichever MCP SDK the project ends up adopting.

### `workflow.run`

```json
{
  "name": "workflow.run",
  "description": "Run a duckflux workflow in the current grove. Returns the run ID immediately, or waits for completion if wait=true.",
  "inputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["source_yaml"],
    "properties": {
      "source_yaml": {
        "type": "string",
        "minLength": 1,
        "maxLength": 262144,
        "description": "The workflow YAML source. Inline only in v1; max 256 KB."
      },
      "inputs": {
        "type": "object",
        "description": "Input envelope passed to the workflow as /workflow/inputs.json.",
        "additionalProperties": true,
        "default": {}
      },
      "grove_id": {
        "type": "string",
        "description": "Optional. Grove to run in. Must equal the agent's home grove; server rejects mismatches. Defaults to the agent's home grove when omitted."
      },
      "wait": {
        "type": "boolean",
        "default": false,
        "description": "If true, the tool blocks until the run reaches a terminal state (succeeded, failed, canceled, timed_out) before returning. If false, returns immediately with run_id and status='queued' or 'provisioning'."
      },
      "timeout_seconds": {
        "type": "integer",
        "minimum": 10,
        "maximum": 21600,
        "default": 3600,
        "description": "Hub-side deadline for the run, independent of any step-level timeout the workflow sets."
      }
    }
  },
  "outputSchema": {
    "type": "object",
    "required": ["run_id", "status"],
    "properties": {
      "run_id":    { "type": "string" },
      "status":    { "type": "string", "enum": ["queued", "provisioning", "running", "succeeded", "failed", "canceled", "timed_out"] },
      "result":    { "description": "Present when wait=true and status=succeeded. The resolved workflow output." },
      "error":     { "type": "string", "description": "Present when status is failed or timed_out." },
      "exit_code": { "type": "integer", "description": "quack exit code, present when wait=true and the run terminated." },
      "trace_url": { "type": "string", "description": "Signed URL to the trace bundle. Present when wait=true and the run terminated." }
    }
  }
}
```

Behavior notes:

- If `wait=false` (default), the implementation calls `POST /api/v1/workflows/runs` and returns the `{ run_id, status }` from the Hub's response.
- If `wait=true`, the implementation returns the same response only after the run reaches a terminal state. See Protocol Considerations for the timeout handling that makes this safe in practice.
- If `grove_id` is omitted, the server uses the `grove` claim in the agent JWT. If provided and different, the server returns `403`; the tool surfaces this as an `error` field rather than throwing out of the MCP layer.

### `workflow.status`

```json
{
  "name": "workflow.status",
  "description": "Fetch the current state of a workflow run, including result when terminal.",
  "inputSchema": {
    "type": "object",
    "additionalProperties": false,
    "required": ["run_id"],
    "properties": {
      "run_id": {
        "type": "string",
        "minLength": 1,
        "description": "The run ID returned by a prior workflow.run call."
      }
    }
  },
  "outputSchema": {
    "type": "object",
    "required": ["run_id", "status"],
    "properties": {
      "run_id":      { "type": "string" },
      "grove_id":    { "type": "string" },
      "broker_id":   { "type": ["string", "null"] },
      "status":      { "type": "string", "enum": ["queued", "provisioning", "running", "succeeded", "failed", "canceled", "timed_out"] },
      "exit_code":   { "type": ["integer", "null"] },
      "result":      {},
      "error":       { "type": ["string", "null"] },
      "trace_url":   { "type": ["string", "null"] },
      "started_at":  { "type": ["string", "null"], "format": "date-time" },
      "finished_at": { "type": ["string", "null"], "format": "date-time" },
      "created_at":  { "type": "string", "format": "date-time" },
      "created_by": {
        "type": "object",
        "properties": {
          "user_id":  { "type": ["string", "null"] },
          "agent_id": { "type": ["string", "null"] }
        }
      }
    }
  }
}
```

Behavior notes:

- Directly maps to `GET /api/v1/workflows/runs/{runId}?include=result` on the Hub.
- Authorization is delegated to the Hub: an agent that asks for a run it does not own gets a `404` (not a `403`, to avoid leaking existence). The tool passes that through as an `error` field.

Both tool names use dot-separated segments (`workflow.run`, `workflow.status`). Some MCP SDKs prefer underscores (`workflow_run`); the names above are the source of truth and the implementer can alias if a specific SDK requires it.

---

## Implementation Sketch

### Language and SDK

Go, to match Scion. The Go MCP ecosystem is still young; candidate SDKs as of April 2026 include:

- `github.com/anthropics/mcp-go` (if available as a first-party Go SDK at implementation time)
- `github.com/mark3labs/mcp-go` (community)
- Hand-rolled JSON-RPC 2.0 over stdio, using `encoding/json` plus a small frame reader

Any of these works. The protocol surface the server needs is narrow (initialize, tools/list, tools/call, shutdown), so "hand-rolled" is not a crazy option if the SDK picture is still unstable. The implementer should make this call during the PR. See Open Questions.

### Code layout

New package:

```
pkg/sciontool/mcp/
├── server.go        # stdio loop, JSON-RPC dispatch, lifecycle
├── tools.go         # tool definitions, argument validation, Hub-client invocation
├── tools_test.go    # argument-validation tests, schema-conformance tests
└── server_test.go   # handshake, tools/list, tools/call golden tests
```

### Wire-up

A new top-level `sciontool` subcommand:

```
sciontool mcp-server [--hub-endpoint URL] [--agent-id ID]
```

Defaults read from environment the same way the Phase 4b workflow CLI does:

- `SCION_HUB_ENDPOINT` → `--hub-endpoint`
- `SCION_HUB_TOKEN`    → agent JWT for all Hub calls
- `SCION_AGENT_ID`     → `--agent-id` (used for logging / tracing; actual identity comes from the JWT)

The command reads newline-delimited JSON-RPC 2.0 frames on stdin, writes responses on stdout, and logs diagnostics on stderr (which the harness typically captures separately). The command runs until stdin closes or an explicit `shutdown` request arrives.

### Hub-client reuse

The server uses `pkg/hubclient` (or whichever package the Phase 4b CLI ended up centralizing on) for:

- `POST /api/v1/workflows/runs`
- `GET /api/v1/workflows/runs/{runId}?include=result`

No new client methods are required; the CLI and the MCP server share the same wrapper, which is the right arrangement. If the CLI path is currently using a narrower helper specific to `cmd/scion/workflow/`, this PR is a reasonable moment to promote it to `pkg/hubclient/workflows.go` so both callers share it.

### Harness registration

Claude Code's existing `mcpServers` wiring (`pkg/harness/claude_code.go:217`) is where Scion injects MCP server entries into the harness config at template hydration. This proposal adds a conditional entry:

```go
// Conceptually, at the point where mcpServers is populated:
if agentConfig.HasWorkflowRunScope() {
    projectSettings["mcpServers"].(map[string]interface{})["scion-workflows"] = map[string]interface{}{
        "command": "sciontool",
        "args":    []string{"mcp-server"},
    }
}
```

`HasWorkflowRunScope()` is a property of the agent config (or the grove's resolved agent-default scopes) that mirrors the `workflow:run` JWT claim. No new source of truth: both are driven by the same opt-in flag on the template or grove policy that governs `workflow:run` itself.

Gemini CLI and other harnesses gain MCP wiring through whichever equivalent injection point they use when (or if) Scion adds first-class MCP support for them. The server is harness-agnostic; only the config injection is per-harness.

### Auth path

The MCP server is authenticated-by-proxy: it does not implement its own auth. Every tool call resolves to a Hub API call carrying the `Authorization: Bearer ${SCION_HUB_TOKEN}` header. The Hub enforces the `workflow:run` scope. No new auth code, no new tokens, no new minting site.

If the agent JWT is expired, the Hub returns `401`, and the MCP server surfaces it as a tool-call error. If the token is rotated mid-session (future sciontool feature), the rotation is transparent: the server re-reads the token from the environment (or from a shared file, depending on how rotation lands) on each call.

---

## Protocol Considerations

MCP is JSON-RPC 2.0 over a transport (stdio for this server). The relevant lifecycle:

1. **`initialize`**: client sends capabilities, server responds with its own. The Scion server advertises only `tools` (no `resources`, no `prompts`).
2. **`tools/list`**: server returns the two tool definitions above.
3. **`tools/call`**: client invokes a tool with arguments. Server validates against `inputSchema`, performs the Hub call, returns the structured result.
4. **`shutdown` / `exit`**: server flushes, closes the Hub client, exits.

### Timeout semantics

`wait=true` is the interesting case. `workflow.status` is always a single cheap GET. `workflow.run` with `wait=false` is also a single cheap POST. But `workflow.run` with `wait=true` can block for the full workflow duration, which (per `.design/workflows.md` §3.1) can be up to `timeout_seconds` (default 3600, max 21600 = six hours).

MCP clients have their own per-tool timeouts. Claude Code's default is on the order of minutes, not hours. A naïve implementation of `wait=true` would simply block until the workflow finishes, and the harness would time out its own tool call long before then. The agent sees a "tool timeout" and has no idea the workflow is still running.

Recommended behavior:

1. **Fast path**: if the run reaches a terminal state within a short window (proposed default 60 seconds, configurable), return the terminal result synchronously.
2. **Slow path**: if the run is still in `queued`, `provisioning`, or `running` at the window expiry, return whatever the current status is with a note in the result indicating the run is still in flight. The `run_id` is always returned; the agent can call `workflow.status` to resume polling.
3. **Optionally**: include a `next_poll_after` hint (seconds) so well-behaved agents can pace their follow-up `workflow.status` calls without burning context.

The window itself can be governed by a `wait_max_seconds` argument on `workflow.run`, defaulting to 60. The exact default should be calibrated against what the dominant harnesses tolerate; anything under 30s is probably safe everywhere, anything over 120s starts to bump into some clients' defaults.

This is a CLI-style polling pattern dressed up in an MCP-style tool. It is unlovely, but it matches what the ecosystem actually supports. Long-running tools over MCP are an open question at the protocol level and are expected to become cleaner as MCP evolves streaming semantics.

---

## Security

1. **Grove pinning.** The server rejects any `grove_id` argument that does not equal the agent JWT's `grove` claim. The check runs server-side (on the Hub) as well; both layers enforce this, so tampering with the MCP server alone is not an escalation.
2. **Source YAML size cap.** 256 KB per Phase 3b. The MCP server validates this client-side in addition to the Hub's own check; this gives the harness a clean error without a Hub round-trip for the trivial case.
3. **No privilege above the JWT.** The server has no secrets of its own. If the agent JWT lacks the `workflow:run` scope, the Hub rejects the run; the MCP server adds no capability that the CLI does not already expose.
4. **Revocation.** If the grove is revoked or the JWT expires mid-session, the Hub returns `401`/`403`, and the MCP server surfaces a structured error. There is no local state to invalidate.
5. **No YAML parsing on the server.** The MCP server passes `source_yaml` through to the Hub unparsed. This keeps the attack surface (YAML parsers are historically a security-sensitive area) entirely on the Hub side, where it is already exposed via the CLI path. No new parser, no new surface.
6. **No argument persistence.** The server is stateless between calls. It does not cache run IDs, YAML sources, or inputs. Logs include the run ID and outcome but not the full YAML.

---

## Observability

1. **Audit events.** Every successful `tools/call` results in an `audit_events` row written by the Hub. The existing Phase 4b audit path already records `agent_invoked=true` for agent-originated runs; the MCP path inherits this with no change (the Hub sees a normal `POST /workflows/runs` with an agent JWT, indistinguishable from the CLI path). If the implementer wants to distinguish MCP from CLI origin in telemetry, the cheapest option is a `User-Agent: sciontool-mcp/<version>` header on the Hub client used by the MCP server, and a corresponding column or tag on the audit event.
2. **OTel spans.** The sciontool OTel plumbing (`pkg/sciontool/telemetry/`) already propagates traces for Hub calls. The MCP server wraps each `tools/call` in a span whose name is the tool name, attaching `tool.name`, `run.id`, and outcome as attributes. Span parents are the incoming MCP request ID; children are the Hub-client HTTP calls.
3. **stderr logs.** The server logs (at info level) every `initialize`, `tools/list`, and `tools/call` invocation, and (at error level) every failed validation or Hub error. The harness's process-capture typically routes stderr to its own log sink, so this is visible to operators without extra wiring.
4. **Metrics.** If the project has `sciontool`-local OTel metrics, relevant additions are a `sciontool_mcp_tool_calls_total{tool, outcome}` counter and a `sciontool_mcp_tool_call_duration_seconds{tool}` histogram. Not strictly required for v1; the Hub-side workflow metrics already cover the upstream picture.

---

## Testing Approach

1. **Unit tests (`tools_test.go`).**
   - Input validation: each invalid `workflow.run` and `workflow.status` input produces a JSON-RPC error with a clear message. Cover missing required fields, oversized YAML, negative/out-of-range `timeout_seconds`, wrong types.
   - JSON Schema conformance: serialize the declared tool schemas and validate a handful of good and bad fixtures against them, guarding against accidental schema drift.
   - Argument-to-request mapping: given a valid `workflow.run` call, confirm the resulting `POST /workflows/runs` body matches expectations.

2. **Integration tests (`server_test.go`).**
   - Fake MCP client driving the server over a pipe-pair, exercising the full `initialize → tools/list → tools/call → shutdown` handshake.
   - Fake Hub (`httptest.Server`) on the other side, verifying the Hub calls the server issues.
   - `wait=false` path returns immediately with `queued`.
   - `wait=true` path returns a terminal result when the fake Hub transitions quickly, and returns the in-flight status when the fake Hub stalls past `wait_max_seconds`.
   - Auth propagation: the `Authorization` header on Hub requests matches `SCION_HUB_TOKEN`.

3. **Manual smoke test.** Provision a Scion agent with `workflow:run` scope and the MCP server registered. Inside the container, observe Claude Code discovering `workflow.run` via `tools/list` and issuing a call that reaches a test Hub. Repeat with Gemini CLI when its MCP wiring lands.

4. **Backward compatibility check.** The CLI path must still work with the MCP server registered. A focused regression test runs `scion workflow run --via-hub` from inside a container that has the MCP server configured; the run behaves identically to an environment without the MCP server.

---

## Migration and Rollout

1. **Feature flag in the template.** A new boolean on agent templates (working name `workflow_mcp_tools: enabled`, final name to be decided alongside the template team) gates MCP server registration. Defaults to `false`.
2. **Enablement order.** After the PR merges, the flag is enabled first for internal Scion templates (dogfood). Once at least one harness has been verified end-to-end with a real workflow, the flag is documented in template-author guides. Per-grove enablement via template update.
3. **Scope coupling.** `workflow_mcp_tools` should imply `workflow:run` in the minted JWT (enabling the MCP tool without the scope is useless). Whether this is a hard coupling ("enabling one auto-enables the other") or a validation error ("refuse to mint a template with MCP but no `workflow:run`") is a template-config detail; either is acceptable.
4. **Rollback.** Disabling the flag removes the MCP server from the next agent's hydration. Existing agents continue as they are until they recycle. The CLI path is unaffected; any agent falls back to `scion workflow run --via-hub` without code changes.
5. **Deprecation path (forward).** If MCP is dropped as a surface in the future, the rollback path above is the deprecation path. Remove the flag, leave the code stubbed for one release to ease upgrade of out-of-tree templates, then delete.

---

## Open Questions

1. **Which MCP SDK?** Options include a first-party Anthropic Go SDK (if/when published), `mark3labs/mcp-go`, and hand-rolled. The tradeoffs are maintenance burden, protocol-feature coverage (for future `resources`/`prompts` ambitions), license, and API stability. This is the first decision the implementer has to make; it should not be pre-committed in this proposal.

2. **`workflow.logs` as tool or MCP resource?** Log streaming is a natural fit for MCP's `resources` primitive, which supports subscription-style pull. But the current server exposes no resources; adding the machinery for one resource is a meaningful scope bump. Alternatively, `workflow.logs` can be a tool that returns the last N lines as a string, with pagination via a cursor. Neither is perfect. Pick one in a follow-up, not here.

3. **`workflow.cancel` as a tool?** Agents initiating runs and then cancelling their own runs is a narrow use case. In the dominant scenarios an agent needs to cancel a run, something has already gone wrong (hallucinated a YAML, triggered an infinite loop inside the workflow). In those scenarios a user intervening via the CLI or web UI is probably more appropriate. Exposing cancel as an agent tool may encourage agents to "fix" problems by cancelling instead of surfacing them. Worth deciding before v1 ships.

4. **Long-running `wait=true` semantics beyond the 60s window.** The polling-fallback design above is the obvious path. MCP as a protocol may add streaming or long-running-tool conventions in later revisions; whether to lean into those (and when) is a followable-signal for future revisions of this proposal.

5. **Multiple MCP servers in one agent.** If Scion ends up with a second MCP server (say, `sciontool mcp-server-tools` for a separate surface), the harness's `mcpServers` map already supports multiple entries. No design decision needed here; calling it out so nobody reinvents a multiplexer.

6. **Result redaction.** Workflow results can contain data the agent should not see (secrets that leaked from a step's output). Today the CLI path also exposes this, so the MCP path is no worse. A future redaction layer at the Hub-response level would cover both paths; whether it lives in the Hub or the MCP-server-side is out of scope for v1.

7. **Versioning of the tool schemas.** If `workflow.run` grows a new optional input in v2, the schema change ripples to every harness that introspected it via `tools/list`. MCP clients generally accept additive changes. A breaking change (rename, required-field addition) would need a new tool name (`workflow.run.v2`) or a hard reset. The implementer should err on the side of additive evolution and document this policy in `server.go`.

---

## References

- [Workflows: First-Class Hub Entity for Deterministic Flows](workflows.md) — The main design doc. Sections "MCP Tool for Agents (Forward-Looking, Phase 4)" and the overall entity model are the direct context for this proposal.
- [Hub Scheduler: Timers and Recurring Events](hosted/scheduler.md) — Structural template this proposal mirrors.
- [Hub API](hosted/hub-api.md) — REST conventions for `POST /api/v1/workflows/runs` and `GET /api/v1/workflows/runs/{runId}`.
- [ScionTool Architecture](sciontool-overview.md) — Host for the new `mcp-server` subcommand, and source of the daemon / hook / telemetry packages this proposal reuses.
- [Agent Auth Refactor](agent-auth-refactor.md) and [Auth Overview](hosted/auth/auth-overview.md) — Agent JWT lifecycle and scope model; `workflow:run` is added at the same minting site (`POST /api/v1/auth/agent-token-exchange`) the MCP server implicitly consumes.
- [ScionTool Auth](hosted/auth/sciontool-auth.md) — How sciontool obtains and uses the agent JWT; the MCP server reuses this path unchanged.
- Model Context Protocol specification — The upstream protocol this server implements. The spec is published by Anthropic; the implementer should link the version-pinned URL current at PR time rather than relying on a guess here.
- duckflux `spec/SPEC.md` v0.7 — The immutable contract for what a workflow is. Not modified by this proposal.
