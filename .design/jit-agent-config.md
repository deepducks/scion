# Inline Agent Configuration: Late-Binding Config at Agent Creation Time

**Status:** Draft
**Created:** 2026-03-06
**Related:** [agent-config-flow.md](./agent-config-flow.md), [hosted-templates.md](./hosted/hosted-templates.md)

---

## 1. Overview

### Problem

Today, agent configuration is assembled from a multi-layered composition of templates, harness configs, settings profiles, and CLI flags. To customize an agent beyond the available CLI flags, users must:

1. Create a custom template directory with a `scion-agent.yaml`
2. Optionally create a custom harness-config directory
3. Reference these by name at agent creation time

This works well for reusable configurations but creates friction for one-off or exploratory use cases. Every new tunable that a user might want to set ad-hoc requires either a new CLI flag (leading to flag proliferation — e.g., `--enable-telemetry`, `--model`, `--max-turns`) or a new template.

The Hub web UI amplifies this problem: building a form that exposes all agent options requires either generating templates server-side or adding every field as a discrete API parameter.

### Goal

Allow agents to be started with an **inline configuration object** — a self-contained document that can express the full range of agent configuration without requiring pre-existing template or harness-config artifacts on disk.

Conceptually, this is "just-in-time" or "late-binding" configuration: the agent's settings are provided at creation time rather than being pre-staged as template artifacts. The implementation achieves this by evolving `ScionConfig` into a superset that absorbs fields currently scattered across harness-config entries, template content files, and CLI flags.

### Design Principles

1. **Additive** — Inline config is a new input path, not a replacement. Templates and harness configs continue to work as before.
2. **Superset via evolution** — `ScionConfig` itself is expanded to absorb fields that today live outside the config file (system prompt content, harness-config details). No new parallel config type is introduced.
3. **Explicit over composed** — When an inline config is provided, its values are authoritative. The multi-layer merge behavior is simplified: inline config is the "template equivalent," composed only with broker/runtime-level concerns.
4. **Backwards compatible** — Existing `scion start --type my-template` workflows are unaffected. Existing `scion-agent.yaml` files remain valid.

---

## 2. Current State

### Configuration Sources (Precedence, Low -> High)

```
Embedded defaults
  -> Global settings (~/.scion/settings.yaml)
    -> Grove settings (.scion/settings.yaml)
      -> Template chain (scion-agent.yaml, inherited)
        -> Harness-config (config.yaml + home/ files)
          -> Agent-persisted config (scion-agent.json)
            -> CLI flags (--image, --enable-telemetry, etc.)
```

### What Lives Where Today

| Concern | Where It's Defined | Format |
|---------|-------------------|--------|
| Harness type | Harness-config `config.yaml` | `harness: claude` |
| Container image | Harness-config `config.yaml` or template | `image: ...` |
| Environment vars | Template, harness-config, settings, CLI | `env: {K: V}` |
| System prompt | Template directory file (`system-prompt.md`) | Markdown file |
| Agent instructions | Template directory file (`agents.md`) | Markdown file |
| Model selection | Template or harness-config | `model: claude-opus-4-6` |
| Auth method | Harness-config or settings profile | `auth_selected_type: api-key` |
| Container user | Harness-config `config.yaml` | `user: scion` |
| Volumes | Template, harness-config, settings | `volumes: [...]` |
| Resources | Template or settings profile | `resources: {requests: ...}` |
| Telemetry | Settings, template, or CLI flag | `telemetry: {enabled: true}` |
| Services (sidecars) | Template | `services: [...]` |
| Max turns/duration | Template | `max_turns: 100` |
| Home directory files | Harness-config `home/` directory | Filesystem artifacts |

### Key Observation

The current `ScionConfig` struct already captures most of these concerns. The gaps are:

1. **System prompt and agent instructions** — stored as file references in `ScionConfig` (`system_prompt: system-prompt.md`) but the actual content lives as files alongside the template. The config references a filename, not inline content.
2. **Harness-config details** — the container user, task flag, default CLI args, and auth method come from `HarnessConfigEntry`, not from `ScionConfig`. Notably, the `harness` field itself lives on `HarnessConfigEntry` and directs to the appropriate harness implementation in `pkg/harness`.
3. **Home directory files** — harness-config `home/` directories provide files like `.claude.json`, `.bashrc`, etc. These are filesystem artifacts that can't be expressed inline. This is the trickiest gap to close, and we accept that templates and harness configs will remain the mechanism for home directory file provisioning. The goal is to capture as much of the common-denominator configuration as possible in the expanded `ScionConfig`.

### Existing Partial Precedent

The `AgentConfigOverride` struct in `pkg/hub/handlers.go` already provides a limited inline override:

```go
type AgentConfigOverride struct {
    Image    string            `json:"image,omitempty"`
    Env      map[string]string `json:"env,omitempty"`
    Detached *bool             `json:"detached,omitempty"`
    Model    string            `json:"model,omitempty"`
}
```

And `hubclient.AgentConfig` similarly has `Image`, `HarnessConfig`, `HarnessAuth`, `Env`, `Model`, `Task`. These are narrow override surfaces that exist because `ScionConfig` wasn't expressive enough to serve as the inline config document. By expanding `ScionConfig` to cover these cases, we can replace `AgentConfigOverride` and thread a full `ScionConfig` through more of the agent creation process — eliminating the need for ad-hoc override structs.

---

## 3. Proposed Design

### 3.1 Expanded ScionConfig Schema

Rather than introducing a new parallel type, we expand `ScionConfig` itself with the fields that today require separate artifacts. This keeps a single authoritative config type and ensures that `scion-agent.yaml` files in templates immediately gain inline content support.

New fields added to `ScionConfig`:

```go
// Added to the existing ScionConfig struct in pkg/api/types.go

// === Content fields (inline instead of file references) ===
// When set, these contain the actual content rather than a filename.
// The content resolution logic checks: if the value is a file:/// URI,
// treat it as a file reference; otherwise treat it as inline content.
//
// Note: system_prompt and agent_instructions already exist on ScionConfig
// as file-reference fields. The change is in how the values are resolved,
// not in the schema itself.

// === Harness-config fields absorbed into ScionConfig ===
User             string   `json:"user,omitempty" yaml:"user,omitempty"`             // Container unix user
AuthSelectedType string   `json:"auth_selected_type,omitempty" yaml:"auth_selected_type,omitempty"`

// === Agent operational parameters ===
Task             string   `json:"task,omitempty" yaml:"task,omitempty"`
Branch           string   `json:"branch,omitempty" yaml:"branch,omitempty"`
```

#### Content Field Resolution

The `system_prompt` and `agent_instructions` fields already exist on `ScionConfig` today, but only accept filenames. The key change is adopting a `file:///` URI convention to distinguish file references from inline content:

```go
func ResolveContent(value string, configDir string) (string, error) {
    // If the value is a file:/// URI, resolve it as a file path
    if strings.HasPrefix(value, "file:///") {
        filePath := strings.TrimPrefix(value, "file:///")
        // Absolute path — read directly
        content, err := os.ReadFile(filePath)
        if err != nil {
            return "", fmt.Errorf("failed to read content file %s: %w", filePath, err)
        }
        return string(content), nil
    }
    if strings.HasPrefix(value, "file://") {
        // Relative path — resolve against the config file's directory
        relPath := strings.TrimPrefix(value, "file://")
        absPath := filepath.Join(configDir, relPath)
        content, err := os.ReadFile(absPath)
        if err != nil {
            return "", fmt.Errorf("failed to read content file %s: %w", absPath, err)
        }
        return string(content), nil
    }
    // No file:// prefix — treat as inline content
    return value, nil
}
```

For backwards compatibility with existing templates, the template content resolution path (which loads files like `system-prompt.md` from the template directory) continues to work as before. The `file://` URI convention applies to inline configs provided via `--config` and the Hub API, where there is no implicit template directory for file resolution.

**URI convention summary:**
- `file:///absolute/path/to/file.md` — absolute file path
- `file://relative/path/to/file.md` — relative to the config file's directory
- Any other value — treated as inline content

#### Pressure Testing: Why Expand ScionConfig Instead of a New Type?

A separate `JITAgentConfig` type was considered and rejected. The single-type model is simpler, avoids conversion logic between parallel types, and means improvements to the schema automatically benefit both templates and inline configs.

Key concerns and mitigations:

- **Bloating `scion-agent.json`:** Fields like `task` and `branch` are small strings. Inline content for `system_prompt` replaces what would otherwise be a separate file on disk. The total data stored is comparable.
- **Input format vs. resolved config:** `ScionConfig` serves as both user-facing input and persisted output. Persisting operational fields like `task` and `branch` is actually desirable — they serve as a durable artifact of what the agent was created with. The `agent-info` system handles mutable agent state on disk, and heartbeats update state on the Hub API, so `scion-agent.json` is the right place for creation-time parameters.
- **Context-specific fields:** Templates can simply omit fields that don't apply. YAML/JSON `omitempty` handles this naturally.

### 3.2 Threading ScionConfig Through Agent Creation

Today, agent creation involves assembling config from multiple sources into a `ScionConfig`, plus separately extracting `HarnessConfigEntry` fields and content files. With the expanded `ScionConfig`:

1. **`ScionConfig` becomes the single carrier** for configuration data through the creation pipeline. The provisioning code receives one `ScionConfig` rather than a `ScionConfig` plus side-channel overrides.
2. **`AgentConfigOverride` is replaced.** The Hub API and `hubclient.AgentConfig` can accept a full `ScionConfig` instead of an ad-hoc subset of fields. This eliminates the need for `AgentConfigOverride` and its limited field set.
3. **`HarnessConfigEntry` fields are resolved from `ScionConfig`.** When `user` or `auth_selected_type` is set on `ScionConfig`, those values are used. When not set, the harness-config defaults still apply. The `HarnessConfigEntry` struct remains for harness-config files (`config.yaml`), but its values are lower-precedence than `ScionConfig`.

```
Precedence with inline config:

Embedded defaults
  -> Global/Grove settings
    -> Template scion-agent.yaml
      -> Harness-config config.yaml (for user, auth, home/ files)
        -> Inline config (--config file) merged over template
          -> CLI flags (--image, etc.) merged over inline config
            -> Runtime concerns (env expansion, auth injection)
```

Home directory files (`.claude.json`, `.bashrc`, etc.) remain the domain of harness-config `home/` directories. The `harness_config` field on `ScionConfig` can reference a harness-config by name to pick up these files, even when all other config is provided inline.

### 3.3 Harness Config Requirement and Merge Semantics

A harness-config is **always required** for agent creation. The harness-config is the primary means for users to specify which harness to use — the `harness` field on `HarnessConfigEntry` directs to the appropriate harness implementation in `pkg/harness`. There is no mechanism to pass a `harness` value directly into `ScionConfig` without a harness-config.

The harness-config can be resolved from:
1. The `--harness-config` CLI flag
2. The `harness_config` field in the inline config or template
3. Global settings defaults

If no harness-config can be resolved from any of these sources, agent creation fails with an error.

**Merge semantics when inline config is present:**

```
Base template (if --type also specified)
  -> Inline config merged over base (inline wins)
    -> CLI flags merged over inline (flags win)
      -> Runtime concerns (auth, env expansion) applied last
```

When an inline config is provided **without** `--type`:
- The harness-config determines the harness (via `--harness-config`, config field, or settings default)
- The inline config replaces the template layer but still composes with harness-config `home/` files and runtime-level concerns

When both `--type` and `--config` are provided:
- Inline config fields override template fields when set
- Template fields are preserved when the inline config field is empty
- This matches existing `MergeScionConfig` behavior

### 3.4 CLI Interface

```bash
# From a file
scion start my-agent --config agent-config.yaml

# From stdin (pipe from another tool)
cat config.yaml | scion start my-agent --config -

# Combined with a base template (inline overrides template)
scion start my-agent --type base-template --config overrides.yaml

# CLI flags still override everything
scion start my-agent --config config.yaml --image custom:latest
```

The `--config` flag accepts a path to a YAML or JSON file. A value of `-` reads from stdin.

#### CLI Flag Consolidation

With `--config` available, recently added CLI flags that duplicate `ScionConfig` fields can be evaluated for removal. Flags that are simple pass-throughs to a single config field (e.g., `--model`, `--max-turns`) are candidates for deprecation in favor of `--config`, reducing CLI surface area. High-frequency flags like `--image` and `--type` should remain as conveniences.

#### Config Help

The CLI should provide a way to discover and understand the config schema:

```bash
# Show the full config schema with field descriptions
scion start --config-help

# Or as a subcommand
scion config schema
```

This outputs the available `ScionConfig` fields, their types, defaults, and descriptions — enabling users to construct config files without consulting external documentation.

### 3.5 Hub API Interface

The existing `CreateAgentRequest` is extended to accept a full `ScionConfig`:

```go
// In pkg/hub/handlers.go
type CreateAgentRequest struct {
    Name          string        `json:"name"`
    GroveID       string        `json:"groveId"`
    Template      string        `json:"template,omitempty"`
    // ... existing fields ...

    // Config provides a complete inline agent configuration.
    // When set, this replaces the template as the primary config source.
    // If Template is also set, Config is merged over the template config.
    // This replaces the previous AgentConfigOverride approach.
    Config        *ScionConfig  `json:"config,omitempty"`
}
```

The Hub handler treats `Config` as a template-equivalent: it extracts the relevant fields and passes them through to the broker in the `RemoteCreateAgentRequest`.

### 3.6 Web UI Integration

With inline config supported by the Hub API (Phase 2), the web UI can present a form for agent creation that serializes directly to a `ScionConfig` JSON object. The form would cover common fields (name, model, image, environment, system prompt, task) and send them as `config` in the create request — no template creation needed.

The detailed design for the web form is deferred to a separate design round once the API surface is stable.

---

## 4. Implementation Approach

### Phase 1: Expand ScionConfig and Add CLI `--config` Flag

**Scope:** Add the new fields to `ScionConfig`, implement content resolution for inline values, and add `--config <path>` to `scion start` and `scion create`.

**Changes:**
- `pkg/api/types.go` — Add `user`, `auth_selected_type`, `task`, `branch` fields to `ScionConfig`. All fields persist to `scion-agent.json` (no `json:"-"` exclusions — these serve as a durable record of creation-time parameters).
- `pkg/agent/provision.go` — Implement `file://` URI-based content resolution for `system_prompt` and `agent_instructions`. When `user` or `auth_selected_type` is set on `ScionConfig`, apply these during harness-config resolution.
- `cmd/start.go` / `cmd/common.go` — Add `--config` flag, load file, parse as `ScionConfig`, merge into the provisioning flow. Add `--config-help` flag or `scion config schema` subcommand for discoverability. Evaluate recently added flags for potential consolidation.

**Key detail:** When `--config` is provided without `--type`, the provisioning path skips template loading and uses the inline config as the base. A harness-config must still be resolvable (via `--harness-config`, the config's `harness_config` field, or settings defaults) — otherwise agent creation fails with an error. The harness-config `home/` directory is still applied.

**Validation:**
- If neither `--type` nor `--config` is provided, existing behavior (default template)
- If `--config` is provided, a harness-config must be resolvable from the config, CLI flags, or settings
- If both `--type` and `--config`, merge config over template

### Phase 2: Hub API Support (Replace AgentConfigOverride)

**Scope:** Extend Hub create-agent API to accept a full `ScionConfig`. Remove `AgentConfigOverride`.

**Changes:**
- `pkg/hub/handlers.go` — Accept `config` (`*ScionConfig`) in `CreateAgentRequest`; merge with template if both provided; pass through to dispatcher. Remove `AgentConfigOverride` — since phases will progress rapidly, there is no need for a compatibility shim.
- `pkg/hub/httpdispatcher.go` — Include `ScionConfig` fields in `RemoteCreateAgentRequest`
- `pkg/runtimebroker/handlers.go` — Accept and apply the full `ScionConfig` during agent provisioning
- `pkg/runtimebroker/types.go` — Replace per-field overrides in `CreateAgentConfig` with a `ScionConfig` field

**Design decision:** The Hub resolves the `ScionConfig` into the existing `RemoteAgentConfig` fields, keeping the broker interface stable. The broker doesn't need to know whether config came from a template or inline. Option (B) from the original design — centralize conversion in the Hub.

### Phase 3: Web UI Form

**Scope:** Add agent creation form to the Hub web UI that generates a `ScionConfig`. Deferred to a separate design round.

### Phase 4: Config Export and Sharing

**Scope:** Allow exporting an existing agent's resolved config as a `ScionConfig` file, enabling config sharing and reproduction.

```bash
# Export current agent config as a reusable config file
scion config export my-agent > agent-config.yaml

# Start a new agent with the same config
scion start new-agent --config agent-config.yaml
```

**Changes:**
- `cmd/config.go` — Add `config export` subcommand
- `pkg/agent/` — Read agent's `scion-agent.json` + content files, produce a complete `ScionConfig`

---

## 5. Alternative Approaches Considered

### A: Separate JITAgentConfig Type (Original Draft Approach)

Introduce a new `JITAgentConfig` type that is a superset of `ScionConfig`, with conversion methods (`ToScionConfig()`, `ToHarnessConfigEntry()`).

**Pros:**
- Clean separation between user input format and resolved config
- Clear boundary: `ScionConfig` is the resolved config, `JITAgentConfig` is the input

**Cons:**
- Two types that must stay in sync — every new field requires updates in both places
- Conversion logic adds complexity and is a source of bugs
- The Hub, CLI, and broker all need to understand both types
- `AgentConfigOverride` would still exist as a third type, or need its own migration

**Verdict:** Rejected. The maintenance cost of two parallel config types outweighs the conceptual cleanliness. Persisting operational fields in `scion-agent.json` is actually beneficial as a durable artifact, and `agent-info` handles mutable state separately.

### B: Templates-as-JSON via API (Ephemeral Templates)

Instead of a new config format, the Hub could create ephemeral/anonymous templates from the web UI form, then reference them normally.

**Pros:**
- No new config path — reuses existing template machinery
- Broker doesn't change at all

**Cons:**
- Creates invisible template artifacts that need lifecycle management
- Ephemeral templates need garbage collection
- Adds latency (create template -> start agent, two-step)
- Doesn't solve the CLI use case

**Verdict:** Rejected. Adds complexity without solving the core problem.

### C: Flag Proliferation (Status Quo Extended)

Continue adding CLI flags for each new option (`--model`, `--max-turns`, `--system-prompt`, etc.).

**Pros:**
- Simple, incremental
- No new concepts

**Cons:**
- Doesn't scale — `ScionConfig` has 20+ fields, many with nested structure
- Each new field requires changes to `cmd/`, `StartOptions`, and all the plumbing
- Can't express complex structures (telemetry config, services) via flags
- Web UI still needs a different solution

**Verdict:** Rejected as a strategy. Individual high-use flags (`--model`) may still be added for convenience alongside `--config`.

### D: Inline Config as Complete Override (No Template Merge)

When `--config` is provided, completely ignore templates — no merge, no composition.

**Pros:**
- Simpler mental model: "config file = everything"
- No ambiguity about precedence

**Cons:**
- Users can't use a template as a base and override a few fields
- Forces duplication if you want "template X but with a different model"
- Loses the composability that makes the current system flexible

**Verdict:** Rejected as default behavior, but could be offered as an opt-in mode (`--config-only` or a field in the config itself: `standalone: true`).

---

## 6. Resolved Design Decisions

The following questions were raised during review and have been resolved:

### Content field resolution strategy

**Decision:** Use a `file://` URI convention to distinguish file references from inline content. `file:///` for absolute paths, `file://` (without triple slash) for paths relative to the config file's directory. Any value without a `file://` prefix is treated as inline content. Standard Go `net/url` or `path/filepath` libraries handle URI parsing. Existing template-based file resolution (which looks up filenames in the template directory) is unchanged for backwards compatibility.

### Schema versioning

**Decision:** The expanded `ScionConfig` uses the same `schema_version` field that already exists. Since this is a non-breaking, additive change, the version stays at `"1"`. Inline configs and template configs share the same schema version and evolve together.

### Validation strictness for standalone inline config

**Decision:** When used standalone (no `--type`), a harness-config must be resolvable — either via `--harness-config` flag, the config's `harness_config` field, or global settings defaults. Other fields fall back to harness-config or embedded defaults, same as templates today.

### Persistence of `task` and `branch`

**Decision:** Include both in `scion-agent.json`. They serve as a durable record of what the agent was created with, useful for auditability and config export (Phase 4). The `task` field should also be persisted back into the agent's `prompt.md` file as originally intended.

### Deprecation path for AgentConfigOverride

**Decision:** Remove `AgentConfigOverride` directly in Phase 2 without a compatibility shim. Phases will progress rapidly, and `AgentConfigOverride` has limited external consumers. `hubclient.AgentConfig` ad-hoc fields are similarly removed and replaced with a `ScionConfig` field.

### Environment/secrets gathering

**Decision:** `ScionConfig` participates fully in the env-gather flow. A `ScionConfig` can declare required environment variables or secrets by specifying a key with no value (empty string). The broker evaluates completeness the same way regardless of config source, prompting for or erroring on missing required values.

### Home directory files

**Decision:** Deferred. For Phase 1, the `harness_config` field references an existing harness-config for `home/` files. Inline file support (`home_files` map) can be added in a later phase if there is demonstrated demand.

---

## 7. Migration and Compatibility

### No Breaking Changes

- Existing `scion start --type <template>` continues to work identically
- Existing Hub API `CreateAgentRequest` without `config` is unchanged
- Existing `scion-agent.yaml` template format is unchanged and gains inline content support for free

### Migration Path

1. **Phase 1:** `ScionConfig` gains new fields. `--config` flag added to CLI. No API changes.
2. **Phase 2:** Hub API gains `config` field. `AgentConfigOverride` and `hubclient.AgentConfig` ad-hoc fields removed and replaced with `ScionConfig`.
3. **Phase 3+:** Web UI form, config export.

---

## 8. Example Config Files

### Minimal: Just override model and add telemetry

```yaml
schema_version: "1"
harness_config: claude-default
model: claude-sonnet-4-6
telemetry:
  enabled: true
```

### Full-featured: Code reviewer agent

```yaml
schema_version: "1"
harness_config: claude-default
model: claude-opus-4-6
image: us-central1-docker.pkg.dev/my-project/scion/scion-claude:latest

system_prompt: |
  You are a meticulous code reviewer. Focus on:
  - Security vulnerabilities
  - Performance issues
  - API contract violations
  Review only the files that changed. Be concise.

agent_instructions: |
  Review the current branch against main.
  Use `git diff main...HEAD` to see changes.
  Write your review as comments in a new file: REVIEW.md

env:
  REVIEW_STRICTNESS: high
  MAX_FILE_SIZE: "10000"

max_turns: 50
max_duration: 30m

resources:
  requests:
    cpu: "2"
    memory: 4Gi

task: "Review the latest changes on this branch"
```

### With file references using URI convention

```yaml
schema_version: "1"
harness_config: claude-default
model: claude-opus-4-6

# Absolute path
system_prompt: "file:///opt/prompts/code-review-system.md"

# Relative to this config file's directory
agent_instructions: "file://prompts/review-instructions.md"

task: "Review the latest changes on this branch"
```

### Template-based with overrides

```yaml
# Used with: scion start reviewer --type code-review --config this-file.yaml
schema_version: "1"
model: claude-sonnet-4-6  # Override the template's default model
env:
  REVIEW_STRICTNESS: low  # Override one env var, template's others preserved
max_turns: 20             # Shorter review
```

### Declaring required environment variables

```yaml
schema_version: "1"
harness_config: claude-default
model: claude-opus-4-6

env:
  # Set values are passed through directly
  LOG_LEVEL: debug
  # Empty values declare a requirement — the env-gather flow
  # will prompt for or error on these if not supplied
  GITHUB_TOKEN: ""
  SLACK_WEBHOOK_URL: ""
```
