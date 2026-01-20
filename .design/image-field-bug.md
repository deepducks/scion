# Bug: Missing `image` field in ScionConfig

## Issue Description
The `scion-agent.json` configuration file, which allows defining agent-specific configuration, includes an `image` field in some templates (e.g., `pkg/config/embeds/opencode/scion-agent.json`). However, the corresponding Go struct `api.ScionConfig` (defined in `pkg/api/types.go`) does not define an `Image` field.

## Consequence
When `scion-agent.json` is loaded and unmarshaled into `api.ScionConfig` via `LoadConfig` (in `pkg/config/templates.go`), the `image` field in the JSON is ignored/discarded by the Go JSON decoder.

As a result:
1. The container image cannot be defined or overridden at the agent level via `scion-agent.json`.
2. The agent image resolution falls back to:
   - CLI flags (`--image`).
   - Harness defaults defined in `settings.json` (via `ResolveHarness`).
   - Hardcoded defaults (e.g., "gemini-cli-sandbox").

## Location
- **File**: `pkg/api/types.go`
- **Struct**: `ScionConfig`

## Reproduction
1. Create a `scion-agent.json` with `"image": "custom-image:latest"`.
2. Start the agent.
3. Observe that `custom-image:latest` is NOT used; the default harness image is used instead.

## Proposed Fix (Deferred)
To support per-agent image configuration via `scion-agent.json`:
1. Add `Image string` field to `ScionConfig` struct in `pkg/api/types.go` with `json:"image,omitempty"`.
2. Update `MergeScionConfig` in `pkg/config/templates.go` to handle merging of the `Image` field.
3. Update `AgentManager.Start` in `pkg/agent/run.go` to respect `finalScionCfg.Image` when resolving the image, giving it precedence over defaults but lower precedence than CLI overrides.
