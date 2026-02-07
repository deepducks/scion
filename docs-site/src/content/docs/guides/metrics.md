---
title: Metrics & OpenTelemetry
description: Collecting and forwarding operational metrics with sciontool telemetry.
---

Scion provides built-in telemetry collection via `sciontool`, which runs as the init process in agent containers. The telemetry pipeline accepts OpenTelemetry Protocol (OTLP) data from agents and forwards it to cloud observability backends.

## Overview

The telemetry system in sciontool:

- **Receives OTLP data** via embedded gRPC (port 4317) and HTTP (port 4318) receivers
- **Filters events** based on include/exclude patterns for privacy control
- **Forwards traces and metrics** to cloud OTLP endpoints (Google Cloud, or any OTLP-compatible backend)
- **Emits correlated logs** alongside spans for system events

## Native Metrics Pipeline

Scion includes a native OTel metrics pipeline that captures operational data from agent sessions. This data is recorded as counters and histograms, providing a time-series view of agent performance.

### Automated Metrics Collection

When harness events occur (via hooks), sciontool automatically records the following metrics:

| Metric | Type | Unit | Description |
|--------|------|------|-------------|
| `gen_ai.tokens.input` | Counter | tokens | Number of input tokens processed |
| `gen_ai.tokens.output` | Counter | tokens | Number of output tokens generated |
| `gen_ai.tokens.cached` | Counter | tokens | Number of tokens retrieved from cache |
| `agent.tool.calls` | Counter | calls | Total number of tool executions |
| `agent.tool.duration` | Histogram | ms | Latency of tool executions |
| `agent.session.count` | Counter | sessions | Total number of agent sessions |
| `gen_ai.api.calls` | Counter | calls | Total number of LLM API requests |
| `gen_ai.api.duration` | Histogram | ms | Latency of LLM API requests |

### Correlated Logs

For every significant lifecycle event (session start/end, tool use, model call), sciontool emits an OTel log record that is automatically correlated with the active trace. This means when viewing a trace waterfall in your observability backend (like Google Cloud Trace), you can click directly through to the specific logs associated with each span.

## Configuration

Telemetry is configured via environment variables. This follows the same pattern as other sciontool configuration (see `pkg/sciontool/hub/client.go` for reference).

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SCION_TELEMETRY_ENABLED` | `true` | Enable/disable telemetry collection |
| `SCION_TELEMETRY_CLOUD_ENABLED` | `true` | Enable forwarding to cloud backend |
| `SCION_OTEL_ENDPOINT` | (required) | Cloud OTLP endpoint URL |
| `SCION_OTEL_PROTOCOL` | `grpc` | Protocol: `grpc` or `http` |
| `SCION_OTEL_INSECURE` | `false` | Skip TLS verification |
| `SCION_OTEL_GRPC_PORT` | `4317` | Local gRPC receiver port |
| `SCION_OTEL_HTTP_PORT` | `4318` | Local HTTP receiver port |
| `SCION_GCP_PROJECT_ID` | (auto) | GCP project ID for exporter |
| `SCION_TELEMETRY_FILTER_EXCLUDE` | `agent.user.prompt` | Comma-separated event types to exclude |
| `SCION_TELEMETRY_FILTER_INCLUDE` | (empty) | Comma-separated event types to include (empty = all) |
| `SCION_TELEMETRY_REDACT` | `prompt,user.email,tool_output,tool_input` | Comma-separated fields to redact |
| `SCION_TELEMETRY_HASH` | `session_id` | Comma-separated fields to hash (SHA256) |
| `SCION_OTEL_LOG_ENABLED` | (auto) | Enable OTel log bridge for Hub/Runtime Broker |

### Basic Setup

To enable cloud telemetry forwarding, set the endpoint:

```bash
# For Google Cloud OTLP endpoint
export SCION_OTEL_ENDPOINT="monitoring.googleapis.com:443"
export SCION_GCP_PROJECT_ID="my-project-id"

# Or for a self-hosted collector
export SCION_OTEL_ENDPOINT="otel-collector.example.com:4317"
```

### Disabling Telemetry

To disable telemetry collection entirely:

```bash
export SCION_TELEMETRY_ENABLED=false
```

To disable only cloud forwarding (local receivers still run):

```bash
export SCION_TELEMETRY_CLOUD_ENABLED=false
```

## Privacy Filtering

By default, sciontool excludes `agent.user.prompt` events to protect user privacy. You can customize filtering:

### Exclude Specific Event Types

```bash
# Exclude multiple event types
export SCION_TELEMETRY_FILTER_EXCLUDE="agent.user.prompt,agent.tool.result"
```

### Include Only Specific Event Types

```bash
# Only forward these specific event types
export SCION_TELEMETRY_FILTER_INCLUDE="agent.session.start,agent.session.end,agent.tool.call"
```

Note: The exclude filter is always applied after the include filter.

## Attribute Redaction

Beyond event filtering, sciontool provides field-level attribute redaction for sensitive data. This allows telemetry to flow while protecting specific values.

### Redacted Fields

Redacted fields have their values replaced with `[REDACTED]`:

```bash
# Default redacted fields
export SCION_TELEMETRY_REDACT="prompt,user.email,tool_output,tool_input"

# Add custom fields to redact
export SCION_TELEMETRY_REDACT="prompt,user.email,tool_output,tool_input,custom_field"

# Disable all redaction (use with caution)
export SCION_TELEMETRY_REDACT=""
```

### Hashed Fields

Hashed fields are replaced with their SHA256 hash, allowing correlation without exposing the original value:

```bash
# Default hashed fields
export SCION_TELEMETRY_HASH="session_id"

# Hash additional fields
export SCION_TELEMETRY_HASH="session_id,user_id"
```

### Default Privacy Settings

| Field | Treatment | Rationale |
|-------|-----------|-----------|
| `prompt` | Redacted | May contain sensitive user instructions |
| `user.email` | Redacted | PII protection |
| `tool_output` | Redacted | May contain file contents, credentials |
| `tool_input` | Redacted | May contain sensitive parameters |
| `session_id` | Hashed | Allows correlation without exposure |

## Receiver Ports

The telemetry receiver listens on two ports:

- **gRPC (4317)**: For OTLP gRPC clients
- **HTTP (4318)**: For OTLP HTTP clients at `/v1/traces`

Agents running inside the container can send OTLP data to `localhost:4317` (gRPC) or `localhost:4318/v1/traces` (HTTP).

### Custom Ports

If the default ports conflict with other services:

```bash
export SCION_OTEL_GRPC_PORT=14317
export SCION_OTEL_HTTP_PORT=14318
```

## Supported Backends

### Google Cloud

For Google Cloud deployments, sciontool can forward to Cloud Trace:

```bash
export SCION_OTEL_ENDPOINT="monitoring.googleapis.com:443"
export SCION_GCP_PROJECT_ID="my-project-id"
```

Authentication is handled via Application Default Credentials (ADC). Ensure the container has access to appropriate credentials.

### Generic OTLP Collector

For self-hosted or multi-cloud deployments:

```bash
export SCION_OTEL_ENDPOINT="otel-collector.internal:4317"
export SCION_OTEL_PROTOCOL="grpc"
```

For insecure connections (development only):

```bash
export SCION_OTEL_INSECURE=true
```

## Graceful Shutdown

When the agent container shuts down, sciontool allows up to 5 seconds for the telemetry pipeline to flush any buffered data before terminating.

## Troubleshooting

### Telemetry Not Starting

Check the agent logs for telemetry startup messages:

```
[sciontool] INFO: Telemetry pipeline started (gRPC: 4317, HTTP: 4318)
```

If telemetry fails to start, you'll see an error like:

```
[sciontool] ERROR: Failed to start telemetry: ...
```

Note: Telemetry failures do not block agent startup. The agent will continue running without telemetry.

### Verifying Receiver is Running

From inside the container:

```bash
netstat -tlnp | grep -E '4317|4318'
```

### Testing OTLP Export

Use `otel-cli` or similar to send test data:

```bash
otel-cli span --service test-agent --name "test-span"
```

## Hook-to-Span Conversion

Harness hook events are automatically converted to OTLP spans:

| Hook Event | Span Name | Attributes |
|------------|-----------|------------|
| `session-start` | `agent.session.start` | session_id, source |
| `session-end` | `agent.session.end` | session_id, reason, tokens_*, duration_ms |
| `tool-start` | `agent.tool.call` | tool_name, tool_input |
| `tool-end` | `agent.tool.result` | tool_name, success, duration_ms |
| `prompt-submit` | `agent.user.prompt` | prompt |
| `model-start` | `gen_ai.api.request` | model |
| `model-end` | `gen_ai.api.response` | success |

### Session Metrics (Gemini)

For Gemini CLI agents, session-end events include aggregated metrics from the session file:

- Token counts: `tokens_input`, `tokens_output`, `tokens_cached`
- Session info: `turn_count`, `duration_ms`, `model`
- Per-tool statistics: `tool.<name>.calls`, `tool.<name>.success`, `tool.<name>.errors`

Session files are automatically parsed from `~/.gemini/sessions/`.

## Implementation Details

The telemetry pipeline is implemented in `pkg/sciontool/telemetry/`:

- `config.go` - Configuration loading from environment variables
- `filter.go` - Event type filtering (include/exclude) and attribute redaction
- `exporter.go` - Cloud OTLP exporter (gRPC and HTTP)
- `receiver.go` - OTLP gRPC/HTTP receiver
- `pipeline.go` - Main orchestration (Start/Stop lifecycle)

Hook-to-span conversion is in `pkg/sciontool/hooks/handlers/`:

- `telemetry.go` - TelemetryHandler for converting hooks to spans
- Session parsing in `pkg/sciontool/hooks/session/parser.go`

The pipeline is integrated into the init command (`cmd/sciontool/commands/init.go`) and starts after user setup, before lifecycle hooks.
