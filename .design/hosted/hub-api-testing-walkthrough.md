# Hub API Manual Testing Walkthrough

This guide provides step-by-step instructions for manually testing the Hub API implementation.

## Prerequisites

- Go 1.21+ installed
- `curl` or similar HTTP client
- `jq` for JSON formatting (optional but recommended)

## 1. Build and Start the Server

```bash
# Build the scion binary
go build -buildvcs=false -o scion .

# Start the Hub API server (default: port 9810, SQLite database)
./scion server start

# Or with custom settings
./scion server start --port 8080 --db ./test-hub.db
```

You should see output like:
```
2025/01/25 10:00:00 Starting Hub API server on 0.0.0.0:9810
2025/01/25 10:00:00 Database: sqlite (/home/user/.scion/hub.db)
2025/01/25 10:00:00 Hub API server starting on 0.0.0.0:9810
```

## 2. Test Health Endpoints

### Health Check
```bash
curl -s http://localhost:9810/healthz | jq
```

Expected response:
```json
{
  "status": "healthy",
  "version": "0.1.0",
  "uptime": "5s",
  "checks": {
    "database": "healthy"
  },
  "stats": {
    "connectedBrokers": 0,
    "activeAgents": 0,
    "groves": 0
  }
}
```

### Readiness Check
```bash
curl -s http://localhost:9810/readyz | jq
```

Expected response:
```json
{
  "status": "ready"
}
```

## 3. Grove Registration

Groves are the primary unit of organization. Register a grove first before creating agents.

### Register a New Grove
```bash
curl -s -X POST http://localhost:9810/api/v1/groves/register \
  -H "Content-Type: application/json" \
  -d '{
    "gitRemote": "https://github.com/myorg/myproject.git",
    "name": "My Project"
  }' | jq
```

Expected response:
```json
{
  "grove": {
    "id": "abc123-...",
    "name": "My Project",
    "slug": "my-project",
    "gitRemote": "github.com/myorg/myproject",
    "created": "2025-01-25T10:00:00Z",
    "updated": "2025-01-25T10:00:00Z",
    "visibility": "private"
  },
  "created": true
}
```

Note: The git remote is normalized (scheme removed, .git suffix removed).

### Idempotent Registration
Registering the same git remote again returns the existing grove:

```bash
curl -s -X POST http://localhost:9810/api/v1/groves/register \
  -H "Content-Type: application/json" \
  -d '{
    "gitRemote": "https://github.com/myorg/myproject.git",
    "name": "My Project"
  }' | jq
```

Note `"created": false` in the response.

### List Groves
```bash
curl -s http://localhost:9810/api/v1/groves | jq
```

### Get Grove by ID
```bash
# Replace GROVE_ID with actual ID from registration
curl -s http://localhost:9810/api/v1/groves/GROVE_ID | jq
```

## 4. Agent Management

### Create an Agent
```bash
# Use the grove ID from the previous step
curl -s -X POST http://localhost:9810/api/v1/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Feature Agent",
    "groveId": "GROVE_ID",
    "template": "claude"
  }' | jq
```

Expected response:
```json
{
  "agent": {
    "id": "agent-uuid-...",
    "agentId": "feature-agent",
    "name": "Feature Agent",
    "template": "claude",
    "groveId": "GROVE_ID",
    "status": "pending",
    "detached": true,
    "visibility": "private",
    "stateVersion": 0
  }
}
```

### List Agents
```bash
curl -s http://localhost:9810/api/v1/agents | jq
```

### Filter by Grove
```bash
curl -s "http://localhost:9810/api/v1/agents?groveId=GROVE_ID" | jq
```

### Get Agent by ID
```bash
curl -s http://localhost:9810/api/v1/agents/AGENT_ID | jq
```

### Update Agent Status
```bash
curl -s -X POST http://localhost:9810/api/v1/agents/AGENT_ID/status \
  -H "Content-Type: application/json" \
  -d '{
    "status": "running",
    "connectionState": "connected"
  }' | jq
```

### Delete Agent
```bash
curl -s -X DELETE http://localhost:9810/api/v1/agents/AGENT_ID
# Returns 204 No Content on success
```

## 5. Template Management

### Create a Template
```bash
curl -s -X POST http://localhost:9810/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "claude-researcher",
    "name": "Claude Researcher",
    "harness": "claude",
    "scope": "global",
    "visibility": "public"
  }' | jq
```

### List Templates
```bash
curl -s http://localhost:9810/api/v1/templates | jq
```

### Filter by Harness
```bash
curl -s "http://localhost:9810/api/v1/templates?harness=claude" | jq
```

## 6. User Management

### Create a User
```bash
curl -s -X POST http://localhost:9810/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "email": "developer@example.com",
    "displayName": "Developer",
    "role": "member"
  }' | jq
```

### List Users
```bash
curl -s http://localhost:9810/api/v1/users | jq
```

## 7. Runtime Broker Management

Runtime brokers are typically created via grove registration with a broker payload, but can be listed:

### List Runtime Brokers
```bash
curl -s http://localhost:9810/api/v1/runtime-brokers | jq
```

### Register Grove with Broker
```bash
curl -s -X POST http://localhost:9810/api/v1/groves/register \
  -H "Content-Type: application/json" \
  -d '{
    "gitRemote": "https://github.com/myorg/another-project",
    "name": "Another Project",
    "broker": {
      "name": "My Workstation",
      "version": "0.1.0",
      "runtimes": [
        {"type": "docker", "available": true}
      ],
      "capabilities": {
        "webPty": true,
        "sync": true,
        "attach": true
      }
    }
  }' | jq
```

This creates both a grove and a runtime broker, returning a broker token.

## 8. Error Handling

### Not Found
```bash
curl -s http://localhost:9810/api/v1/agents/nonexistent | jq
```

Expected response (404):
```json
{
  "error": {
    "code": "not_found",
    "message": "Agent not found"
  }
}
```

### Validation Error
```bash
curl -s -X POST http://localhost:9810/api/v1/agents \
  -H "Content-Type: application/json" \
  -d '{"name": ""}' | jq
```

Expected response (400):
```json
{
  "error": {
    "code": "validation_error",
    "message": "name is required"
  }
}
```

### Invalid JSON
```bash
curl -s -X POST http://localhost:9810/api/v1/agents \
  -H "Content-Type: application/json" \
  -d '{invalid json}' | jq
```

Expected response (400):
```json
{
  "error": {
    "code": "invalid_request",
    "message": "Invalid request body: ..."
  }
}
```

### Method Not Allowed
```bash
curl -s -X PUT http://localhost:9810/healthz | jq
```

Expected response (405):
```json
{
  "error": {
    "code": "method_not_allowed",
    "message": "Method not allowed"
  }
}
```

## 9. CORS Testing

### Preflight Request
```bash
curl -s -X OPTIONS http://localhost:9810/api/v1/agents \
  -H "Origin: http://localhost:3000" \
  -H "Access-Control-Request-Method: POST" \
  -v 2>&1 | grep -i "access-control"
```

Should show CORS headers:
```
< Access-Control-Allow-Origin: http://localhost:3000
< Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
< Access-Control-Allow-Headers: Authorization, Content-Type, ...
```

## 10. Pagination

### List with Limit
```bash
curl -s "http://localhost:9810/api/v1/agents?limit=10" | jq
```

### Cursor-based Pagination
```bash
# Get first page
curl -s "http://localhost:9810/api/v1/agents?limit=2" | jq

# Use nextCursor from response for next page
curl -s "http://localhost:9810/api/v1/agents?limit=2&cursor=NEXT_CURSOR" | jq
```

## 11. Full Workflow Example

Here's a complete workflow script:

```bash
#!/bin/bash
set -e

BASE_URL="http://localhost:9810"

echo "=== 1. Health Check ==="
curl -s $BASE_URL/healthz | jq

echo -e "\n=== 2. Register Grove ==="
GROVE_RESPONSE=$(curl -s -X POST $BASE_URL/api/v1/groves/register \
  -H "Content-Type: application/json" \
  -d '{
    "gitRemote": "https://github.com/test/demo-project",
    "name": "Demo Project"
  }')
echo $GROVE_RESPONSE | jq
GROVE_ID=$(echo $GROVE_RESPONSE | jq -r '.grove.id')

echo -e "\n=== 3. Create Template ==="
curl -s -X POST $BASE_URL/api/v1/templates \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "demo-template",
    "name": "Demo Template",
    "harness": "claude",
    "scope": "global",
    "visibility": "public"
  }' | jq

echo -e "\n=== 4. Create Agent ==="
AGENT_RESPONSE=$(curl -s -X POST $BASE_URL/api/v1/agents \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"Demo Agent\",
    \"groveId\": \"$GROVE_ID\",
    \"template\": \"demo-template\"
  }")
echo $AGENT_RESPONSE | jq
AGENT_ID=$(echo $AGENT_RESPONSE | jq -r '.agent.id')

echo -e "\n=== 5. Update Agent Status ==="
curl -s -X POST $BASE_URL/api/v1/agents/$AGENT_ID/status \
  -H "Content-Type: application/json" \
  -d '{
    "status": "running",
    "connectionState": "connected",
    "sessionStatus": "started"
  }' | jq

echo -e "\n=== 6. List All Agents ==="
curl -s $BASE_URL/api/v1/agents | jq

echo -e "\n=== 7. Get Agent Details ==="
curl -s $BASE_URL/api/v1/agents/$AGENT_ID | jq

echo -e "\n=== 8. Final Health Check ==="
curl -s $BASE_URL/healthz | jq

echo -e "\n=== Done! ==="
```

## 12. Cleanup

To stop the server, press `Ctrl+C`. The server handles graceful shutdown.

To reset the database:
```bash
rm ~/.scion/hub.db
# Or if using custom path
rm ./test-hub.db
```

## Troubleshooting

### Port Already in Use
```bash
# Find process using port 9810
lsof -i :9810

# Use a different port
./scion server start --port 9811
```

### Database Locked
If you see "database is locked" errors, ensure only one server instance is running against the same database file.

### Permission Denied
Ensure the database directory is writable:
```bash
mkdir -p ~/.scion
chmod 755 ~/.scion
```
