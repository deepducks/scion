# Settings Refactor Design

## Motivation

Current configuration management suffers from coupled concerns and "tangled" logic.
- **Deep Nesting**: Previous designs nested runtimes or providers deeply, making it hard to reuse configurations across different environments.
- **Runtime vs Container Intersection**: Specific container images are often needed for specific runtimes (e.g., signed images for prod), but hardcoding them is inflexible.
- **Feature Flags**: Settings like `use_tmux` should be properties of the *environment* (profile), not the runtime definition itself.
- **Duplication**: Defining the same provider multiple times for different runtimes leads to redundancy and maintenance burden.

## Proposed Structure: The "Flat Registry" Model

We propose a "Relational" approach where `Runtimes`, `Providers`, and `Profiles` are top-level, independent entities. A `Profile` acts as the "glue" that binds a specific Runtime to specific Provider overrides.

### JSON Schema Draft

```json
{
  "active_profile": "local-dev",

  "runtimes": {
    "docker-local": { "type": "docker", "host": "unix:///var/run/docker.sock" },
    "k8s-prod": { "type": "kubernetes", "context": "gke_my-project_us-central1_my-cluster" }
  },

  "providers": {
    "gemini": { "image": "gemini-cli:base", "user": "root" },
    "claude": { "image": "claude-code:base", "user": "node" }
  },

  "profiles": {
    "local-dev": {
      "runtime": "docker-local",
      "tmux": true,
      "overrides": {
        "gemini": { "image": "gemini-cli:dev" }
      }
    },
    "k8s-prod": {
      "runtime": "k8s-prod",
      "tmux": false,
      "overrides": {
        "gemini": { "image": "gemini-cli:signed-prod" }
      }
    }
  }
}
```

## Key Concepts

### 1. Flat Registries
`runtimes` and `providers` are top-level maps. They define **what** is available, not **how** it is used in a specific context. This normalization allows defining "Gemini" or "Docker Local" once and referencing them by name.

### 2. Profiles as "Glue"
A `profile` binds a specific runtime to a set of behavior flags (like `tmux`) and provider overrides. It represents a coherent "environment" (e.g., "Local Development", "Production K8s").

### 3. Overrides
Profiles can override specific settings of a provider (like the image tag) without redefining the whole provider. This handles the "intersection" logic cleanly.

## Impact on Codebase

### `Settings` Struct
The Go struct will change to reflect the relational model.

```go
type RuntimeConfig struct {
    Type string `json:"type"`
    // Additional fields (host, context, etc.) are flattened in the JSON
    // and handled via custom unmarshaling or mapstructure.
}

type ProviderConfig struct {
    Image string `json:"image"`
    User  string `json:"user"`
}

type ProviderOverride struct {
    Image string `json:"image,omitempty"`
    User  string `json:"user,omitempty"`
}

type ProfileConfig struct {
    Runtime   string                      `json:"runtime"` // Name of the runtime in "runtimes"
    Tmux      bool                        `json:"tmux"`
    Overrides map[string]ProviderOverride `json:"overrides,omitempty"` // Key is provider name
}

type Settings struct {
    ActiveProfile string                    `json:"active_profile"`
    Runtimes      map[string]RuntimeConfig  `json:"runtimes"`
    Providers     map[string]ProviderConfig `json:"providers"`
    Profiles      map[string]ProfileConfig  `json:"profiles"`
}
```

### Resolution Logic
When starting an agent:
1.  **Determine Active Profile**: Check CLI arg (`--profile`), then `active_profile` in JSON, then default.
2.  **Load Profile**: Look up the profile in `profiles`.
3.  **Resolve Runtime**: Look up the referenced runtime name in `runtimes` to get the base runtime config.
4.  **Resolve Provider**: Look up the requested provider in `providers` to get the base provider config.
5.  **Apply Overrides**: Apply any `overrides` found in the `ProfileConfig` to the `ProviderConfig`.
6.  **Construct RunConfig**: Combine the resolved Runtime, Provider, and Profile settings (like `tmux`).

## Benefits
- **Clean Separation**: Runtimes and Providers are independent. Adding a new runtime doesn't require touching provider configs.
- **Normalization**: "Gemini" is defined once. "K8s Prod" is defined once. They are mixed and matched via Profiles.
- **Flexibility**: Profiles allow "patching" logic (overrides) without deep nesting or duplication.
- **Clarity**: It is easy to see the "base" state of a provider and exactly how it changes per profile.
