# Runtime Broker API Design

## Status
**Proposed** (v3 - Aligned with Hub API)

## 1. Overview

The **Runtime Broker API** is the control plane interface exposed by a Scion Runtime Broker (e.g., a Kubernetes cluster or a dedicated Docker server). It allows the **Scion Hub** to remotely manage the lifecycle of agents, workspaces, and executions.

This API effectively exposes the `pkg/agent.Manager` interface over a network boundary, adding authentication, multi-tenancy context, and streaming capabilities for PTY access.

### 1.1. Grove-Centric Architecture

Runtime Brokers interact with the Hub through the **groves they contribute to**. A broker does not register itself as a standalone entity; instead, it registers one or more groves via `POST /api/v1/groves/register` on the Hub. This grove registration:

1. Creates or links to an existing grove (identified by git remote URL)
2. Adds this broker as a contributor to the grove
3. Returns a broker token for subsequent authentication

All agent operations on a broker are scoped to the groves it contributes to.

### 1.2. Relationship to Hub API

This document describes the **Direct HTTP** interface for Runtime Brokers. The Hub communicates with brokers via two transports:

1. **Direct HTTP** (this API): Hub calls Broker endpoints directly. Used when brokers have stable, reachable endpoints (K8s services, cloud VMs).

2. **Control Channel** (Hub API Section 10): Broker initiates WebSocket to Hub; commands are serialized over this connection. Used when brokers are behind NAT/firewalls.

Both transports use **identical command semantics**. The Hub selects the transport based on broker connectivity:
- If Broker has active control channel → use WebSocket
- If Broker has registered `endpoint` and is reachable → use Direct HTTP
- Otherwise → return error

### 1.3. Transport & Connectivity

- **Protocol:** HTTP/1.1 (REST) for control operations; WebSocket for streaming (logs, PTY).
- **Discovery:**
  - **Direct:** Hub connects to Broker IP/DNS (e.g., internal K8s service or public IP).
  - **Control Channel:** Broker establishes persistent WebSocket to Hub; Hub routes requests through this channel (for brokers behind firewalls/NAT).

### 1.4. Connectivity and Permissions

Operational capabilities are determined by the **permissions system**. A Runtime Broker is either **Online** (connected to the Hub) or **Offline**.

When a Runtime Broker is connected to a Hub, it reports status and heartbeats. The specific operations it can perform (or commands it will accept from the Hub) are governed by policies:
- **Lifecycle Management**: If permitted, the broker accepts commands to create, start, stop, and delete agents.
- **Observation**: If limited to read-only permissions, the broker will only accept status queries and log streaming requests, rejecting lifecycle commands with a `403 Forbidden` error.

In **Solo Mode** (Hub integration disabled), the API server may be disabled or limited to local-only traffic.

## 2. Authentication & Security

### 2.1. Transport Security

- **TLS:** Required for all connections (minimum TLS 1.2, prefer TLS 1.3)
- **Certificate Validation:** Required in production; self-signed certs allowed for development with explicit trust

### 2.2. Authentication Methods

Runtime Broker authentication uses **HMAC-based request signing** as the primary method. This provides mutual authentication between Hub and Runtime Brokers without requiring token transmission after initial registration.

| Header | Format | Description |
|--------|--------|-------------|
| `X-Scion-Broker-ID` | UUID or slug | Unique identifier for the Runtime Broker |
| `X-Scion-Timestamp` | RFC 3339 | Request timestamp (e.g., `2025-01-30T12:00:00Z`) |
| `X-Scion-Nonce` | Base64 (16 bytes) | Random nonce for replay prevention |
| `X-Scion-Signature` | Base64 (32 bytes) | HMAC-SHA256 signature |

The shared secret is established during broker registration (see [Runtime Broker Auth](auth/runtime-broker-auth.md) Section 3).

### 2.3. Request Signing Process

All authenticated requests between Hub and Runtime Broker are HMAC-signed:

1. **Build Canonical String:**
   ```
   METHOD + "\n" + PATH + "\n" + QUERY (sorted) + "\n" +
   TIMESTAMP + "\n" + NONCE + "\n" + CONTENT_HASH
   ```

2. **Compute Signature:** `HMAC-SHA256(shared_secret, canonical_string)`

3. **Verification:**
   - Clock skew tolerance: 5 minutes
   - Optional nonce cache for strict replay prevention
   - Constant-time signature comparison

See [Runtime Broker Auth](auth/runtime-broker-auth.md) for the complete specification.

## 3. Broker Lifecycle & Events

### 3.1. Grove Registration (Broker → Hub)

Brokers register **groves** with the Hub, not themselves as standalone entities. On startup or when linking a new grove:

```
POST {HUB_URL}/api/v1/groves/register
```

This endpoint:
1. Creates a new grove or links to an existing one (matched by git remote URL)
2. Adds this broker as a contributor to the grove
3. Returns a grove ID and broker authentication token

See Hub API Section 4.3 for the full request/response format.

### 3.2. Control Channel Connection (Broker → Hub)

After registering at least one grove, brokers establish a persistent WebSocket for real-time communication:

```
WS {HUB_URL}/api/v1/runtime-brokers/connect
```

See Hub API Section 10 for the control channel protocol.

### 3.3. Heartbeat (Broker → Hub)

Brokers report health every 30 seconds. Hub marks broker `offline` after 3 missed heartbeats.

```
POST {HUB_URL}/api/v1/runtime-brokers/{brokerId}/heartbeat
```

```json
{
  "status": "online",
  "resources": {
    "cpuAvailable": "4",
    "memoryAvailable": "8Gi",
    "agentsRunning": 3
  },
  "groves": [
    {
      "groveId": "grove-xyz",
      "agentCount": 2,
      "agents": [
        {
          "agentId": "agent-123",
          "status": "running",
          "containerStatus": "Up 2 hours"
        }
      ]
    }
  ]
}
```

### 3.4. Event Push (Broker → Hub)

Brokers push state changes via the Hub's event endpoint or control channel.

**Via HTTP (if Hub reachable):**
```
POST {HUB_URL}/api/v1/runtime-brokers/{brokerId}/events
```

**Via Control Channel:**
```json
{
  "type": "event",
  "eventId": "evt-uuid",
  "timestamp": "2025-01-24T10:00:00Z",
  "event": "agent_status",
  "payload": {
    "groveId": "grove-xyz",
    "agentId": "agent-123",
    "previousStatus": "provisioning",
    "status": "running",
    "reason": "Container started"
  }
}
```

## 4. API Resources (Hub → Broker)

Base URL: `https://{broker-endpoint}/api/v1`

### 4.1. Agents

#### Agent State Machine

```
pending → provisioning → starting → running → stopping → stopped
                                  ↘ error
```

| State | Description |
|-------|-------------|
| `pending` | Received by Broker, not yet processing |
| `provisioning` | Pulling images, creating volumes/PVCs |
| `starting` | Container created, waiting for ready check |
| `running` | Healthy and ready |
| `stopping` | Graceful shutdown in progress |
| `stopped` | Container exited normally |
| `error` | Error during lifecycle (e.g., crash loop) |

#### List Agents

Returns agents running on this broker.

```
GET /api/v1/agents
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `groveId` | string | Filter by Grove |
| `status` | string | Filter by status |
| `limit` | int | Maximum results (default 50) |
| `cursor` | string | Pagination cursor |

**Response:** `200 OK`
```json
{
  "agents": [
    {
      "id": "uuid",
      "agentId": "fix-bug",
      "name": "Fix Authentication Bug",
      "status": "running",
      "groveId": "grove-xyz",
      "containerStatus": "Up 2 hours"
    }
  ],
  "nextCursor": "cursor-xyz",
  "totalCount": 150
}
```

#### Create Agent

Provisions resources and starts an agent. Idempotent based on `requestId`.

```
POST /api/v1/agents
```

**Request Body:**
```json
{
  "requestId": "uuid",
  "agentId": "uuid",
  "name": "feature-x",
  "groveId": "grove-xyz",
  "userId": "user-abc",
  "config": {
    "template": "claude",
    "image": "scion-claude:latest",
    "homeDir": "/home/scion",
    "workspace": "/workspace",
    "repoRoot": "/repo",
    "env": ["FOO=bar", "BAZ=qux"],
    "volumes": [
      {
        "source": "/host/path",
        "target": "/container/path",
        "readOnly": false
      }
    ],
    "labels": {"env": "prod"},
    "annotations": {},
    "harness": "claude",
    "useTmux": true,
    "task": "Fix the authentication bug",
    "commandArgs": ["--resume"],
    "kubernetes": {
      "namespace": "scion",
      "resources": {
        "requests": {"cpu": "1", "memory": "2Gi"},
        "limits": {"cpu": "2", "memory": "4Gi"}
      }
    }
  },
  "resolvedEnv": {
    "ANTHROPIC_API_KEY": "sk-...",
    "LOG_LEVEL": "debug",
    "PROJECT_ID": "my-project"
  },
  "hubEndpoint": "https://hub.scion.dev",
  "agentToken": "eyJ..."
}
```

**Response:** `201 Created` or `202 Accepted` (if async)
```json
{
  "agent": Agent,
  "created": true
}
```

#### Get Agent Details

```
GET /api/v1/agents/{agentId}
```

**Response:** `200 OK`
```json
{
  "id": "uuid",
  "agentId": "fix-bug",
  "name": "Fix Authentication Bug",
  "groveId": "grove-xyz",
  "userId": "user-abc",

  "status": "running",
  "statusReason": null,
  "ready": true,
  "containerStatus": "Up 2 hours",

  "config": {
    "image": "scion-claude:latest",
    "template": "claude",
    "harness": "claude",
    "resources": {"cpu": "1", "memory": "2Gi"}
  },

  "runtime": {
    "containerId": "abc123def456",
    "node": "node-1",
    "startedAt": "2025-01-24T10:00:00Z",
    "ipAddress": "10.0.1.50"
  },

  "createdAt": "2025-01-24T09:55:00Z",
  "updatedAt": "2025-01-24T10:00:00Z"
}
```

#### Stop Agent

```
POST /api/v1/agents/{agentId}/stop
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `timeout` | int | Grace period in seconds (default 30) |

**Response:** `202 Accepted`

#### Start Agent

Resumes a stopped agent.

```
POST /api/v1/agents/{agentId}/start
```

**Response:** `202 Accepted`

#### Restart Agent

Convenience for Stop + Start.

```
POST /api/v1/agents/{agentId}/restart
```

**Response:** `202 Accepted`

#### Delete Agent

Removes the agent container and optionally cleans up the workspace.

```
DELETE /api/v1/agents/{agentId}
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `deleteFiles` | bool | Delete workspace files (default false) |
| `removeBranch` | bool | Remove git branch (default false) |

**Response:** `204 No Content`

### 4.2. Interaction & Execution

#### Send Message

Injects a message into the agent's harness (e.g., via tmux).

```
POST /api/v1/agents/{agentId}/message
```

**Request Body:**
```json
{
  "message": "Please fix the failing test.",
  "interrupt": true
}
```

**Response:** `200 OK`

#### Execute Command

Runs a one-off command inside the agent container.

```
POST /api/v1/agents/{agentId}/exec
```

**Request Body:**
```json
{
  "command": ["ls", "-la"],
  "timeout": 30
}
```

**Response:** `200 OK`
```json
{
  "output": "total 24\ndrwxr-xr-x...",
  "exitCode": 0
}
```

#### Attach PTY (WebSocket)

Provides a bidirectional stream for terminal access.

```
GET /api/v1/agents/{agentId}/attach
Upgrade: websocket
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `streamId` | string | Hub-assigned stream ID (for multiplexing) |

**Subprotocol:** `scion-pty-v1`

**Message Format:**
```json
{
  "type": "data",
  "data": "base64-encoded-bytes"
}
```

**Resize Message:**
```json
{
  "type": "resize",
  "cols": 120,
  "rows": 40
}
```

**Stream Close:**
```json
{
  "type": "close",
  "reason": "client disconnected"
}
```

### 4.3. System & Diagnostics

#### Get Logs

```
GET /api/v1/agents/{agentId}/logs
```

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `follow` | bool | Stream logs (upgrade to SSE/WS) |
| `tail` | int | Lines from end (default 100) |
| `since` | string | RFC3339 timestamp |
| `timestamps` | bool | Include timestamps |

**Response:**
- `follow=false`: `200 OK` with `text/plain` body
- `follow=true`: `200 OK` with chunked transfer or SSE

#### Get Stats

```
GET /api/v1/agents/{agentId}/stats
```

**Response:** `200 OK`
```json
{
  "cpuUsagePercent": 0.5,
  "memoryUsageBytes": 104857600,
  "memoryLimitBytes": 2147483648,
  "networkRxBytes": 1024000,
  "networkTxBytes": 512000
}
```

#### Broker Health

```
GET /healthz
```

**Response:** `200 OK`
```json
{
  "status": "healthy",
  "version": "1.2.3",
  "agentCount": 5,
  "uptime": "48h30m"
}
```

#### Broker Info

```
GET /api/v1/info
```

**Response:** `200 OK`
```json
{
  "brokerId": "broker-abc",
  "name": "Production K8s East",
  "version": "1.2.3",
  "type": "kubernetes",
  "capabilities": {
    "webPty": true,
    "sync": true,
    "attach": true
  },
  "supportedHarnesses": ["claude", "gemini"],
  "resources": {
    "cpuAvailable": "16",
    "memoryAvailable": "64Gi",
    "agentsRunning": 5,
    "agentsCapacity": 20
  },
  "groves": [
    {
      "groveId": "grove-xyz",
      "groveName": "my-project",
      "gitRemote": "github.com/org/repo",
      "profiles": ["docker", "k8s-prod"],
      "agentCount": 3
    }
  ]
}
```

## 5. Environment Variable Injection

When the Hub dispatches a `CreateAgent` command to a Runtime Broker, it includes a `resolvedEnv` field containing the fully merged environment variables and secrets for the agent.

### 5.1 Resolution Process

The Hub resolves environment variables from multiple scopes before dispatching:

1. **User scope:** Variables/secrets defined for the agent's owner
2. **Grove scope:** Variables/secrets defined for the grove
3. **Runtime Broker scope:** Variables/secrets defined for the target broker
4. **Agent config:** Variables explicitly set in the agent creation request

Later scopes override earlier ones. See `hosted-architecture.md` Section 6 for the full design.

### 5.2 Request Format

The `resolvedEnv` field in the agent creation request contains the final merged environment:

```json
{
  "resolvedEnv": {
    "ANTHROPIC_API_KEY": "sk-...",   // Secret from user scope
    "LOG_LEVEL": "debug",            // Env var from broker scope
    "PROJECT_ID": "my-project"       // Env var from grove scope
  }
}
```

The `config.env` field contains additional variables specified directly in the agent creation request. The Runtime Broker should merge both, with `config.env` taking precedence over `resolvedEnv`.

### 5.3 Injection Behavior

The Runtime Broker must:

1. Merge `resolvedEnv` with `config.env` (config.env takes precedence)
2. Inject all variables into the container environment
3. Never log secret values (variables from the secrets table)
4. Handle missing `resolvedEnv` gracefully (empty object for solo mode)

---

## 6. Data Structures & Standards

### 6.1. Error Response

All errors return a standardized JSON body:

```json
{
  "error": {
    "code": "agent_not_found",
    "message": "Agent 'agent-999' not found",
    "requestId": "req-12345",
    "details": {
      "agentId": "agent-999"
    }
  }
}
```

**Error Codes:**
| HTTP Status | Code | Description |
|-------------|------|-------------|
| 400 | `invalid_request` | Malformed request |
| 400 | `validation_error` | Validation failed |
| 401 | `unauthorized` | Missing/invalid auth |
| 403 | `forbidden` | Insufficient permissions |
| 404 | `agent_not_found` | Agent not found |
| 405 | `method_not_allowed` | Operation not allowed in current mode |
| 409 | `conflict` | Resource conflict |
| 500 | `internal_error` | Server error |

### 6.2. Type Mappings

| API Concept | Go Type (`pkg/api`) |
|-------------|---------------------|
| Agent Config | `StartOptions` / `ScionConfig` |
| Agent Details | `AgentInfo` |
| Resources | `K8sResources` |
| Volume Mount | `VolumeMount` |

### 6.3. Naming Conventions

All JSON fields use **camelCase**:
- `agentId`, `groveId`, `brokerId`
- `requestId`, `eventId`
- `createdAt`, `updatedAt`

All status values use **lowercase**:
- `pending`, `provisioning`, `running`, `stopped`, `error`

## 7. Timeouts & Limits

### 7.1. Command Timeouts

| Operation | Default | Max |
|-----------|---------|-----|
| Create Agent | 120s | 300s |
| Start Agent | 60s | 120s |
| Stop Agent | 30s | 60s |
| Delete Agent | 30s | 60s |
| Exec | 30s | 300s |
| Attach (open) | 10s | 30s |

### 7.2. Rate Limits

| Endpoint Category | Limit |
|-------------------|-------|
| Read operations | 1000/minute |
| Write operations | 100/minute |
| Agent creation | 20/minute |

## 8. Implementation Plan

1. **Phase 1:** Define Go interfaces/structs for API models (aligned with Hub API types)
2. **Phase 2:** Implement Broker API Server (wrapping `pkg/agent.Manager`)
3. **Phase 3:** Implement Hub client for Direct HTTP mode
4. **Phase 4:** Implement Control Channel adapter (translates WS commands to local API calls)

## 9. References

- **Hub API Specification:** `hub-api.md` (primary reference)
- **Architecture Overview:** `hosted-architecture.md`
- **Grove Registration:** Hub API Section 4.3
- **Control Channel Protocol:** Hub API Section 10
