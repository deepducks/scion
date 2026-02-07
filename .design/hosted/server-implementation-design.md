# Scion Server Implementation Design

## Status
**Proposed**

## 1. Overview

The Scion Server is the network-exposed component that enables distributed agent management. Three server components are implemented within the same `scion` binary, activated via the `scion server` command subgroup:

1. **Runtime Broker API** - Agent lifecycle management on compute nodes
2. **Hub API** - Centralized state management, routing, and coordination
3. **Web Frontend** - Browser-based dashboard for user interaction

This unified approach simplifies deployment while allowing flexible configuration for different operational scenarios.

### Design Goals

1. **Single Binary:** All server functionality ships in the `scion` CLI binary
2. **Modular Activation:** Enable/disable each server independently via flags
3. **Unified Configuration:** Settings flow through the same koanf-based system
4. **Consistent Ports:** Each server uses a fixed port whether run alone or together
5. **Operational Simplicity:** Standard daemon patterns (background, PID files, signals)

---

## 2. Command Interface

### 2.1 Server Command Group

```
scion server <subcommand>
```

| Subcommand | Description |
|------------|-------------|
| `start` | Start one or more server components |
| `stop` | Gracefully stop running server(s) |
| `status` | Show server status and health |
| `restart` | Stop then start servers |

### 2.2 Start Command

```
scion server start [flags]
```

**Server Selection Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--enable-runtime-broker` | `false` | Enable the Runtime Broker API |
| `--enable-hub` | `false` | Enable the Hub API |
| `--enable-web` | `false` | Enable the Web Frontend |
| `--enable-all` | `false` | Enable all servers (convenience) |

**Execution Mode:**

| Flag | Default | Description |
|------|---------|-------------|
| `--background` | `false` | Run as daemon (detach from terminal) |
| `--pid-file` | `~/.scion/server.pid` | PID file location (background mode) |

**Configuration:**

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | - | Path to dedicated server config file |
| `--settings` | - | Path to settings file (overrides default) |

**Examples:**

```bash
# Start Runtime Broker API in foreground
scion server start --enable-runtime-broker

# Start Hub API as background daemon
scion server start --enable-hub --background

# Start Hub + Web Frontend (typical hosted deployment)
scion server start --enable-hub --enable-web --background

# Start all servers
scion server start --enable-all

# Start with custom config
scion server start --enable-hub --config /etc/scion/server.yaml
```

### 2.3 Stop Command

```
scion server stop [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--force` | `false` | Force immediate shutdown (skip drain) |
| `--timeout` | `30s` | Graceful shutdown timeout |
| `--pid-file` | `~/.scion/server.pid` | PID file to read |

### 2.4 Status Command

```
scion server status [flags]
```

**Output:**
```
Scion Server Status
-------------------
Runtime Broker API: running (port 9800)
  Health: healthy
  Uptime: 2h 15m
  Agents: 5

Hub API: running (port 9810)
  Health: healthy
  Uptime: 2h 15m
  Connected Brokers: 3
  Active Groves: 12

Web Frontend: running (port 9820)
  Health: healthy
  Uptime: 2h 15m
  Hub Backend: connected (localhost:9810)
```

---

## 3. Port Assignment

Each server has a dedicated port that remains consistent regardless of deployment configuration.

| Server | Default Port | Purpose |
|--------|--------------|---------|
| Runtime Broker API | `9800` | Agent lifecycle, PTY, exec |
| Hub API | `9810` | State management, routing, WebSocket |
| Web Frontend | `9820` | Browser dashboard, static assets |

**Rationale:**
- Ports in the 9800-9899 range are unassigned by IANA
- Fixed ports enable predictable firewall/ingress configuration
- Runtime brokers can assume `localhost:9810` for Hub when not explicitly configured
- Web Frontend can assume `localhost:9810` for Hub API when co-located

**Configuration Override:**

```yaml
server:
  runtimeBroker:
    port: 9800
    host: "0.0.0.0"
  hub:
    port: 9810
    host: "0.0.0.0"
  web:
    port: 9820
    host: "0.0.0.0"
```

---

## 4. Configuration System

### 4.1 Configuration Sources (Priority Order)

Configuration is resolved using koanf with the following precedence (highest to lowest):

1. **Command-line flags** (`--port`, `--enable-hub`, etc.)
2. **Environment variables** (`SCION_SERVER_HUB_PORT`, etc.)
3. **Dedicated config file** (`--config /path/to/server.yaml`)
4. **Global settings file** (`~/.scion/settings.yaml` or `.scion/settings.yaml`)
5. **Built-in defaults**

### 4.2 Environment Variable Mapping

Environment variables follow the pattern: `SCION_<SECTION>_<KEY>`

| Variable | Maps To |
|----------|---------|
| `SCION_SERVER_RUNTIME_BROKER_PORT` | `server.runtimeBroker.port` |
| `SCION_SERVER_RUNTIME_BROKER_ENABLED` | `server.runtimeBroker.enabled` |
| `SCION_SERVER_HUB_PORT` | `server.hub.port` |
| `SCION_SERVER_HUB_ENABLED` | `server.hub.enabled` |
| `SCION_SERVER_HUB_DATABASE_URL` | `server.hub.database.url` |
| `SCION_SERVER_WEB_PORT` | `server.web.port` |
| `SCION_SERVER_WEB_ENABLED` | `server.web.enabled` |
| `SCION_SERVER_WEB_HUB_ENDPOINT` | `server.web.hubEndpoint` |
| `SCION_SERVER_TLS_CERT_FILE` | `server.tls.certFile` |
| `SCION_SERVER_TLS_KEY_FILE` | `server.tls.keyFile` |

### 4.3 Settings File Schema

```yaml
server:
  # Runtime Broker API settings
  runtimeBroker:
    enabled: false
    port: 9800
    host: "0.0.0.0"

    # Hub endpoint for status reporting (when Hub not co-located)
    hubEndpoint: ""  # Empty = localhost:9810 if Hub enabled, else disabled

  # Hub API settings
  hub:
    enabled: false
    port: 9810
    host: "0.0.0.0"

    # Database configuration
    database:
      driver: "sqlite"  # sqlite, postgres, firestore
      url: "~/.scion/hub.db"
      # postgres example: "postgres://user:pass@host:5432/scion"
      # firestore example: "firestore://project-id"

      # Connection pool (postgres only)
      maxConnections: 20
      maxIdleConnections: 5
      connectionMaxLifetime: "1h"

    # CORS settings (for web clients)
    cors:
      enabled: true
      allowedOrigins: ["*"]
      allowedMethods: ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
      allowedHeaders: ["Authorization", "Content-Type", "X-Scion-*"]
      maxAge: 3600

  # Web Frontend settings
  web:
    enabled: false
    port: 9820
    host: "0.0.0.0"

    # Hub API endpoint (for proxying/SSR)
    hubEndpoint: ""  # Empty = localhost:9810 if Hub enabled, else required

    # Static asset configuration
    assets:
      # Embedded assets are used by default; override for development
      path: ""  # Empty = use embedded assets
      cacheMaxAge: "24h"

    # Authentication
    auth:
      # OAuth provider configuration (details TBD)
      provider: ""  # google, github, oidc
      clientId: ""
      # clientSecret should be via env var: SCION_SERVER_WEB_AUTH_CLIENT_SECRET

    # Session management
    session:
      secret: ""  # Required; use env var: SCION_SERVER_WEB_SESSION_SECRET
      maxAge: "24h"
      secure: true  # Require HTTPS for cookies

  # Shared TLS settings
  tls:
    enabled: false
    certFile: ""
    keyFile: ""

    # Client certificate verification (mTLS)
    clientCA: ""
    clientAuth: "none"  # none, request, require, verify

  # Logging
  logging:
    level: "info"  # debug, info, warn, error
    format: "text"  # text, json
    output: "stderr"  # stderr, stdout, file
    file: ""  # Path when output=file

  # Metrics and observability
  metrics:
    enabled: false
    port: 9801  # Separate metrics port
    path: "/metrics"

  # Graceful shutdown
  shutdown:
    timeout: "30s"
    drainConnections: true
```

### 4.4 Hub Endpoint Auto-Discovery

When the Runtime Broker API needs to communicate with a Hub:

1. If `server.runtimeBroker.hubEndpoint` is explicitly set, use it
2. If Hub API is enabled in same process, use `localhost:<hub-port>`
3. If neither, Hub integration is disabled (Solo mode)

---

## 5. Server Architecture

### 5.1 Process Model

```
            ┌───────────────────────────────────────────────────┐
            │                   scion server                    │
            │                                                   │
            │  ┌───────────┐  ┌───────────┐  ┌───────────┐      │
            │  │  Runtime  │  │    Hub    │  │    Web    │      │
            │  │Broker API │  │    API    │  │  Frontend │      │
            │  │  :9800    │  │   :9810   │  │   :9820   │      │
            │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘      │
            │        │              │              │            │
            │        └──────────────┼──────────────┘            │
            │                       │                           │
            │                ┌──────┴──────┐                    │
            │                │   Shared    │                    │
            │                │   Services  │                    │
            │                │             │                    │
            │                │ - Logging   │                    │
            │                │ - Metrics   │                    │
            │                │ - Shutdown  │                    │
            │                │ - TLS       │                    │
            │                └─────────────┘                    │
            └───────────────────────────────────────────────────┘
```

### 5.2 Startup Sequence

```
1. Parse command-line flags
2. Load configuration (koanf merge)
3. Validate configuration
4. Initialize shared services:
   a. Logging
   b. Metrics (if enabled)
   c. TLS configuration
5. If Hub enabled:
   a. Connect to database
   b. Run migrations
   c. Initialize Hub API server
6. If Runtime Broker enabled:
   a. Detect available runtimes (Docker, K8s, Apple)
   b. Initialize agent manager
   c. Initialize Runtime Broker API server
   d. Connect to Hub (if configured)
7. If Web Frontend enabled:
   a. Load/verify static assets
   b. Initialize session store
   c. Configure Hub API client
   d. Initialize Web server
8. Start health check endpoints
9. If background mode:
   a. Write PID file
   b. Detach from terminal
10. Start HTTP listeners
11. Block on shutdown signal
```

### 5.3 Shutdown Sequence

```
1. Receive SIGTERM/SIGINT or stop command
2. Stop accepting new connections
3. If Web Frontend enabled:
   a. Invalidate sessions (optional, based on config)
   b. Close SSE/WebSocket connections to clients
4. If Hub enabled:
   a. Send disconnect to connected brokers
   b. Close WebSocket connections gracefully
5. If Runtime Broker enabled:
   a. Send heartbeat with "shutting_down" status
   b. Close control channel to Hub
6. Wait for in-flight requests (up to timeout)
7. Close database connections
8. Write final log entry
9. Remove PID file
10. Exit
```

### 5.4 Signal Handling

| Signal | Action |
|--------|--------|
| `SIGTERM` | Graceful shutdown |
| `SIGINT` | Graceful shutdown |
| `SIGHUP` | Reload configuration (future) |
| `SIGUSR1` | Dump goroutine stacks (debug) |

---

## 6. Health Endpoints

### 6.1 Runtime Broker API Health

```
GET /healthz
```

**Response:**
```json
{
  "status": "healthy",
  "version": "1.2.3",
  "uptime": "2h15m30s",
  "checks": {
    "docker": "healthy",
    "kubernetes": "unavailable",
    "hubConnection": "connected"
  }
}
```

### 6.2 Hub API Health

```
GET /healthz
```

**Response:**
```json
{
  "status": "healthy",
  "version": "1.2.3",
  "uptime": "2h15m30s",
  "checks": {
    "database": "healthy",
    "websocket": "healthy"
  },
  "stats": {
    "connectedBrokers": 3,
    "activeAgents": 15,
    "groves": 8
  }
}
```

### 6.3 Web Frontend Health

```
GET /healthz
```

**Response:**
```json
{
  "status": "healthy",
  "version": "1.2.3",
  "uptime": "2h15m30s",
  "checks": {
    "hubApi": "connected",
    "assets": "loaded",
    "sessions": "healthy"
  }
}
```

### 6.4 Readiness vs Liveness

| Endpoint | Purpose |
|----------|---------|
| `/healthz` | Liveness - is process running and not deadlocked |
| `/readyz` | Readiness - is server ready to accept traffic |

**Readiness conditions:**
- Database connected and migrated
- At least one runtime available (Runtime Broker)
- Not in shutdown mode

---

## 7. TLS Configuration

### 7.1 TLS Modes

| Mode | Use Case |
|------|----------|
| **None** | Local development, localhost only |
| **Server TLS** | Production Hub, public endpoints |
| **mTLS** | Hub-to-Broker communication, high security |

### 7.2 Certificate Configuration

```yaml
server:
  tls:
    enabled: true
    certFile: "/etc/scion/tls/server.crt"
    keyFile: "/etc/scion/tls/server.key"

    # For mTLS (Broker verification)
    clientCA: "/etc/scion/tls/ca.crt"
    clientAuth: "verify"  # require client certs
```

### 7.3 Auto-TLS (Future)

Consider Let's Encrypt integration for Hub deployments:

```yaml
server:
  tls:
    auto: true
    domains: ["hub.scion.example.com"]
    email: "admin@example.com"
```

---

## 8. Logging

### 8.1 Log Format

**Text (default, development):**
```
2025-01-24T10:30:00Z INFO  server starting port=9800 api=runtime-broker
2025-01-24T10:30:01Z INFO  agent created agent_id=abc123 grove=my-project
```

**JSON (production, structured):**
```json
{"time":"2025-01-24T10:30:00Z","level":"info","msg":"server starting","port":9800,"api":"runtime-broker"}
```

### 8.2 Log Fields

Common fields included in all log entries:

| Field | Description |
|-------|-------------|
| `time` | Timestamp (RFC3339) |
| `level` | Log level |
| `msg` | Log message |
| `api` | Which API (runtime-broker, hub) |
| `request_id` | Request correlation ID |
| `duration_ms` | Request duration (for HTTP logs) |

### 8.3 Request Logging

All HTTP requests are logged with:
- Method and path
- Response status code
- Duration
- Request ID (passed in `X-Request-ID` header or generated)

---

## 9. Metrics and Observability

### 9.1 Prometheus Metrics

Exposed on separate port (default 9801) at `/metrics`:

**Runtime Broker API:**
```
scion_runtime_agents_total{grove="...",status="running"} 5
scion_runtime_container_start_duration_seconds_bucket{le="10"} 42
scion_runtime_api_requests_total{method="POST",path="/agents",status="201"} 100
```

**Hub API:**
```
scion_hub_connected_brokers_total 3
scion_hub_active_groves_total 8
scion_hub_websocket_connections_current 15
scion_hub_api_requests_total{method="GET",path="/agents",status="200"} 500
```

### 9.2 Tracing (Future)

OpenTelemetry support for distributed tracing:

```yaml
server:
  tracing:
    enabled: true
    exporter: "otlp"
    endpoint: "localhost:4317"
    sampleRate: 0.1
```

---

## 10. Database Configuration (Hub)

### 10.1 Supported Backends

| Backend | Use Case | Connection String |
|---------|----------|-------------------|
| **SQLite** | Solo/development, single-node | `file:~/.scion/hub.db` |
| **PostgreSQL** | Production, multi-node | `postgres://user:pass@host:5432/db` |
| **Firestore** | GCP-native, serverless | `firestore://project-id` |

### 10.2 Migration Strategy

Migrations run automatically on startup:

1. Check current schema version
2. Apply pending migrations in order
3. If migration fails, abort startup with clear error

```yaml
server:
  hub:
    database:
      autoMigrate: true  # default true
      migrationLock: true  # prevent concurrent migrations
```

### 10.3 Environment Variables & Secrets Tables

The Hub stores environment variables and secrets in dedicated tables with scope-based partitioning.

#### env_vars Table

```sql
CREATE TABLE env_vars (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    scope       TEXT NOT NULL,             -- 'user', 'grove', 'runtime_broker'
    scope_id    TEXT NOT NULL,             -- user_id, grove_id, or broker_id
    description TEXT,
    sensitive   BOOLEAN DEFAULT FALSE,     -- mask in UI/logs
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  TEXT,

    UNIQUE(key, scope, scope_id)
);

CREATE INDEX idx_env_vars_scope ON env_vars(scope, scope_id);
CREATE INDEX idx_env_vars_key ON env_vars(key);
```

#### secrets Table

```sql
CREATE TABLE secrets (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,             -- Future: encrypted blob
    scope       TEXT NOT NULL,             -- 'user', 'grove', 'runtime_broker'
    scope_id    TEXT NOT NULL,             -- user_id, grove_id, or broker_id
    description TEXT,
    version     INTEGER DEFAULT 1,         -- incremented on update
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by  TEXT,
    updated_by  TEXT,

    UNIQUE(key, scope, scope_id)
);

CREATE INDEX idx_secrets_scope ON secrets(scope, scope_id);
CREATE INDEX idx_secrets_key ON secrets(key);
```

#### Secret Audit Log (Future)

For compliance and security auditing, secret access events can be logged:

```sql
CREATE TABLE secret_audit_log (
    id          TEXT PRIMARY KEY,
    secret_id   TEXT NOT NULL,
    action      TEXT NOT NULL,             -- 'created', 'updated', 'deleted', 'accessed'
    user_id     TEXT,
    agent_id    TEXT,                      -- if accessed during agent creation
    ip_address  TEXT,
    timestamp   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (secret_id) REFERENCES secrets(id) ON DELETE CASCADE
);

CREATE INDEX idx_secret_audit_log_secret ON secret_audit_log(secret_id);
CREATE INDEX idx_secret_audit_log_timestamp ON secret_audit_log(timestamp);
```

---

## 11. Background Mode

### 11.1 Daemonization

When `--background` is specified:

1. Fork child process
2. Create new session (setsid)
3. Close stdin/stdout/stderr
4. Redirect logs to file (if configured) or syslog
5. Write PID to file
6. Parent exits with success

### 11.2 PID File

- Location: `~/.scion/server.pid` (default)
- Contains: Process ID only
- Created: After successful startup
- Removed: On clean shutdown

### 11.3 Log File (Background Mode)

When running in background, logs default to:
- `~/.scion/server.log`

Override via:
```yaml
server:
  logging:
    output: "file"
    file: "/var/log/scion/server.log"
```

---

## 12. Web Frontend Server

The Web Frontend provides a browser-based dashboard for managing agents, groves, and monitoring system status. This section provides a high-level overview; detailed specifications will be documented separately.

### 12.1 Responsibilities

| Function | Description |
|----------|-------------|
| **Static Assets** | Serve compiled SPA (HTML, JS, CSS, images) |
| **Authentication** | OAuth login flow, session management |
| **API Proxy** | Optionally proxy Hub API requests (simplifies CORS) |
| **WebSocket Relay** | Relay PTY and event streams to browser clients |
| **SSR (Future)** | Server-side rendering for initial page loads |

### 12.2 Asset Embedding

Static assets are embedded in the binary for single-binary deployment:

```go
//go:embed web/dist/*
var webAssets embed.FS
```

For development, assets can be served from disk:

```yaml
server:
  web:
    assets:
      path: "./web/dist"  # Serve from filesystem instead of embedded
```

### 12.3 Authentication Flow

```
┌────────┐     ┌─────────────┐     ┌──────────────┐     ┌─────────┐
│Browser │────▶│Web Frontend │────▶│OAuth Provider│────▶│ Hub API │
│        │     │   :9820     │     │(Google/GitHub)│     │ :9810   │
└────────┘     └─────────────┘     └──────────────┘     └─────────┘
     │                │                    │                  │
     │  1. Login      │                    │                  │
     │───────────────▶│                    │                  │
     │                │  2. Redirect       │                  │
     │◀───────────────│───────────────────▶│                  │
     │  3. OAuth flow │                    │                  │
     │◀───────────────────────────────────▶│                  │
     │                │  4. Token exchange │                  │
     │                │◀───────────────────│                  │
     │                │  5. Create session │                  │
     │                │────────────────────────────────────▶│
     │  6. Set cookie │                    │                  │
     │◀───────────────│                    │                  │
```

### 12.4 Hub API Integration

The Web Frontend communicates with the Hub API in two modes:

**Proxy Mode (Default):**
- Browser → Web Frontend → Hub API
- Simplifies CORS, cookies work naturally
- Frontend handles auth header injection

**Direct Mode:**
- Browser → Hub API directly
- Web Frontend only serves assets
- Requires proper CORS configuration on Hub

```yaml
server:
  web:
    hubProxy:
      enabled: true  # Proxy mode
      pathPrefix: "/api"
```

### 12.5 WebSocket Proxying

PTY and event WebSocket connections are proxied through the Web Frontend to the Hub API, enabling cookie-based authentication for browsers:

```
Browser WS ──▶ Web Frontend ──▶ Hub API ──▶ Runtime Broker
  :9820/ws/pty    (proxy)       :9810      (control channel)
```

### 12.6 Technology Stack (TBD)

The Web Frontend implementation will be specified in a dedicated document. Considerations include:

- **SPA Framework:** React, Vue, or Svelte
- **Build Tool:** Vite, esbuild
- **UI Components:** Tailwind, shadcn/ui, or custom
- **Terminal Emulator:** xterm.js for PTY display

---

## 13. Deployment Patterns

### 13.1 Developer Laptop (Local Management)

```bash
# Start Runtime Broker reporting to team Hub
scion server start --enable-runtime-broker --background
```

```yaml
server:
  runtimeBroker:
    enabled: true
    hubEndpoint: "https://hub.team.example.com"
```

### 13.2 Self-Hosted Hub (All-in-One)

```bash
# Start all servers (Hub + Web + Runtime Broker)
scion server start --enable-all --background
```

```yaml
server:
  hub:
    enabled: true
    database:
      driver: "postgres"
      url: "postgres://scion:pass@localhost:5432/scion"
  web:
    enabled: true
    # hubEndpoint auto-detected as localhost:9810
    auth:
      provider: "oidc"
      clientId: "scion-dashboard"
  runtimeBroker:
    enabled: true
    # hubEndpoint auto-detected as localhost:9810
  tls:
    enabled: true
    certFile: "/etc/scion/tls/cert.pem"
    keyFile: "/etc/scion/tls/key.pem"
```

### 13.3 Kubernetes Runtime Broker

```bash
# Runtime Broker only, Hub is external
scion server start --enable-runtime-broker
```

```yaml
server:
  runtimeBroker:
    enabled: true
    hubEndpoint: "https://hub.scion.cloud"
  tls:
    enabled: true
    certFile: "/etc/scion/tls/tls.crt"
    keyFile: "/etc/scion/tls/tls.key"
    clientCA: "/etc/scion/tls/ca.crt"
    clientAuth: "verify"
```

### 13.4 Hosted SaaS (Hub + Web)

```bash
# Hub API + Web Frontend for cloud deployment
scion server start --enable-hub --enable-web --background
```

```yaml
server:
  hub:
    enabled: true
    port: 9810
    database:
      driver: "firestore"
      url: "firestore://scion-prod"
  web:
    enabled: true
    port: 443  # User-facing HTTPS
    hubEndpoint: "http://localhost:9810"  # Internal
    auth:
      provider: "google"
      clientId: "scion-prod.apps.googleusercontent.com"
    session:
      secure: true
  tls:
    auto: true
    domains: ["app.scion.cloud"]
  metrics:
    enabled: true
```

**Note:** In production, the Hub API (port 9810) would typically be internal-only, with the Web Frontend (port 443) being the public entry point.

---

## 14. Security Considerations

### 14.1 Network Binding

- Default bind to `0.0.0.0` for container deployments
- Recommend `127.0.0.1` for local-only development
- Production should use firewall/network policy

### 14.2 Authentication

- Runtime Broker API: Bearer token from Hub registration
- Hub API: User bearer tokens, API keys, or agent/broker tokens
- Web Frontend: Session cookies, OAuth tokens
- WebSocket: Query parameter tokens or ticket-based auth

### 14.3 Rate Limiting

```yaml
server:
  rateLimit:
    enabled: true
    requests: 1000
    window: "1m"
    burstSize: 100
```

---

## 15. Error Handling

### 15.1 Startup Errors

| Error | Exit Code | Action |
|-------|-----------|--------|
| Port already in use | 1 | Log error, exit |
| Database connection failed | 1 | Log error, exit |
| TLS cert not found | 1 | Log error, exit |
| Invalid configuration | 1 | Log validation errors, exit |
| Runtime not available | 0 | Log warning, continue without runtime |

### 15.2 Runtime Errors

| Error | Action |
|-------|--------|
| Database disconnected | Attempt reconnect with backoff |
| Hub unreachable | Queue events, retry with backoff |
| Memory pressure | Log warning, reject new requests |

---

## 16. Implementation Plan

### Phase 1: Core Server Framework
- [ ] Server command group (`server start/stop/status`)
- [ ] Configuration loading via koanf
- [ ] Graceful shutdown handling
- [ ] PID file management
- [ ] Health endpoints

### Phase 2: Runtime Broker API
- [ ] HTTP server setup
- [ ] Agent lifecycle endpoints
- [ ] WebSocket PTY endpoint
- [ ] Hub connection (when configured)

### Phase 3: Hub API
- [ ] HTTP server setup
- [ ] Database abstraction layer
- [ ] Core CRUD endpoints
- [ ] WebSocket control channel

### Phase 4: Web Frontend
- [ ] Static asset serving (embedded + filesystem)
- [ ] OAuth authentication flow
- [ ] Session management
- [ ] Hub API proxy
- [ ] WebSocket PTY relay

### Phase 5: Production Readiness
- [ ] TLS support (including auto-TLS)
- [ ] Metrics endpoints
- [ ] Log rotation
- [ ] Systemd unit file examples
- [ ] Docker/K8s deployment manifests

---

## 17. References

- **Hub API Specification:** `hub-api.md`
- **Runtime Broker API Specification:** `runtime-broker-api.md`
- **Hosted Architecture:** `hosted-architecture.md`
- **koanf Documentation:** https://github.com/knadh/koanf
