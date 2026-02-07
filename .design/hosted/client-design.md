# Scion API Client Libraries Design

## Status
**Proposed**

## 1. Overview

This document specifies the design for Go client libraries that provide programmatic access to both the **Hub API** and **Runtime Broker API**. These clients will be used:

1. **In the CLI** to interact with the Hub for hosted mode operations
2. **Within the Hub** to communicate with remote Runtime Brokers (Direct HTTP mode)
3. **By external consumers** who want to build integrations with the Scion hosted platform

Since external use is a design goal, these clients are implemented as public packages with stable, documented interfaces.

### 1.1. Design Principles

- **Interface-First**: Define interfaces before implementation to enable mocking and testing
- **Context-Aware**: All operations accept `context.Context` for cancellation and timeouts
- **Error Transparency**: Expose structured API errors to allow callers to react to specific conditions
- **Transport Agnostic**: Core logic is decoupled from HTTP transport where possible
- **Minimal Dependencies**: Rely only on stdlib and shared internal packages
- **Idiomatic Go**: Follow Go conventions for naming, error handling, and package layout

### 1.2. Package Structure

```
pkg/
├── hubclient/              # Hub API client
│   ├── client.go           # Client implementation
│   ├── options.go          # Client configuration options
│   ├── agents.go           # Agent operations
│   ├── groves.go           # Grove operations
│   ├── runtime_brokers.go    # Runtime broker operations
│   ├── templates.go        # Template operations
│   ├── users.go            # User operations
│   ├── auth.go             # Authentication helpers
│   ├── errors.go           # Error types and handling
│   ├── types.go            # Client-specific types
│   └── client_test.go      # Tests
│
├── brokerclient/             # Runtime Broker API client
│   ├── client.go           # Client implementation
│   ├── options.go          # Client configuration options
│   ├── agents.go           # Agent operations
│   ├── attach.go           # PTY attachment (WebSocket)
│   ├── errors.go           # Error types and handling
│   ├── types.go            # Client-specific types
│   └── client_test.go      # Tests
│
└── apiclient/              # Shared client utilities
    ├── transport.go        # HTTP client with retry, timeout handling
    ├── auth.go             # Token management interface
    ├── errors.go           # Common API error types
    └── pagination.go       # Pagination helpers
```

---

## 2. Shared Client Infrastructure (`pkg/apiclient`)

Common utilities used by both Hub and Runtime Broker clients.

### 2.1. Transport Layer

```go
package apiclient

import (
    "context"
    "net/http"
    "time"
)

// Transport provides HTTP transport with standard behaviors.
type Transport struct {
    BaseURL    string
    HTTPClient *http.Client
    UserAgent  string

    // Optional retry configuration
    MaxRetries int
    RetryWait  time.Duration
}

// TransportOption configures a Transport.
type TransportOption func(*Transport)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) TransportOption {
    return func(t *Transport) { t.HTTPClient = c }
}

// WithTimeout sets the default request timeout.
func WithTimeout(d time.Duration) TransportOption {
    return func(t *Transport) {
        t.HTTPClient.Timeout = d
    }
}

// WithRetry configures automatic retry behavior.
func WithRetry(maxRetries int, wait time.Duration) TransportOption {
    return func(t *Transport) {
        t.MaxRetries = maxRetries
        t.RetryWait = wait
    }
}

// NewTransport creates a new Transport with the given base URL and options.
func NewTransport(baseURL string, opts ...TransportOption) *Transport

// Do executes an HTTP request with configured behaviors.
// Handles retries, timeout, and wraps errors.
func (t *Transport) Do(ctx context.Context, req *http.Request) (*http.Response, error)

// Get performs an HTTP GET request.
func (t *Transport) Get(ctx context.Context, path string, headers http.Header) (*http.Response, error)

// Post performs an HTTP POST request with JSON body.
func (t *Transport) Post(ctx context.Context, path string, body interface{}, headers http.Header) (*http.Response, error)

// Put performs an HTTP PUT request with JSON body.
func (t *Transport) Put(ctx context.Context, path string, body interface{}, headers http.Header) (*http.Response, error)

// Patch performs an HTTP PATCH request with JSON body.
func (t *Transport) Patch(ctx context.Context, path string, body interface{}, headers http.Header) (*http.Response, error)

// Delete performs an HTTP DELETE request.
func (t *Transport) Delete(ctx context.Context, path string, headers http.Header) (*http.Response, error)
```

### 2.2. Authentication Interface

```go
package apiclient

import "net/http"

// Authenticator provides authentication credentials for API requests.
type Authenticator interface {
    // ApplyAuth adds authentication to the request (header, query param, etc.)
    ApplyAuth(req *http.Request) error

    // Refresh refreshes expired credentials if supported.
    // Returns false if refresh is not supported.
    Refresh() (bool, error)
}

// BearerAuth implements Bearer token authentication.
type BearerAuth struct {
    Token string
}

func (a *BearerAuth) ApplyAuth(req *http.Request) error {
    req.Header.Set("Authorization", "Bearer "+a.Token)
    return nil
}

func (a *BearerAuth) Refresh() (bool, error) { return false, nil }

// APIKeyAuth implements API key authentication.
type APIKeyAuth struct {
    Key string
}

func (a *APIKeyAuth) ApplyAuth(req *http.Request) error {
    req.Header.Set("X-API-Key", a.Key)
    return nil
}

func (a *APIKeyAuth) Refresh() (bool, error) { return false, nil }

// HostTokenAuth implements Runtime Broker token authentication.
type HostTokenAuth struct {
    Token string
}

func (a *HostTokenAuth) ApplyAuth(req *http.Request) error {
    req.Header.Set("X-Scion-Broker-Token", a.Token)
    return nil
}

func (a *HostTokenAuth) Refresh() (bool, error) { return false, nil }

// AgentTokenAuth implements Agent token authentication.
type AgentTokenAuth struct {
    Token string
}

func (a *AgentTokenAuth) ApplyAuth(req *http.Request) error {
    req.Header.Set("X-Scion-Agent-Token", a.Token)
    return nil
}

func (a *AgentTokenAuth) Refresh() (bool, error) { return false, nil }
```

### 2.3. Error Types

```go
package apiclient

import (
    "fmt"
    "net/http"
)

// APIError represents a structured error response from the API.
type APIError struct {
    StatusCode int                    // HTTP status code
    Code       string                 // Machine-readable error code
    Message    string                 // Human-readable message
    Details    map[string]interface{} // Additional context
    RequestID  string                 // Request tracking ID
}

func (e *APIError) Error() string {
    return fmt.Sprintf("%s: %s (status: %d, request: %s)",
        e.Code, e.Message, e.StatusCode, e.RequestID)
}

// IsNotFound returns true if the error is a 404 Not Found.
func (e *APIError) IsNotFound() bool {
    return e.StatusCode == http.StatusNotFound
}

// IsConflict returns true if the error is a 409 Conflict.
func (e *APIError) IsConflict() bool {
    return e.StatusCode == http.StatusConflict
}

// IsUnauthorized returns true if the error is a 401 Unauthorized.
func (e *APIError) IsUnauthorized() bool {
    return e.StatusCode == http.StatusUnauthorized
}

// IsForbidden returns true if the error is a 403 Forbidden.
func (e *APIError) IsForbidden() bool {
    return e.StatusCode == http.StatusForbidden
}

// IsRateLimited returns true if the error is a 429 Too Many Requests.
func (e *APIError) IsRateLimited() bool {
    return e.StatusCode == http.StatusTooManyRequests
}

// IsServerError returns true if the error is a 5xx error.
func (e *APIError) IsServerError() bool {
    return e.StatusCode >= 500 && e.StatusCode < 600
}

// Standard error codes (matching API specification)
const (
    ErrCodeInvalidRequest  = "invalid_request"
    ErrCodeValidationError = "validation_error"
    ErrCodeUnauthorized    = "unauthorized"
    ErrCodeForbidden       = "forbidden"
    ErrCodeNotFound        = "not_found"
    ErrCodeConflict        = "conflict"
    ErrCodeVersionConflict = "version_conflict"
    ErrCodeUnprocessable   = "unprocessable"
    ErrCodeRateLimited     = "rate_limited"
    ErrCodeInternalError   = "internal_error"
    ErrCodeRuntimeError    = "runtime_error"
    ErrCodeUnavailable     = "unavailable"
)

// ParseErrorResponse parses an API error response body.
func ParseErrorResponse(resp *http.Response) *APIError
```

### 2.4. Pagination

```go
package apiclient

// PageOptions configures pagination for list requests.
type PageOptions struct {
    Limit  int    // Maximum results per page (default varies by endpoint)
    Cursor string // Pagination cursor from previous response
}

// PageResult contains pagination metadata from a list response.
type PageResult struct {
    NextCursor string // Cursor for the next page (empty if no more pages)
    TotalCount int    // Total count of items (if available)
}

// HasMore returns true if there are more pages available.
func (p *PageResult) HasMore() bool {
    return p.NextCursor != ""
}
```

---

## 3. Hub API Client (`pkg/hubclient`)

### 3.1. Client Interface and Implementation

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
    "github.com/ptone/scion-agent/pkg/store"
)

// Client is the interface for the Hub API client.
// This interface enables mocking for tests.
type Client interface {
    // Agents returns the agent operations interface.
    Agents() AgentService

    // Groves returns the grove operations interface.
    Groves() GroveService

    // RuntimeBrokers returns the runtime broker operations interface.
    RuntimeBrokers() RuntimeBrokerService

    // Templates returns the template operations interface.
    Templates() TemplateService

    // Users returns the user operations interface.
    Users() UserService

    // Env returns the environment variable operations interface.
    Env() EnvService

    // Secrets returns the secret operations interface.
    Secrets() SecretService

    // Auth returns the authentication operations interface.
    Auth() AuthService

    // Health checks API availability.
    Health(ctx context.Context) (*HealthResponse, error)
}

// client is the concrete implementation of Client.
type client struct {
    transport *apiclient.Transport
    auth      apiclient.Authenticator

    agents       *agentService
    groves       *groveService
    runtimeBrokers *runtimeBrokerService
    templates    *templateService
    users        *userService
    authService  *authService
}

// New creates a new Hub API client.
func New(baseURL string, opts ...Option) (Client, error) {
    c := &client{
        transport: apiclient.NewTransport(baseURL),
    }

    for _, opt := range opts {
        opt(c)
    }

    // Initialize service implementations
    c.agents = &agentService{c: c}
    c.groves = &groveService{c: c}
    c.runtimeBrokers = &runtimeBrokerService{c: c}
    c.templates = &templateService{c: c}
    c.users = &userService{c: c}
    c.authService = &authService{c: c}

    return c, nil
}
```

### 3.2. Client Options

```go
package hubclient

import (
    "net/http"
    "time"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// Option configures a Hub client.
type Option func(*client)

// WithBearerToken sets Bearer token authentication.
func WithBearerToken(token string) Option {
    return func(c *client) {
        c.auth = &apiclient.BearerAuth{Token: token}
    }
}

// WithAPIKey sets API key authentication.
func WithAPIKey(key string) Option {
    return func(c *client) {
        c.auth = &apiclient.APIKeyAuth{Key: key}
    }
}

// WithAuthenticator sets a custom authenticator.
func WithAuthenticator(auth apiclient.Authenticator) Option {
    return func(c *client) {
        c.auth = auth
    }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
    return func(c *client) {
        c.transport.HTTPClient = hc
    }
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
    return func(c *client) {
        c.transport.HTTPClient.Timeout = d
    }
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, wait time.Duration) Option {
    return func(c *client) {
        c.transport.MaxRetries = maxRetries
        c.transport.RetryWait = wait
    }
}
```

### 3.3. Agent Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// AgentService handles agent operations.
type AgentService interface {
    // List returns agents matching the filter criteria.
    List(ctx context.Context, opts *ListAgentsOptions) (*ListAgentsResponse, error)

    // Get returns a single agent by ID.
    Get(ctx context.Context, agentID string) (*Agent, error)

    // Create creates a new agent.
    Create(ctx context.Context, req *CreateAgentRequest) (*CreateAgentResponse, error)

    // Update updates an agent's metadata.
    Update(ctx context.Context, agentID string, req *UpdateAgentRequest) (*Agent, error)

    // Delete removes an agent.
    Delete(ctx context.Context, agentID string, opts *DeleteAgentOptions) error

    // Start starts a stopped agent.
    Start(ctx context.Context, agentID string) error

    // Stop stops a running agent.
    Stop(ctx context.Context, agentID string) error

    // Restart restarts an agent.
    Restart(ctx context.Context, agentID string) error

    // SendMessage sends a message to an agent.
    SendMessage(ctx context.Context, agentID string, message string, interrupt bool) error

    // Exec executes a command in an agent container.
    Exec(ctx context.Context, agentID string, command []string, timeout int) (*ExecResponse, error)

    // GetLogs retrieves agent logs.
    GetLogs(ctx context.Context, agentID string, opts *GetLogsOptions) (string, error)
}

// ListAgentsOptions configures agent list filtering.
type ListAgentsOptions struct {
    GroveID       string            // Filter by grove
    Status        string            // Filter by status
    RuntimeBrokerID string            // Filter by runtime broker
    Labels        map[string]string // Label selector
    Page          apiclient.PageOptions
}

// ListAgentsResponse is the response from listing agents.
type ListAgentsResponse struct {
    Agents []Agent
    Page   apiclient.PageResult
}

// CreateAgentRequest is the request body for creating an agent.
type CreateAgentRequest struct {
    Name          string            `json:"name"`
    GroveID       string            `json:"groveId"`
    Template      string            `json:"template,omitempty"`
    RuntimeBrokerID string            `json:"runtimeBrokerId,omitempty"`
    Task          string            `json:"task,omitempty"`
    Branch        string            `json:"branch,omitempty"`
    Workspace     string            `json:"workspace,omitempty"`
    Labels        map[string]string `json:"labels,omitempty"`
    Annotations   map[string]string `json:"annotations,omitempty"`
    Config        *AgentConfig      `json:"config,omitempty"`
    Resume        bool              `json:"resume,omitempty"`
}

// CreateAgentResponse is the response from creating an agent.
type CreateAgentResponse struct {
    Agent    *Agent   `json:"agent"`
    Warnings []string `json:"warnings,omitempty"`
}

// UpdateAgentRequest is the request body for updating an agent.
type UpdateAgentRequest struct {
    Name         string            `json:"name,omitempty"`
    Labels       map[string]string `json:"labels,omitempty"`
    Annotations  map[string]string `json:"annotations,omitempty"`
    TaskSummary  string            `json:"taskSummary,omitempty"`
    StateVersion int64             `json:"stateVersion"` // Required for optimistic locking
}

// DeleteAgentOptions configures agent deletion.
type DeleteAgentOptions struct {
    DeleteFiles  bool // Also delete agent files
    RemoveBranch bool // Remove git branch
}

// GetLogsOptions configures log retrieval.
type GetLogsOptions struct {
    Tail  int    // Number of lines from end
    Since string // RFC3339 timestamp
}

// ExecResponse is the response from executing a command.
type ExecResponse struct {
    Output   string `json:"output"`
    ExitCode int    `json:"exitCode"`
}
```

### 3.4. Grove Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// GroveService handles grove operations.
type GroveService interface {
    // List returns groves matching the filter criteria.
    List(ctx context.Context, opts *ListGrovesOptions) (*ListGrovesResponse, error)

    // Get returns a single grove by ID.
    Get(ctx context.Context, groveID string) (*Grove, error)

    // Register registers a grove (upsert based on git remote).
    Register(ctx context.Context, req *RegisterGroveRequest) (*RegisterGroveResponse, error)

    // Create creates a grove without a contributing broker.
    Create(ctx context.Context, req *CreateGroveRequest) (*Grove, error)

    // Update updates grove metadata.
    Update(ctx context.Context, groveID string, req *UpdateGroveRequest) (*Grove, error)

    // Delete removes a grove.
    Delete(ctx context.Context, groveID string, deleteAgents bool) error

    // ListAgents returns agents in a grove.
    ListAgents(ctx context.Context, groveID string, opts *ListAgentsOptions) (*ListAgentsResponse, error)

    // ListContributors returns runtime brokers contributing to a grove.
    ListContributors(ctx context.Context, groveID string) (*ListContributorsResponse, error)

    // RemoveContributor removes a broker from a grove.
    RemoveContributor(ctx context.Context, groveID, brokerID string) error

    // GetSettings retrieves grove settings.
    GetSettings(ctx context.Context, groveID string) (*GroveSettings, error)

    // UpdateSettings updates grove settings.
    UpdateSettings(ctx context.Context, groveID string, settings *GroveSettings) (*GroveSettings, error)
}

// ListGrovesOptions configures grove list filtering.
type ListGrovesOptions struct {
    Visibility string // Filter by visibility
    GitRemote  string // Filter by git remote (exact or prefix)
    BrokerID   string // Filter by contributing broker
    Labels     map[string]string
    Page       apiclient.PageOptions
}

// ListGrovesResponse is the response from listing groves.
type ListGrovesResponse struct {
    Groves []Grove
    Page   apiclient.PageResult
}

// RegisterGroveRequest is the request for registering a grove.
type RegisterGroveRequest struct {
    Name      string            `json:"name"`
    GitRemote string            `json:"gitRemote"`
    Path      string            `json:"path"`
    Broker    *BrokerInfo       `json:"broker"`
    Profiles  []string          `json:"profiles,omitempty"`
    Labels    map[string]string `json:"labels,omitempty"`
}

// BrokerInfo describes the registering broker.
type BrokerInfo struct {
    ID                 string              `json:"id,omitempty"`
    Name               string              `json:"name"`
    Version            string              `json:"version"`
    Capabilities       *BrokerCapabilities `json:"capabilities,omitempty"`
    Runtimes           []BrokerRuntime     `json:"runtimes,omitempty"`
    SupportedHarnesses []string            `json:"supportedHarnesses,omitempty"`
}

// RegisterGroveResponse is the response from registering a grove.
type RegisterGroveResponse struct {
    Grove     *Grove        `json:"grove"`
    Broker      *RuntimeBroker  `json:"broker"`
    Created     bool            `json:"created"` // True if grove was newly created
    BrokerToken string          `json:"brokerToken"`
}

// CreateGroveRequest is the request for creating a grove without a broker.
type CreateGroveRequest struct {
    Name       string            `json:"name"`
    GitRemote  string            `json:"gitRemote,omitempty"`
    Visibility string            `json:"visibility,omitempty"`
    Labels     map[string]string `json:"labels,omitempty"`
}

// UpdateGroveRequest is the request for updating a grove.
type UpdateGroveRequest struct {
    Name        string            `json:"name,omitempty"`
    Labels      map[string]string `json:"labels,omitempty"`
    Annotations map[string]string `json:"annotations,omitempty"`
    Visibility  string            `json:"visibility,omitempty"`
}

// ListContributorsResponse is the response from listing grove contributors.
type ListContributorsResponse struct {
    Contributors []GroveContributor `json:"contributors"`
}
```

### 3.5. Runtime Broker Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// RuntimeBrokerService handles runtime broker operations.
type RuntimeBrokerService interface {
    // List returns runtime brokers matching the filter criteria.
    List(ctx context.Context, opts *ListBrokersOptions) (*ListBrokersResponse, error)

    // Get returns a single runtime broker by ID.
    Get(ctx context.Context, brokerID string) (*RuntimeBroker, error)

    // Update updates broker metadata.
    Update(ctx context.Context, brokerID string, req *UpdateBrokerRequest) (*RuntimeBroker, error)

    // Delete removes a broker from all groves.
    Delete(ctx context.Context, brokerID string) error

    // ListGroves returns groves this broker contributes to.
    ListGroves(ctx context.Context, brokerID string) (*ListBrokerGrovesResponse, error)

    // Heartbeat sends a heartbeat for a broker.
    Heartbeat(ctx context.Context, brokerID string, status *BrokerHeartbeat) error
}

// ListBrokersOptions configures runtime broker list filtering.
type ListBrokersOptions struct {
    Type    string // Filter by type (docker, kubernetes, apple)
    Status  string // Filter by status (online, offline)
    GroveID string // Filter by grove contribution
    Page    apiclient.PageOptions
}

// ListBrokersResponse is the response from listing runtime brokers.
type ListBrokersResponse struct {
    Brokers []RuntimeBroker
    Page  apiclient.PageResult
}

// UpdateBrokerRequest is the request for updating a runtime broker.
type UpdateBrokerRequest struct {
    Name        string            `json:"name,omitempty"`
    Labels      map[string]string `json:"labels,omitempty"`
    Annotations map[string]string `json:"annotations,omitempty"`
}

// ListBrokerGrovesResponse is the response from listing broker groves.
type ListBrokerGrovesResponse struct {
    Groves []BrokerGroveInfo `json:"groves"`
}

// BrokerGroveInfo describes a grove from a broker's perspective.
type BrokerGroveInfo struct {
    GroveID    string   `json:"groveId"`
    GroveName  string   `json:"groveName"`
    GitRemote  string   `json:"gitRemote,omitempty"`
    Profiles   []string `json:"profiles,omitempty"`
    AgentCount int      `json:"agentCount"`
}

// HostHeartbeat is the heartbeat payload.
type HostHeartbeat struct {
    Status    string            `json:"status"`
    Resources *HostResources    `json:"resources,omitempty"`
    Groves    []GroveHeartbeat  `json:"groves,omitempty"`
}

// GroveHeartbeat is per-grove status in a heartbeat.
type GroveHeartbeat struct {
    GroveID    string           `json:"groveId"`
    AgentCount int              `json:"agentCount"`
    Agents     []AgentHeartbeat `json:"agents,omitempty"`
}

// AgentHeartbeat is per-agent status in a heartbeat.
type AgentHeartbeat struct {
    AgentID         string `json:"agentId"`
    Status          string `json:"status"`
    ContainerStatus string `json:"containerStatus,omitempty"`
}
```

### 3.6. Template Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// TemplateService handles template operations.
type TemplateService interface {
    // List returns templates matching the filter criteria.
    List(ctx context.Context, opts *ListTemplatesOptions) (*ListTemplatesResponse, error)

    // Get returns a single template by ID.
    Get(ctx context.Context, templateID string) (*Template, error)

    // Create creates a new template.
    Create(ctx context.Context, req *CreateTemplateRequest) (*Template, error)

    // Update updates a template.
    Update(ctx context.Context, templateID string, req *UpdateTemplateRequest) (*Template, error)

    // Delete removes a template.
    Delete(ctx context.Context, templateID string) error

    // Clone creates a copy of a template.
    Clone(ctx context.Context, templateID string, req *CloneTemplateRequest) (*Template, error)
}

// ListTemplatesOptions configures template list filtering.
type ListTemplatesOptions struct {
    Scope   string // Filter by scope (global, grove, user)
    GroveID string // Filter by grove
    Harness string // Filter by harness type
    Page    apiclient.PageOptions
}

// ListTemplatesResponse is the response from listing templates.
type ListTemplatesResponse struct {
    Templates []Template
    Page      apiclient.PageResult
}

// CreateTemplateRequest is the request for creating a template.
type CreateTemplateRequest struct {
    Name       string          `json:"name"`
    Harness    string          `json:"harness"`
    Scope      string          `json:"scope"`
    GroveID    string          `json:"groveId,omitempty"`
    Config     *TemplateConfig `json:"config,omitempty"`
    Visibility string          `json:"visibility,omitempty"`
}

// UpdateTemplateRequest is the request for updating a template.
type UpdateTemplateRequest struct {
    Name       string          `json:"name,omitempty"`
    Config     *TemplateConfig `json:"config,omitempty"`
    Visibility string          `json:"visibility,omitempty"`
}

// CloneTemplateRequest is the request for cloning a template.
type CloneTemplateRequest struct {
    Name    string `json:"name"`
    Scope   string `json:"scope"`
    GroveID string `json:"groveId,omitempty"`
}
```

### 3.7. User Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// UserService handles user operations.
type UserService interface {
    // List returns users.
    List(ctx context.Context, opts *apiclient.PageOptions) (*ListUsersResponse, error)

    // Get returns a user by ID.
    Get(ctx context.Context, userID string) (*User, error)

    // Update updates a user.
    Update(ctx context.Context, userID string, req *UpdateUserRequest) (*User, error)
}

// ListUsersResponse is the response from listing users.
type ListUsersResponse struct {
    Users []User
    Page  apiclient.PageResult
}

// UpdateUserRequest is the request for updating a user.
type UpdateUserRequest struct {
    DisplayName string           `json:"displayName,omitempty"`
    Preferences *UserPreferences `json:"preferences,omitempty"`
}
```

### 3.8. Env Service

```go
package hubclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// EnvService handles environment variable operations.
type EnvService interface {
    // List returns environment variables for the specified scope.
    List(ctx context.Context, opts *ListEnvOptions) (*ListEnvResponse, error)

    // Get returns a specific environment variable by key.
    Get(ctx context.Context, key string, opts *EnvScopeOptions) (*EnvVar, error)

    // Set creates or updates an environment variable.
    Set(ctx context.Context, key string, req *SetEnvRequest) (*SetEnvResponse, error)

    // Delete removes an environment variable.
    Delete(ctx context.Context, key string, opts *EnvScopeOptions) error
}

// ListEnvOptions configures environment variable listing.
type ListEnvOptions struct {
    Scope   string // user, grove, runtime_broker (default: user)
    ScopeID string // ID of the scoped entity (required for grove/runtime_broker)
    Key     string // Optional: filter by specific key
}

// ListEnvResponse is the response from listing environment variables.
type ListEnvResponse struct {
    EnvVars []EnvVar `json:"envVars"`
    Scope   string   `json:"scope"`
    ScopeID string   `json:"scopeId"`
}

// EnvScopeOptions specifies the scope for get/delete operations.
type EnvScopeOptions struct {
    Scope   string // user, grove, runtime_broker (default: user)
    ScopeID string // ID of the scoped entity (required for grove/runtime_broker)
}

// SetEnvRequest is the request for setting an environment variable.
type SetEnvRequest struct {
    Value       string `json:"value"`                 // Required: variable value
    Scope       string `json:"scope,omitempty"`       // Scope type (default: user)
    ScopeID     string `json:"scopeId,omitempty"`     // Required for grove/runtime_broker scope
    Description string `json:"description,omitempty"` // Optional description
    Sensitive   bool   `json:"sensitive,omitempty"`   // Mask value in responses
}

// SetEnvResponse is the response from setting an environment variable.
type SetEnvResponse struct {
    EnvVar  *EnvVar `json:"envVar"`
    Created bool    `json:"created"` // Whether this was a new variable
}

// EnvVar represents an environment variable.
type EnvVar struct {
    ID          string    `json:"id"`
    Key         string    `json:"key"`
    Value       string    `json:"value"`
    Scope       string    `json:"scope"`
    ScopeID     string    `json:"scopeId"`
    Description string    `json:"description,omitempty"`
    Sensitive   bool      `json:"sensitive,omitempty"`
    Created     time.Time `json:"created"`
    Updated     time.Time `json:"updated"`
    CreatedBy   string    `json:"createdBy,omitempty"`
}
```

### 3.9. Secret Service

```go
package hubclient

import (
    "context"
    "time"
)

// SecretService handles secret operations.
// Note: Secret values are write-only and never returned by the API.
type SecretService interface {
    // List returns secret metadata for the specified scope.
    // Values are never returned.
    List(ctx context.Context, opts *ListSecretOptions) (*ListSecretResponse, error)

    // Get returns metadata for a specific secret by key.
    // Value is never returned.
    Get(ctx context.Context, key string, opts *SecretScopeOptions) (*Secret, error)

    // Set creates or updates a secret.
    Set(ctx context.Context, key string, req *SetSecretRequest) (*SetSecretResponse, error)

    // Delete removes a secret.
    Delete(ctx context.Context, key string, opts *SecretScopeOptions) error
}

// ListSecretOptions configures secret listing.
type ListSecretOptions struct {
    Scope   string // user, grove, runtime_broker (default: user)
    ScopeID string // ID of the scoped entity (required for grove/runtime_broker)
}

// ListSecretResponse is the response from listing secrets.
type ListSecretResponse struct {
    Secrets []Secret `json:"secrets"` // Metadata only, no values
    Scope   string   `json:"scope"`
    ScopeID string   `json:"scopeId"`
}

// SecretScopeOptions specifies the scope for get/delete operations.
type SecretScopeOptions struct {
    Scope   string // user, grove, runtime_broker (default: user)
    ScopeID string // ID of the scoped entity (required for grove/runtime_broker)
}

// SetSecretRequest is the request for setting a secret.
type SetSecretRequest struct {
    Value       string `json:"value"`                 // Required: secret value (write-only)
    Scope       string `json:"scope,omitempty"`       // Scope type (default: user)
    ScopeID     string `json:"scopeId,omitempty"`     // Required for grove/runtime_broker scope
    Description string `json:"description,omitempty"` // Optional description
}

// SetSecretResponse is the response from setting a secret.
type SetSecretResponse struct {
    Secret  *Secret `json:"secret"` // Metadata only, no value
    Created bool    `json:"created"` // Whether this was a new secret
}

// Secret represents secret metadata (value is never returned).
type Secret struct {
    ID          string    `json:"id"`
    Key         string    `json:"key"`
    Scope       string    `json:"scope"`
    ScopeID     string    `json:"scopeId"`
    Description string    `json:"description,omitempty"`
    Version     int       `json:"version"`
    Created     time.Time `json:"created"`
    Updated     time.Time `json:"updated"`
    CreatedBy   string    `json:"createdBy,omitempty"`
    UpdatedBy   string    `json:"updatedBy,omitempty"`
}
```

### 3.10. Auth Service

```go
package hubclient

import "context"

// AuthService handles authentication operations.
type AuthService interface {
    // Login performs user login.
    Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error)

    // Logout invalidates the current session.
    Logout(ctx context.Context) error

    // Refresh refreshes an access token.
    Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error)

    // Me returns the current authenticated user.
    Me(ctx context.Context) (*User, error)

    // GetWSTicket gets a short-lived WebSocket authentication ticket.
    GetWSTicket(ctx context.Context) (*WSTicketResponse, error)
}

// LoginRequest is the request for user login.
type LoginRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`
}

// LoginResponse is the response from login.
type LoginResponse struct {
    AccessToken  string `json:"accessToken"`
    RefreshToken string `json:"refreshToken"`
    ExpiresAt    string `json:"expiresAt"`
    User         *User  `json:"user"`
}

// TokenResponse is the response from token refresh.
type TokenResponse struct {
    AccessToken  string `json:"accessToken"`
    RefreshToken string `json:"refreshToken,omitempty"`
    ExpiresAt    string `json:"expiresAt"`
}

// WSTicketResponse is the response for WebSocket ticket.
type WSTicketResponse struct {
    Ticket    string `json:"ticket"`
    ExpiresAt string `json:"expiresAt"`
}
```

### 3.9. Data Types

```go
package hubclient

import "time"

// Agent represents an agent from the Hub API.
type Agent struct {
    ID              string            `json:"id"`
    AgentID         string            `json:"agentId"`
    Name            string            `json:"name"`
    Template        string            `json:"template,omitempty"`
    GroveID         string            `json:"groveId,omitempty"`
    Grove           string            `json:"grove,omitempty"`
    Labels          map[string]string `json:"labels,omitempty"`
    Annotations     map[string]string `json:"annotations,omitempty"`
    Status          string            `json:"status"`
    ConnectionState string            `json:"connectionState,omitempty"`
    ContainerStatus string            `json:"containerStatus,omitempty"`
    SessionStatus   string            `json:"sessionStatus,omitempty"`
    RuntimeState    string            `json:"runtimeState,omitempty"`
    Image           string            `json:"image,omitempty"`
    Detached        bool              `json:"detached,omitempty"`
    Runtime         string            `json:"runtime,omitempty"`
    RuntimeBrokerID   string            `json:"runtimeBrokerId,omitempty"`
    RuntimeBrokerType string            `json:"runtimeBrokerType,omitempty"`
    WebPTYEnabled   bool              `json:"webPtyEnabled,omitempty"`
    TaskSummary     string            `json:"taskSummary,omitempty"`
    AppliedConfig   *AgentConfig      `json:"appliedConfig,omitempty"`
    DirectConnect   *DirectConnect    `json:"directConnect,omitempty"`
    Kubernetes      *KubernetesInfo   `json:"kubernetes,omitempty"`
    Created         time.Time         `json:"created"`
    Updated         time.Time         `json:"updated"`
    LastSeen        time.Time         `json:"lastSeen,omitempty"`
    CreatedBy       string            `json:"createdBy,omitempty"`
    OwnerID         string            `json:"ownerId,omitempty"`
    Visibility      string            `json:"visibility,omitempty"`
    StateVersion    int64             `json:"stateVersion,omitempty"`
}

// AgentConfig represents agent configuration.
type AgentConfig struct {
    Image   string            `json:"image,omitempty"`
    Harness string            `json:"harness,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
    Model   string            `json:"model,omitempty"`
}

// DirectConnect contains direct connection info.
type DirectConnect struct {
    Enabled bool   `json:"enabled"`
    SSHHost string `json:"sshHost,omitempty"`
    SSHPort int    `json:"sshPort,omitempty"`
    SSHUser string `json:"sshUser,omitempty"`
}

// KubernetesInfo contains K8s-specific metadata.
type KubernetesInfo struct {
    Cluster   string `json:"cluster,omitempty"`
    Namespace string `json:"namespace,omitempty"`
    PodName   string `json:"podName,omitempty"`
    SyncedAt  string `json:"syncedAt,omitempty"`
}

// Grove represents a grove from the Hub API.
type Grove struct {
    ID              string            `json:"id"`
    Name            string            `json:"name"`
    Slug            string            `json:"slug"`
    GitRemote       string            `json:"gitRemote,omitempty"`
    Created         time.Time         `json:"created"`
    Updated         time.Time         `json:"updated"`
    CreatedBy       string            `json:"createdBy,omitempty"`
    OwnerID         string            `json:"ownerId,omitempty"`
    Visibility      string            `json:"visibility,omitempty"`
    Labels          map[string]string `json:"labels,omitempty"`
    Annotations     map[string]string `json:"annotations,omitempty"`
    Contributors    []GroveContributor `json:"contributors,omitempty"`
    AgentCount      int               `json:"agentCount,omitempty"`
    ActiveBrokerCount int             `json:"activeBrokerCount,omitempty"`
}

// GroveContributor represents a broker contributing to a grove.
type GroveContributor struct {
    BrokerID  string    `json:"brokerId"`
    BrokerName string   `json:"brokerName"`
    Status    string    `json:"status"`
    Profiles  []string  `json:"profiles,omitempty"`
    LastSeen  time.Time `json:"lastSeen,omitempty"`
    LocalPath string    `json:"localPath,omitempty"`
}

// GroveSettings represents grove configuration settings.
type GroveSettings struct {
    ActiveProfile   string                 `json:"activeProfile,omitempty"`
    DefaultTemplate string                 `json:"defaultTemplate,omitempty"`
    Bucket          *BucketConfig          `json:"bucket,omitempty"`
    Runtimes        map[string]interface{} `json:"runtimes,omitempty"`
    Harnesses       map[string]interface{} `json:"harnesses,omitempty"`
    Profiles        map[string]interface{} `json:"profiles,omitempty"`
}

// BucketConfig represents cloud storage configuration.
type BucketConfig struct {
    Provider string `json:"provider"`
    Name     string `json:"name"`
    Prefix   string `json:"prefix,omitempty"`
}

// RuntimeBroker represents a runtime broker from the Hub API.
type RuntimeBroker struct {
    ID                 string            `json:"id"`
    Name               string            `json:"name"`
    Slug               string            `json:"slug"`
    Type               string            `json:"type"`
    Version            string            `json:"version"`
    Status             string            `json:"status"`
    ConnectionState    string            `json:"connectionState"`
    LastHeartbeat      time.Time         `json:"lastHeartbeat,omitempty"`
    Capabilities       *BrokerCapabilities `json:"capabilities,omitempty"`
    SupportedHarnesses []string            `json:"supportedHarnesses,omitempty"`
    Resources          *BrokerResources    `json:"resources,omitempty"`
    Runtimes           []BrokerRuntime     `json:"runtimes,omitempty"`
    Labels             map[string]string `json:"labels,omitempty"`
    Annotations        map[string]string `json:"annotations,omitempty"`
    Endpoint           string            `json:"endpoint,omitempty"`
    Groves             []BrokerGroveInfo  `json:"groves,omitempty"`
    Created            time.Time         `json:"created"`
    Updated            time.Time         `json:"updated"`
}

// BrokerCapabilities describes runtime broker capabilities.
type BrokerCapabilities struct {
    WebPTY bool `json:"webPty"`
    Sync   bool `json:"sync"`
    Attach bool `json:"attach"`
}

// BrokerResources describes broker resource availability.
type BrokerResources struct {
    CPUAvailable    string `json:"cpuAvailable,omitempty"`
    MemoryAvailable string `json:"memoryAvailable,omitempty"`
    AgentsRunning   int    `json:"agentsRunning,omitempty"`
    AgentsCapacity  int    `json:"agentsCapacity,omitempty"`
}

// BrokerRuntime describes a container runtime on a broker.
type BrokerRuntime struct {
    Type      string `json:"type"`
    Available bool   `json:"available"`
    Context   string `json:"context,omitempty"`
    Namespace string `json:"namespace,omitempty"`
}

// Template represents a template from the Hub API.
type Template struct {
    ID         string          `json:"id"`
    Name       string          `json:"name"`
    Slug       string          `json:"slug"`
    Harness    string          `json:"harness"`
    Image      string          `json:"image,omitempty"`
    Config     *TemplateConfig `json:"config,omitempty"`
    Scope      string          `json:"scope"`
    GroveID    string          `json:"groveId,omitempty"`
    OwnerID    string          `json:"ownerId,omitempty"`
    Visibility string          `json:"visibility,omitempty"`
    StorageURI string          `json:"storageUri,omitempty"`
    Created    time.Time       `json:"created"`
    Updated    time.Time       `json:"updated"`
}

// TemplateConfig holds template configuration.
type TemplateConfig struct {
    Harness     string            `json:"harness,omitempty"`
    ConfigDir   string            `json:"configDir,omitempty"`
    Env         map[string]string `json:"env,omitempty"`
    Detached    bool              `json:"detached,omitempty"`
    CommandArgs []string          `json:"commandArgs,omitempty"`
    Model       string            `json:"model,omitempty"`
}

// User represents a user from the Hub API.
type User struct {
    ID          string           `json:"id"`
    Email       string           `json:"email"`
    DisplayName string           `json:"displayName"`
    AvatarURL   string           `json:"avatarUrl,omitempty"`
    Role        string           `json:"role"`
    Status      string           `json:"status"`
    Preferences *UserPreferences `json:"preferences,omitempty"`
    Created     time.Time        `json:"created"`
    LastLogin   time.Time        `json:"lastLogin,omitempty"`
}

// UserPreferences holds user preferences.
type UserPreferences struct {
    DefaultTemplate string `json:"defaultTemplate,omitempty"`
    DefaultProfile  string `json:"defaultProfile,omitempty"`
    Theme           string `json:"theme,omitempty"`
}

// HealthResponse is the response from health check.
type HealthResponse struct {
    Status  string            `json:"status"`
    Version string            `json:"version"`
    Uptime  string            `json:"uptime"`
    Checks  map[string]string `json:"checks,omitempty"`
}
```

---

## 4. Runtime Broker API Client (`pkg/brokerclient`)

### 4.1. Client Interface and Implementation

```go
package brokerclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
    "github.com/ptone/scion-agent/pkg/runtimebroker"
)

// Client is the interface for the Runtime Broker API client.
type Client interface {
    // Agents returns the agent operations interface.
    Agents() AgentService

    // Info returns broker information.
    Info(ctx context.Context) (*runtimebroker.BrokerInfoResponse, error)

    // Health checks broker availability.
    Health(ctx context.Context) (*runtimebroker.HealthResponse, error)
}

// client is the concrete implementation of Client.
type client struct {
    transport *apiclient.Transport
    auth      apiclient.Authenticator
    agents    *agentService
}

// New creates a new Runtime Broker API client.
func New(baseURL string, opts ...Option) (Client, error) {
    c := &client{
        transport: apiclient.NewTransport(baseURL),
    }

    for _, opt := range opts {
        opt(c)
    }

    c.agents = &agentService{c: c}

    return c, nil
}
```

### 4.2. Client Options

```go
package brokerclient

import (
    "net/http"
    "time"

    "github.com/ptone/scion-agent/pkg/apiclient"
)

// Option configures a Runtime Broker client.
type Option func(*client)

// WithBearerToken sets Bearer token authentication.
func WithBearerToken(token string) Option {
    return func(c *client) {
        c.auth = &apiclient.BearerAuth{Token: token}
    }
}

// WithHostToken sets Runtime Broker token authentication.
func WithHostToken(token string) Option {
    return func(c *client) {
        c.auth = &apiclient.HostTokenAuth{Token: token}
    }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
    return func(c *client) {
        c.transport.HTTPClient = hc
    }
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
    return func(c *client) {
        c.transport.HTTPClient.Timeout = d
    }
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, wait time.Duration) Option {
    return func(c *client) {
        c.transport.MaxRetries = maxRetries
        c.transport.RetryWait = wait
    }
}
```

### 4.3. Agent Service

```go
package brokerclient

import (
    "context"

    "github.com/ptone/scion-agent/pkg/apiclient"
    "github.com/ptone/scion-agent/pkg/runtimebroker"
)

// AgentService handles agent operations on a runtime broker.
type AgentService interface {
    // List returns agents on this broker.
    List(ctx context.Context, opts *ListAgentsOptions) (*runtimebroker.ListAgentsResponse, error)

    // Get returns a single agent by ID.
    Get(ctx context.Context, agentID string) (*runtimebroker.AgentResponse, error)

    // Create creates and starts a new agent.
    Create(ctx context.Context, req *runtimebroker.CreateAgentRequest) (*runtimebroker.CreateAgentResponse, error)

    // Start starts a stopped agent.
    Start(ctx context.Context, agentID string) error

    // Stop stops a running agent.
    Stop(ctx context.Context, agentID string, timeout int) error

    // Restart restarts an agent.
    Restart(ctx context.Context, agentID string) error

    // Delete removes an agent.
    Delete(ctx context.Context, agentID string, opts *DeleteAgentOptions) error

    // SendMessage sends a message to an agent.
    SendMessage(ctx context.Context, agentID string, message string, interrupt bool) error

    // Exec executes a command in an agent container.
    Exec(ctx context.Context, agentID string, command []string, timeout int) (*runtimebroker.ExecResponse, error)

    // GetLogs retrieves agent logs.
    GetLogs(ctx context.Context, agentID string, opts *GetLogsOptions) (string, error)

    // GetStats retrieves agent resource statistics.
    GetStats(ctx context.Context, agentID string) (*runtimebroker.StatsResponse, error)

    // Attach returns a PTY attachment for the agent.
    // The caller is responsible for closing the returned AttachSession.
    Attach(ctx context.Context, agentID string, opts *AttachOptions) (*AttachSession, error)
}

// ListAgentsOptions configures agent list filtering.
type ListAgentsOptions struct {
    GroveID string
    Status  string
    Page    apiclient.PageOptions
}

// DeleteAgentOptions configures agent deletion.
type DeleteAgentOptions struct {
    DeleteFiles  bool
    RemoveBranch bool
}

// GetLogsOptions configures log retrieval.
type GetLogsOptions struct {
    Follow     bool   // Stream logs
    Tail       int    // Lines from end
    Since      string // RFC3339 timestamp
    Timestamps bool   // Include timestamps
}

// AttachOptions configures PTY attachment.
type AttachOptions struct {
    StreamID string // Hub-assigned stream ID (for multiplexing)
    Cols     int    // Terminal columns
    Rows     int    // Terminal rows
}
```

### 4.4. PTY Attachment

```go
package brokerclient

import (
    "context"
    "io"

    "github.com/gorilla/websocket"
)

// AttachSession represents an active PTY connection to an agent.
type AttachSession struct {
    conn *websocket.Conn
}

// Read reads data from the PTY.
// Implements io.Reader for convenience.
func (s *AttachSession) Read(p []byte) (n int, err error)

// Write writes data to the PTY.
// Implements io.Writer for convenience.
func (s *AttachSession) Write(p []byte) (n int, err error)

// Resize sends a terminal resize message.
func (s *AttachSession) Resize(cols, rows int) error

// Close closes the PTY connection.
func (s *AttachSession) Close() error

// ReadMessage reads a raw WebSocket message.
// Returns message type and payload.
func (s *AttachSession) ReadMessage() (messageType int, p []byte, err error)

// WriteMessage writes a raw WebSocket message.
func (s *AttachSession) WriteMessage(messageType int, data []byte) error

// Stream provides a bidirectional stream interface.
// Copies between the session and the provided reader/writer until EOF or error.
func (s *AttachSession) Stream(ctx context.Context, stdin io.Reader, stdout io.Writer) error
```

---

## 5. Error Handling Patterns

### 5.1. Checking Error Types

```go
package main

import (
    "context"
    "errors"
    "fmt"

    "github.com/ptone/scion-agent/pkg/apiclient"
    "github.com/ptone/scion-agent/pkg/hubclient"
)

func example(ctx context.Context, client hubclient.Client) {
    agent, err := client.Agents().Get(ctx, "my-agent")
    if err != nil {
        var apiErr *apiclient.APIError
        if errors.As(err, &apiErr) {
            switch {
            case apiErr.IsNotFound():
                fmt.Println("Agent not found")
            case apiErr.IsUnauthorized():
                fmt.Println("Authentication required")
            case apiErr.IsForbidden():
                fmt.Println("Access denied")
            case apiErr.IsConflict():
                // Check for specific conflict types
                if apiErr.Code == apiclient.ErrCodeVersionConflict {
                    fmt.Println("Agent was modified by another request")
                }
            case apiErr.IsRateLimited():
                fmt.Println("Rate limited, try again later")
            case apiErr.IsServerError():
                fmt.Printf("Server error: %s\n", apiErr.Message)
            default:
                fmt.Printf("API error: %s\n", apiErr.Message)
            }
        } else {
            // Network or other non-API error
            fmt.Printf("Error: %v\n", err)
        }
        return
    }

    fmt.Printf("Agent: %s\n", agent.Name)
}
```

### 5.2. Handling Runtime Broker Errors

```go
func createAgentWithFallback(ctx context.Context, client hubclient.Client, req *hubclient.CreateAgentRequest) (*hubclient.Agent, error) {
    resp, err := client.Agents().Create(ctx, req)
    if err != nil {
        var apiErr *apiclient.APIError
        if errors.As(err, &apiErr) {
            // Check if it's a "no runtime broker" error
            if apiErr.Code == "no_runtime_broker" || apiErr.Code == "runtime_broker_unavailable" {
                // Extract available brokers from error details
                if brokers, ok := apiErr.Details["availableBrokers"].([]interface{}); ok {
                    fmt.Println("Available brokers:")
                    for _, b := range brokers {
                        if broker, ok := b.(map[string]interface{}); ok {
                            fmt.Printf("  - %s (%s)\n", broker["name"], broker["id"])
                        }
                    }
                }
            }
        }
        return nil, err
    }
    return resp.Agent, nil
}
```

---

## 6. Usage Examples

### 6.1. Hub Client - Creating an Agent

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/ptone/scion-agent/pkg/hubclient"
)

func main() {
    ctx := context.Background()

    // Create client with bearer token
    client, err := hubclient.New(
        "https://hub.scion.dev",
        hubclient.WithBearerToken("your-token"),
        hubclient.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Create an agent
    resp, err := client.Agents().Create(ctx, &hubclient.CreateAgentRequest{
        Name:     "fix-auth-bug",
        GroveID:  "grove-abc123",
        Template: "claude",
        Task:     "Fix the authentication bug in login.go",
        Labels: map[string]string{
            "team": "platform",
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Created agent: %s (status: %s)\n", resp.Agent.Name, resp.Agent.Status)

    // Wait for agent to be running
    for {
        agent, err := client.Agents().Get(ctx, resp.Agent.AgentID)
        if err != nil {
            log.Fatal(err)
        }

        if agent.Status == "running" {
            fmt.Println("Agent is running!")
            break
        }

        if agent.Status == "error" {
            log.Fatalf("Agent failed: %s", agent.RuntimeState)
        }

        time.Sleep(2 * time.Second)
    }
}
```

### 6.2. Hub Client - Registering a Grove

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/ptone/scion-agent/pkg/hubclient"
)

func main() {
    ctx := context.Background()

    client, _ := hubclient.New(
        "https://hub.scion.dev",
        hubclient.WithAPIKey("api-key"),
    )

    // Register a grove
    resp, err := client.Groves().Register(ctx, &hubclient.RegisterGroveRequest{
        Name:      "my-project",
        GitRemote: "git@github.com:myorg/my-project.git",
        Path:      "/Users/dev/projects/my-project/.scion",
        Broker: &hubclient.BrokerInfo{
            Name:    "Dev Laptop",
            Version: "1.2.3",
            Capabilities: &hubclient.BrokerCapabilities{
                WebPTY: true,
                Attach: true,
            },
            SupportedHarnesses: []string{"claude", "gemini"},
        },
        Profiles: []string{"docker", "k8s-dev"},
    })
    if err != nil {
        log.Fatal(err)
    }

    if resp.Created {
        fmt.Println("Created new grove")
    } else {
        fmt.Println("Linked to existing grove")
    }

    fmt.Printf("Grove ID: %s\n", resp.Grove.ID)
    fmt.Printf("Broker Token: %s\n", resp.BrokerToken)

    // Store the broker token for future use
    // ...
}
```

### 6.3. Runtime Broker Client - Hub Dispatching to Broker

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/ptone/scion-agent/pkg/brokerclient"
    "github.com/ptone/scion-agent/pkg/runtimebroker"
)

// This example shows how the Hub would dispatch an agent creation
// to a Runtime Broker in Direct HTTP mode.
func dispatchAgentCreate(ctx context.Context, brokerEndpoint, brokerToken string, req *runtimebroker.CreateAgentRequest) error {
    client, err := brokerclient.New(
        brokerEndpoint,
        brokerclient.WithBearerToken(brokerToken),
    )
    if err != nil {
        return err
    }

    resp, err := client.Agents().Create(ctx, req)
    if err != nil {
        return fmt.Errorf("failed to create agent on broker: %w", err)
    }

    fmt.Printf("Agent created: %s (container: %s)\n",
        resp.Agent.AgentID,
        resp.Agent.Runtime.ContainerID)

    return nil
}
```

### 6.4. Runtime Broker Client - PTY Attachment

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/ptone/scion-agent/pkg/brokerclient"
    "golang.org/x/term"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    client, err := brokerclient.New(
        "https://broker.scion.dev:9800",
        brokerclient.WithBearerToken("broker-token"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Get terminal size
    cols, rows, _ := term.GetSize(int(os.Stdin.Fd()))

    // Attach to agent PTY
    session, err := client.Agents().Attach(ctx, "my-agent", &brokerclient.AttachOptions{
        Cols: cols,
        Rows: rows,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer session.Close()

    // Put terminal in raw mode
    oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
    if err != nil {
        log.Fatal(err)
    }
    defer term.Restore(int(os.Stdin.Fd()), oldState)

    // Handle window resize
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGWINCH)
    go func() {
        for range sigCh {
            cols, rows, _ := term.GetSize(int(os.Stdin.Fd()))
            session.Resize(cols, rows)
        }
    }()

    // Stream stdin/stdout
    err = session.Stream(ctx, os.Stdin, os.Stdout)
    if err != nil && err != io.EOF {
        fmt.Fprintf(os.Stderr, "\nConnection error: %v\n", err)
    }
}
```

### 6.5. Pagination

```go
package main

import (
    "context"
    "fmt"

    "github.com/ptone/scion-agent/pkg/apiclient"
    "github.com/ptone/scion-agent/pkg/hubclient"
)

func listAllAgents(ctx context.Context, client hubclient.Client, groveID string) ([]hubclient.Agent, error) {
    var allAgents []hubclient.Agent

    opts := &hubclient.ListAgentsOptions{
        GroveID: groveID,
        Status:  "running",
        Page: apiclient.PageOptions{
            Limit: 50,
        },
    }

    for {
        resp, err := client.Agents().List(ctx, opts)
        if err != nil {
            return nil, err
        }

        allAgents = append(allAgents, resp.Agents...)

        if !resp.Page.HasMore() {
            break
        }

        opts.Page.Cursor = resp.Page.NextCursor
    }

    return allAgents, nil
}
```

---

## 7. Testing Strategy

### 7.1. Interface-Based Mocking

The interface-first design enables easy mocking for tests:

```go
package myapp

import (
    "context"
    "testing"

    "github.com/ptone/scion-agent/pkg/hubclient"
)

// MockAgentService implements hubclient.AgentService for testing.
type MockAgentService struct {
    CreateFunc func(ctx context.Context, req *hubclient.CreateAgentRequest) (*hubclient.CreateAgentResponse, error)
    GetFunc    func(ctx context.Context, agentID string) (*hubclient.Agent, error)
    // ... other methods
}

func (m *MockAgentService) Create(ctx context.Context, req *hubclient.CreateAgentRequest) (*hubclient.CreateAgentResponse, error) {
    return m.CreateFunc(ctx, req)
}

func (m *MockAgentService) Get(ctx context.Context, agentID string) (*hubclient.Agent, error) {
    return m.GetFunc(ctx, agentID)
}

// ... implement other methods

func TestMyFunction(t *testing.T) {
    mock := &MockAgentService{
        GetFunc: func(ctx context.Context, agentID string) (*hubclient.Agent, error) {
            return &hubclient.Agent{
                AgentID: agentID,
                Name:    "Test Agent",
                Status:  "running",
            }, nil
        },
    }

    // Use mock in tests
    _ = mock
}
```

### 7.2. HTTP Test Server

For integration-style tests:

```go
package hubclient_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/ptone/scion-agent/pkg/hubclient"
)

func TestGetAgent(t *testing.T) {
    // Create test server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/api/v1/agents/test-agent" && r.Method == "GET" {
            w.Header().Set("Content-Type", "application/json")
            json.NewEncoder(w).Encode(map[string]interface{}{
                "agentId": "test-agent",
                "name":    "Test Agent",
                "status":  "running",
            })
            return
        }
        w.WriteHeader(http.StatusNotFound)
    }))
    defer server.Close()

    // Create client pointing to test server
    client, err := hubclient.New(server.URL)
    if err != nil {
        t.Fatal(err)
    }

    // Test
    agent, err := client.Agents().Get(t.Context(), "test-agent")
    if err != nil {
        t.Fatal(err)
    }

    if agent.Name != "Test Agent" {
        t.Errorf("expected name 'Test Agent', got '%s'", agent.Name)
    }
}
```

---

## 8. Implementation Plan

### Phase 1: Shared Infrastructure
1. Implement `pkg/apiclient` package with transport, auth, and error types
2. Add unit tests for transport and error handling

### Phase 2: Hub Client
1. Implement `pkg/hubclient` with all service interfaces
2. Start with Agent and Grove services (most critical for CLI)
3. Add RuntimeBroker, Template, and User services
4. Add comprehensive tests

### Phase 3: Runtime Broker Client
1. Implement `pkg/brokerclient` with agent operations
2. Implement WebSocket-based PTY attachment
3. Add tests

### Phase 4: Integration
1. Integrate Hub client into CLI for hosted mode
2. Integrate Runtime Broker client into Hub for Direct HTTP dispatch
3. End-to-end testing

---

## 9. References

- **Hub API Specification:** `.design/hosted/hub-api.md`
- **Runtime Broker API Specification:** `.design/hosted/runtime-broker-api.md`
- **Architecture Overview:** `.design/hosted/hosted-architecture.md`
- **Server Implementation:** `.design/hosted/server-implementation-design.md`
