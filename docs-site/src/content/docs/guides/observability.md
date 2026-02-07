---
title: Observability
description: Monitoring agents with logs and metrics.
---

Scion provides comprehensive observability for agent containers through the `sciontool` telemetry pipeline. This guide covers how to monitor agent activity, collect logs, and integrate with observability platforms.

## Architecture Overview

```
┌─────────────────────────────────────────┐
│           Agent Container               │
│                                         │
│  ┌─────────────┐                       │
│  │   Agent     │ OTLP                  │
│  │  (Claude/   │───────┐               │
│  │   Gemini)   │       │               │
│  └─────────────┘       │               │
│                        ▼               │
│              ┌─────────────────┐       │
│              │   sciontool     │       │
│              │   telemetry     │       │
│              │   pipeline      │       │
│              └────────┬────────┘       │
│                       │ Traces, Metrics,│
│                       │ Correlated Logs │
└───────────────────────┼─────────────────┘
                        │
                        ▼
              ┌─────────────────┐
              │  Cloud Backend  │
              │  (GCP, OTLP)    │
              └─────────────────┘
```

## Agent Logs

Agent logs are written to `/home/scion/agent.log` inside the container. The sciontool logging system writes to both stderr and this file.

### Log Ownership and Permissions

The `sciontool` utility ensures that `agent.log` is owned by the `scion` user during initialization, even if `sciontool` is initially run as root. The log file is created with permissive `0666` permissions to ensure multiple processes can contribute to the log stream.

If the primary log location is not writable, `sciontool` will automatically fall back to `/tmp/agent.log` and enable debug logging.

### Log Levels

- **INFO**: Normal operational events
- **ERROR**: Critical failures
- **DEBUG**: Detailed information (enabled with `SCION_DEBUG=true` or `--log-level=debug`)

### Log Format

Logs from `sciontool` and shared packages using `slog` are unified:

```
2026-02-05 10:30:00 [sciontool] [INFO] Telemetry pipeline started (gRPC: 4317, HTTP: 4318)
2026-02-05 10:30:01 [sciontool] [INFO] Running pre-start hooks...
```

### Viewing Logs

From inside the container:

```bash
tail -f /home/scion/agent.log
```

From the host (if volume mounted):

```bash
tail -f /path/to/agent-home/agent.log
```

## Telemetry Collection

The telemetry pipeline in sciontool collects and forwards OpenTelemetry (OTLP) data from agents. See the [Metrics & OpenTelemetry guide](/guides/metrics) for configuration details.

### What's Collected

| Data Type | Source | Description |
|-----------|--------|-------------|
| Traces | Agent OTLP | Span data for tool calls, API requests |
| Metrics | sciontool | Counters and histograms for tokens, tools, and latency |
| Correlated Logs | sciontool | Log records linked to traces for every hook event |
| Hook Events | Harness hooks | Tool calls, prompts, model invocations converted to spans |
| Session Metrics | Gemini session files | Token counts, turn counts, tool statistics |
| Lifecycle Events | sciontool | Pre-start, post-start, pre-stop, session-end |
| Status Updates | sciontool | Agent state changes |
| System Logs | Hub/Runtime Broker | Structured logs via OTel bridge |

### Privacy Controls

By default, user prompts (`agent.user.prompt`) are excluded from telemetry to protect privacy. Additionally, sensitive attributes are automatically redacted or hashed. See [Privacy Filtering](/guides/metrics#privacy-filtering) and [Attribute Redaction](/guides/metrics#attribute-redaction) for customization.

### Session Metrics (Gemini)

For Gemini CLI agents, sciontool automatically parses session files on session completion to extract:

- **Token usage**: Input, output, and cached tokens
- **Session info**: Turn count, duration, model used
- **Tool statistics**: Per-tool call counts, success/error rates

These metrics are included as attributes on the `agent.session.end` span.

### OTel Log Bridge (Hub & Runtime Broker)

The Hub and Runtime Broker servers can forward their internal logs to an OTLP endpoint using the OpenTelemetry log bridge pattern:

```bash
# Enable OTel log forwarding
export SCION_OTEL_LOG_ENABLED=true
export SCION_OTEL_ENDPOINT="monitoring.googleapis.com:443"
```

This allows system component logs to appear alongside agent traces in your observability backend. The log bridge runs in parallel with local logging - both destinations receive all log records.

## Integration Points

### Google Cloud

For GCP deployments, sciontool can forward telemetry to:

- **Cloud Trace**: Distributed tracing for agent operations
- **Cloud Logging**: Centralized log aggregation

See [Metrics & OpenTelemetry - Google Cloud](/guides/metrics#google-cloud) for setup.

### Self-Hosted Collectors

For on-premise or multi-cloud deployments, forward to any OTLP-compatible collector:

```bash
export SCION_OTEL_ENDPOINT="otel-collector.internal:4317"
```

## Monitoring Agent Health

### Status Files

Agents maintain status in `~/agent-info.json`:

```json
{
  "status": "running",
  "startedAt": "2026-02-05T10:30:00Z",
  "lastActivity": "2026-02-05T10:35:00Z"
}
```

### Hub Integration (Hosted Mode)

In hosted mode, agents report status to the Scion Hub via heartbeats. The Hub tracks:

- Agent status (starting, running, idle, stopping)
- Session metrics (token counts, tool usage)
- Error counts

## Troubleshooting

### Agent Not Producing Logs

1. Check that the agent home directory exists and is writable
2. Verify the agent process is running
3. Check for permission issues on the log file

### Telemetry Not Forwarding

1. Verify `SCION_TELEMETRY_ENABLED=true`
2. Check `SCION_OTEL_ENDPOINT` is set correctly
3. Look for error messages in agent logs:
   ```
   [sciontool] ERROR: Failed to start telemetry: ...
   ```

### High Telemetry Volume

Use filtering to reduce volume:

```bash
# Only forward specific event types
export SCION_TELEMETRY_FILTER_INCLUDE="agent.session.start,agent.session.end"

# Or exclude high-volume events
export SCION_TELEMETRY_FILTER_EXCLUDE="agent.tool.result,gen_ai.api.request"
```

## Related Guides

- [Metrics & OpenTelemetry](/guides/metrics) - Detailed telemetry configuration
- [Hub Server](/guides/hub-server) - Hub integration for hosted mode
