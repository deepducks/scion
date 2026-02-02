# Hosted Scion Metrics System Design

## Status
**Draft** - Initial design, pending iteration

## 1. Overview

This document defines the metrics and observability architecture for the Hosted Scion platform. The design synthesizes research on LLM agent telemetry patterns (Codex, Gemini CLI, OpenCode) with the Hosted Scion architecture to create a unified observability strategy.

### Design Principles

1. **Sciontool as Primary Collector**: The `sciontool` binary running inside each agent container serves as the single point of telemetry collection, normalization, and forwarding.

2. **Cloud-Native Observability Backend**: Raw telemetry data (logs, traces, metrics) is forwarded to a dedicated cloud-based observability platform (e.g., Google Cloud Observability, Datadog, Honeycomb). The Hub does not become a general-purpose metrics or logging backend.

3. **Hub for High-Level Aggregates Only**: The Hub receives lightweight, pre-aggregated session and agent metrics for dashboard display, not raw telemetry streams.

4. **Configurable Filtering**: Sciontool provides event filtering to control volume, respect privacy settings, and honor debug mode configurations.

5. **Progressive Enhancement**: Initial implementation focuses on core metrics flow; advanced analytics via the web UI will come in a future phase.

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Agent Container                                   │
│                                                                             │
│  ┌─────────────────────┐                                                   │
│  │  Agent Process      │                                                   │
│  │  (Claude/Gemini)    │                                                   │
│  │                     │                                                   │
│  │  Emits:             │                                                   │
│  │  - OTLP (native)    │──────────┐                                        │
│  │  - JSON logs        │          │                                        │
│  │  - Hook events      │          │                                        │
│  └─────────────────────┘          │                                        │
│           │                       │                                        │
│           │ Hook calls            │ OTLP                                   │
│           ▼                       ▼                                        │
│  ┌─────────────────────────────────────────────────────────────┐           │
│  │                     Sciontool                                │           │
│  │                                                              │           │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │           │
│  │  │ Event        │  │ OTLP         │  │ Aggregation  │       │           │
│  │  │ Normalizer   │  │ Receiver     │  │ Engine       │       │           │
│  │  │              │  │ :4317        │  │              │       │           │
│  │  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘       │           │
│  │         │                 │                 │                │           │
│  │         └─────────────────┼─────────────────┘                │           │
│  │                           │                                  │           │
│  │                    ┌──────┴──────┐                          │           │
│  │                    │   Filter    │                          │           │
│  │                    │   Engine    │                          │           │
│  │                    └──────┬──────┘                          │           │
│  │                           │                                  │           │
│  │         ┌─────────────────┼─────────────────┐               │           │
│  │         │                 │                 │               │           │
│  │         ▼                 ▼                 ▼               │           │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │           │
│  │  │ Cloud        │  │ Hub          │  │ Local        │       │           │
│  │  │ Forwarder    │  │ Reporter     │  │ Debug        │       │           │
│  │  │              │  │              │  │ Output       │       │           │
│  │  └──────┬───────┘  └──────┬───────┘  └──────────────┘       │           │
│  │         │                 │                                  │           │
│  └─────────┼─────────────────┼──────────────────────────────────┘           │
│            │                 │                                              │
└────────────┼─────────────────┼──────────────────────────────────────────────┘
             │                 │
             │                 │
             ▼                 ▼
    ┌─────────────────┐  ┌─────────────────┐
    │ Cloud           │  │ Scion Hub       │
    │ Observability   │  │                 │
    │ Backend         │  │ Stores:         │
    │                 │  │ - Session       │
    │ - Full traces   │  │   summaries     │
    │ - All logs      │  │ - Agent metrics │
    │ - Raw metrics   │  │ - Activity      │
    │                 │  │                 │
    └─────────────────┘  └─────────────────┘
             │
             │ Query API
             ▼
    ┌─────────────────┐
    │ Web UI          │
    │ (Future)        │
    │                 │
    │ - Deep analytics│
    │ - Trace viewer  │
    │ - Log search    │
    └─────────────────┘
```

---

## 3. Sciontool as Primary Collector

### 3.1 Data Ingestion

Sciontool receives telemetry from agent processes through multiple channels:

| Channel | Source | Format | Example Events |
|---------|--------|--------|----------------|
| **OTLP Receiver** | Agents with native OTel (Codex, OpenCode) | OTLP gRPC/HTTP | Spans, metrics, logs |
| **Hook Events** | Harness hook calls | JSON via CLI args | `tool-start`, `tool-end`, `prompt-submit` |
| **Session Files** | Gemini CLI session JSON | File watch/poll | Token counts, tool calls |
| **Stdout/Stderr** | Agent process output | Line-based text | Structured log lines |

### 3.2 Event Normalization

All ingested data is normalized to a common schema before processing. This enables harness-agnostic analytics.

#### Normalized Event Schema

```json
{
  "timestamp": "2026-02-02T10:30:00Z",
  "event_type": "agent.tool.call",
  "session_id": "uuid",
  "agent_id": "agent-abc123",
  "grove_id": "grove-xyz",

  "attributes": {
    "tool_name": "shell_execute",
    "duration_ms": 1250,
    "success": true,
    "model": "gemini-2.0-pro"
  },

  "metrics": {
    "tokens_input": 1500,
    "tokens_output": 450,
    "tokens_cached": 800
  }
}
```

#### Event Type Catalog

Based on the normalized metrics research, sciontool recognizes these event types:

| Event Type | Category | Description |
|------------|----------|-------------|
| `agent.session.start` | Lifecycle | Agent session initiated |
| `agent.session.end` | Lifecycle | Agent session completed |
| `agent.user.prompt` | Interaction | User input received |
| `agent.response.complete` | Interaction | Agent response finished |
| `agent.tool.call` | Tool Use | Tool execution started |
| `agent.tool.result` | Tool Use | Tool execution completed |
| `agent.approval.request` | Interaction | Permission requested from user |
| `gen_ai.api.request` | LLM | API call to LLM provider |
| `gen_ai.api.response` | LLM | Response received from LLM |
| `gen_ai.api.error` | LLM | API error occurred |

### 3.3 Dialect Parsing

Each harness emits events in its native format. Sciontool's dialect parsers translate these to the normalized schema.

```
┌──────────────────────────────────────────────────────────┐
│                    Dialect Parsers                       │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │ Claude      │  │ Gemini      │  │ OpenCode    │      │
│  │ Dialect     │  │ Dialect     │  │ Dialect     │      │
│  │             │  │             │  │             │      │
│  │ Parses:     │  │ Parses:     │  │ Parses:     │      │
│  │ - CC hooks  │  │ - session   │  │ - AI SDK    │      │
│  │ - Settings  │  │   JSON      │  │   spans     │      │
│  │   events    │  │ - OTEL      │  │ - Bus       │      │
│  │             │  │   events    │  │   events    │      │
│  └─────────────┘  └─────────────┘  └─────────────┘      │
│         │                │                │              │
│         └────────────────┼────────────────┘              │
│                          ▼                               │
│              ┌─────────────────────┐                     │
│              │ Normalized Event    │                     │
│              │ Stream              │                     │
│              └─────────────────────┘                     │
└──────────────────────────────────────────────────────────┘
```

---

## 4. Data Destinations

### 4.1 Cloud Observability Backend (Primary)

The majority of telemetry data is forwarded to a cloud-based observability platform. This enables:

- Full-fidelity trace analysis
- Log search and aggregation
- Long-term metric storage
- Advanced querying and dashboards

**Supported Backends (Initial):**

| Backend | Protocol | Use Case |
|---------|----------|----------|
| Google Cloud Observability | OTLP | GCP-native deployments |
| Generic OTLP Collector | OTLP gRPC/HTTP | Self-hosted, multi-cloud |

**Future Backends:**

- Datadog
- Honeycomb
- Grafana Cloud

#### Forward Configuration

```yaml
# sciontool config (injected via env or config file)
telemetry:
  cloud:
    enabled: true
    endpoint: "otel-collector.example.com:4317"
    protocol: "grpc"  # grpc, http
    headers:
      Authorization: "Bearer ${OTEL_API_KEY}"

    # Batch settings for efficiency
    batch:
      maxSize: 512
      timeout: "5s"

    # TLS configuration
    tls:
      enabled: true
      insecureSkipVerify: false
```

#### Data Forwarded to Cloud

| Data Type | Volume | Retention (typical) |
|-----------|--------|---------------------|
| Traces | All spans | 14-30 days |
| Logs | All agent logs | 30-90 days |
| Metrics | All counters/histograms | 13 months |

### 4.2 Hub Reporting (Aggregated)

The Hub receives only lightweight, pre-aggregated data for display in the web dashboard. This keeps the Hub focused on its core responsibility: state management.

**Data Sent to Hub:**

| Metric | Aggregation | Frequency |
|--------|-------------|-----------|
| Session summary | Per-session | On session end |
| Token usage | Per-session totals | On session end |
| Tool call counts | Per-session by tool | On session end |
| Agent status | Current state | On change |
| Error counts | Rolling 1-hour window | Every 5 minutes |

#### Hub Reporting Protocol

Sciontool reports to the Hub via the existing daemon heartbeat channel, extending the payload:

```json
{
  "type": "agent_metrics",
  "agent_id": "agent-abc123",
  "timestamp": "2026-02-02T10:35:00Z",

  "session": {
    "id": "session-uuid",
    "started_at": "2026-02-02T10:00:00Z",
    "ended_at": "2026-02-02T10:35:00Z",
    "status": "completed",
    "turn_count": 15,
    "model": "gemini-2.0-pro"
  },

  "tokens": {
    "input": 45000,
    "output": 12000,
    "cached": 30000,
    "reasoning": 5000
  },

  "tools": {
    "shell_execute": { "calls": 8, "success": 7, "error": 1 },
    "read_file": { "calls": 25, "success": 25, "error": 0 },
    "write_file": { "calls": 4, "success": 4, "error": 0 }
  },

  "cost_estimate_usd": 0.42,

  "languages": ["TypeScript", "Go", "Markdown"]
}
```

#### Hub Storage

The Hub stores these summaries in a dedicated table (not raw events):

```sql
CREATE TABLE agent_session_metrics (
    id              TEXT PRIMARY KEY,
    agent_id        TEXT NOT NULL,
    grove_id        TEXT NOT NULL,
    session_id      TEXT NOT NULL,

    started_at      TIMESTAMP NOT NULL,
    ended_at        TIMESTAMP,
    status          TEXT,

    turn_count      INTEGER,
    model           TEXT,

    tokens_input    INTEGER,
    tokens_output   INTEGER,
    tokens_cached   INTEGER,
    tokens_reasoning INTEGER,

    tool_calls      JSONB,  -- {"tool_name": {"calls": N, "success": N, "error": N}}
    languages       TEXT[], -- ["TypeScript", "Go"]

    cost_estimate   DECIMAL(10, 6),

    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (agent_id) REFERENCES agents(id),
    FOREIGN KEY (grove_id) REFERENCES groves(id)
);

CREATE INDEX idx_session_metrics_agent ON agent_session_metrics(agent_id);
CREATE INDEX idx_session_metrics_grove ON agent_session_metrics(grove_id);
CREATE INDEX idx_session_metrics_time ON agent_session_metrics(started_at);
```

### 4.3 Local Debug Output

In debug mode or when cloud forwarding is disabled, sciontool can output telemetry locally for troubleshooting.

| Output | Trigger | Format |
|--------|---------|--------|
| Console (stderr) | `SCION_LOG_LEVEL=debug` | Structured text |
| File | `telemetry.local.file` configured | JSONL |
| Debug endpoint | `telemetry.local.endpoint` | OTLP to localhost |

---

## 5. Filtering and Sampling

Sciontool provides configurable filtering to manage telemetry volume and respect privacy requirements.

### 5.1 Filter Configuration

```yaml
telemetry:
  filter:
    # Global enable/disable
    enabled: true

    # Respect debug mode (SCION_LOG_LEVEL)
    respectDebugMode: true

    # Event type filtering
    events:
      # Include list (if set, only these are forwarded)
      include: []

      # Exclude list (these are never forwarded)
      exclude:
        - "agent.user.prompt"  # Privacy: don't forward user prompts by default

    # Attribute filtering
    attributes:
      # Fields to redact (replaced with "[REDACTED]")
      redact:
        - "prompt"
        - "user.email"
        - "tool_output"  # May contain sensitive file contents

      # Fields to hash (replaced with SHA256 hash)
      hash:
        - "session_id"  # For correlation without exposing raw IDs

    # Sampling (for high-volume events)
    sampling:
      # Default sample rate (1.0 = 100%)
      default: 1.0

      # Per-event-type rates
      rates:
        "gen_ai.api.request": 0.1  # Sample 10% of API requests
        "agent.tool.result": 0.5   # Sample 50% of tool results
```

### 5.2 Debug Mode Behavior

When debug mode is enabled (`SCION_LOG_LEVEL=debug`):

1. All filtering is bypassed for local output
2. Sampling rates are ignored for local output
3. Cloud forwarding still respects privacy filters (redaction)
4. Additional diagnostic events are emitted

### 5.3 Privacy Defaults

Out of the box, sciontool applies these privacy-preserving defaults:

| Data | Default Behavior | Rationale |
|------|------------------|-----------|
| User prompts | Redacted | May contain sensitive instructions |
| Tool output | Redacted | May contain file contents, credentials |
| User email | Redacted | PII |
| Session ID | Hashed | Allow correlation without exposure |
| Agent ID | Passed through | Required for routing |
| Token counts | Passed through | Non-sensitive, needed for cost tracking |

Users can opt-in to full prompt/output logging via configuration:

```yaml
telemetry:
  filter:
    attributes:
      # Override defaults to allow prompt logging
      redact: []  # Empty = no redaction
```

---

## 6. Hub Metrics API

The Hub exposes an API for retrieving aggregated metrics for display in the web UI.

### 6.1 Endpoints

#### Get Agent Metrics Summary

```
GET /api/v1/agents/{agentId}/metrics/summary
```

**Response:**
```json
{
  "agent_id": "agent-abc123",
  "period": "24h",

  "sessions": {
    "total": 15,
    "completed": 14,
    "errored": 1
  },

  "tokens": {
    "input": 450000,
    "output": 120000,
    "cached": 300000
  },

  "cost_estimate_usd": 4.20,

  "top_tools": [
    { "name": "read_file", "calls": 250, "success_rate": 1.0 },
    { "name": "shell_execute", "calls": 80, "success_rate": 0.95 },
    { "name": "write_file", "calls": 40, "success_rate": 1.0 }
  ],

  "languages": ["TypeScript", "Go", "Python"]
}
```

#### Get Grove Metrics Summary

```
GET /api/v1/groves/{groveId}/metrics/summary
```

Returns aggregated metrics across all agents in the grove.

#### Get Metrics Time Series

```
GET /api/v1/groves/{groveId}/metrics/timeseries?metric=tokens.input&period=7d&interval=1h
```

Returns time-bucketed metric values for charting.

### 6.2 What the Hub Does NOT Provide

The Hub explicitly does **not** provide:

- Raw log search/retrieval
- Trace viewing
- Full-fidelity metric queries
- Log aggregation pipelines

These capabilities are delegated to the cloud observability backend.

---

## 7. Future: Web UI Observability Features

In a future phase, the web UI will provide deeper observability by fetching data from the cloud backend.

### 7.1 Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Web UI                               │
│                                                             │
│  ┌───────────────────┐  ┌───────────────────────────────┐  │
│  │ Dashboard         │  │ Deep Analytics (Future)       │  │
│  │                   │  │                               │  │
│  │ Data from: Hub    │  │ Data from: Cloud Backend      │  │
│  │                   │  │                               │  │
│  │ - Session counts  │  │ - Trace viewer                │  │
│  │ - Token totals    │  │ - Log search                  │  │
│  │ - Cost estimates  │  │ - Custom queries              │  │
│  │ - Agent status    │  │ - Anomaly detection           │  │
│  └───────────────────┘  └───────────────────────────────┘  │
│           │                          │                      │
└───────────┼──────────────────────────┼──────────────────────┘
            │                          │
            ▼                          ▼
     ┌─────────────┐          ┌─────────────────────┐
     │  Scion Hub  │          │ Cloud Observability │
     │  API        │          │ Query API           │
     └─────────────┘          └─────────────────────┘
```

### 7.2 Planned Features

| Feature | Data Source | Priority |
|---------|-------------|----------|
| Session list with metrics | Hub | P1 |
| Token usage charts | Hub | P1 |
| Cost tracking dashboard | Hub | P1 |
| Trace waterfall view | Cloud Backend | P2 |
| Log search | Cloud Backend | P2 |
| Tool execution timeline | Cloud Backend | P2 |
| Error analysis | Cloud Backend | P3 |
| Custom metric queries | Cloud Backend | P3 |

### 7.3 Cloud Backend Integration

The web UI will authenticate to the cloud backend using one of:

1. **Proxy through Hub**: Hub makes cloud queries on behalf of UI (simpler auth)
2. **Direct with short-lived tokens**: Hub issues tokens for UI to query cloud directly

The specific approach will be determined based on the chosen cloud backend.

---

## 8. Implementation Phases

### Phase 1: Core Telemetry Pipeline

**Goal:** Establish basic telemetry flow from agents to cloud backend.

| Task | Component | Notes |
|------|-----------|-------|
| OTLP receiver in sciontool | `pkg/sciontool/telemetry` | Receive from OTel-native agents |
| Cloud forwarder | `pkg/sciontool/telemetry` | OTLP export to cloud backend |
| Basic filtering | `pkg/sciontool/telemetry` | Event include/exclude |
| Configuration loading | `cmd/sciontool` | Environment + config file |

### Phase 2: Harness Integration

**Goal:** Capture telemetry from all harness types.

| Task | Component | Notes |
|------|-----------|-------|
| Hook event normalization | `pkg/sciontool/hooks` | Convert hook calls to events |
| Gemini session file parsing | `pkg/sciontool/hooks/dialects` | Read session-*.json |
| Claude dialect parser | `pkg/sciontool/hooks/dialects` | Parse CC hook payloads |

### Phase 3: Hub Aggregation

**Goal:** Report session summaries to Hub.

| Task | Component | Notes |
|------|-----------|-------|
| In-memory aggregation engine | `pkg/sciontool/telemetry` | Per-session accumulators |
| Hub reporter | `pkg/sciontool/hub` | Extend heartbeat protocol |
| Hub metrics storage | `pkg/hub/store` | agent_session_metrics table |
| Hub metrics API | `pkg/hub/api` | Summary endpoints |

### Phase 4: Web UI Integration

**Goal:** Display metrics in web dashboard.

| Task | Component | Notes |
|------|-----------|-------|
| Session metrics component | `web/src/client` | Display session stats |
| Token usage charts | `web/src/client` | Visualization |
| Cost tracking | `web/src/client` | Aggregate cost display |

### Phase 5: Advanced Analytics (Future)

**Goal:** Deep observability via cloud backend.

| Task | Component | Notes |
|------|-----------|-------|
| Cloud backend query proxy | `pkg/hub/api` or Web | TBD |
| Trace viewer | `web/src/client` | Embedded trace UI |
| Log search | `web/src/client` | Query interface |

---

## 9. Configuration Reference

### 9.1 Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SCION_OTEL_ENDPOINT` | Cloud OTLP endpoint | (required if cloud enabled) |
| `SCION_OTEL_PROTOCOL` | OTLP protocol (grpc, http) | `grpc` |
| `SCION_OTEL_HEADERS` | Additional headers (JSON) | `{}` |
| `SCION_OTEL_INSECURE` | Skip TLS verification | `false` |
| `SCION_TELEMETRY_ENABLED` | Enable telemetry collection | `true` |
| `SCION_TELEMETRY_CLOUD_ENABLED` | Forward to cloud backend | `true` |
| `SCION_TELEMETRY_HUB_ENABLED` | Report to Hub | `true` (if hosted mode) |
| `SCION_TELEMETRY_DEBUG` | Local debug output | `false` |
| `SCION_LOG_LEVEL` | Logging verbosity | `info` |

### 9.2 Full Configuration File

```yaml
telemetry:
  enabled: true

  # Cloud forwarding
  cloud:
    enabled: true
    endpoint: "${SCION_OTEL_ENDPOINT}"
    protocol: "grpc"
    headers:
      Authorization: "Bearer ${OTEL_API_KEY}"
    tls:
      enabled: true
      insecureSkipVerify: false
    batch:
      maxSize: 512
      timeout: "5s"

  # Hub reporting
  hub:
    enabled: true  # Auto-enabled in hosted mode
    reportInterval: "30s"

  # Local debug output
  local:
    enabled: false
    file: ""  # If set, write JSONL to file
    console: false  # If true, write to stderr

  # Filtering
  filter:
    enabled: true
    respectDebugMode: true

    events:
      include: []  # Empty = all
      exclude:
        - "agent.user.prompt"

    attributes:
      redact:
        - "prompt"
        - "user.email"
        - "tool_output"
      hash:
        - "session_id"

    sampling:
      default: 1.0
      rates: {}

  # Resource attributes (added to all events)
  resource:
    service.name: "scion-agent"
    # Additional attributes from environment:
    # agent.id, grove.id, runtime.host populated automatically
```

---

## 10. Open Questions

The following questions require further discussion and resolution:

### 10.1 Cloud Backend Selection

**Question:** Which cloud observability backend should be the initial target?

**Options:**
1. Google Cloud Observability (Cloud Trace, Cloud Logging, Cloud Monitoring)
   - Pros: Native GCP integration, unified with existing infra
   - Cons: GCP lock-in
2. Generic OTLP Collector (self-hosted)
   - Pros: Flexibility, no vendor lock-in
   - Cons: Operational overhead
3. Honeycomb
   - Pros: Excellent query UX, OpenTelemetry-native
   - Cons: Cost at scale

**Impact:** Affects configuration schema, authentication approach, and web UI integration.

### 10.2 Prompt Logging Opt-In

**Question:** How should users opt-in to full prompt/response logging?

**Options:**
1. Per-agent configuration in template
2. Per-grove setting in Hub
3. Per-user preference
4. Runtime environment variable

**Impact:** Affects privacy defaults and configuration complexity.

### 10.3 Cost Estimation Accuracy

**Question:** How accurate should cost estimates be, and who provides pricing data?

**Considerations:**
- Pricing varies by model and changes over time
- Different providers have different pricing structures
- Cached tokens have different rates

**Options:**
1. Sciontool embeds static pricing tables (requires updates)
2. Hub provides pricing API (central management)
3. User configures custom rates (flexibility)

### 10.4 Session File Watching

**Question:** For Gemini CLI, should sciontool actively watch session files or parse on-demand?

**Options:**
1. File watcher (inotify/fsnotify) - real-time but more complex
2. Poll-based reading - simpler but potential gaps
3. End-of-session parsing only - simplest but no real-time metrics

**Impact:** Affects real-time dashboard updates and implementation complexity.

### 10.5 Multi-Model Sessions

**Question:** How should metrics be attributed when a single session uses multiple models?

**Example:** A session that starts with `gemini-2.0-flash` then escalates to `gemini-2.0-pro`.

**Options:**
1. Attribute to primary/first model
2. Break down by model within session
3. Attribute to session only, model breakdown in details

### 10.6 Cross-Agent Correlation

**Question:** Should the system support tracing across agent boundaries (e.g., when one agent spawns a task handled by another)?

**Options:**
1. Not initially (each agent is independent)
2. Pass trace context through agent-to-agent communication
3. Hub-level correlation via shared identifiers

**Impact:** Affects trace propagation and Hub data model.

### 10.7 Retention and Archival

**Question:** What retention policies should apply to Hub-stored session metrics?

**Considerations:**
- Storage costs
- Compliance requirements
- Historical analysis needs

**Options:**
1. Hub retains summaries indefinitely
2. Configurable retention with automatic cleanup
3. Archive to cold storage after N days

---

## 11. References

- [Normalized Metrics Research](./../metrics/metrics-data-findings.md)
- [Codex OTel Catalog](./../metrics/codex-otel.md)
- [Gemini CLI Metrics Extraction](./../metrics/gcli-session-core-logic.md)
- [Gemini Custom OTel Forwarder](./../metrics/gemini-custom-otel-forwarder.md)
- [OpenCode OTel Research](./../metrics/opencode-otel.md)
- [Sciontool Architecture](./../sciontool-overview.md)
- [Hosted Architecture](./hosted-architecture.md)
- [Server Implementation](./server-implementation-design.md)
