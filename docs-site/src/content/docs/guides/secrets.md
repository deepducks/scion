---
title: Secret & Environment Management
description: Managing environment variables and secrets via the Scion Hub.
---

Scion's hosted architecture provides a centralized way to manage configuration and sensitive data across your team. Instead of sharing `.env` files or hardcoding credentials, you can use the Scion Hub to store and inject environment variables and secrets into your agents.

## Variables vs. Secrets

Scion distinguishes between regular environment variables and secure secrets:

| Feature | Environment Variables (`env`) | Secrets (`secret`) |
| :--- | :--- | :--- |
| **Visibility** | Read/Write (via API and CLI) | Write-only (cannot be read back) |
| **Storage** | Plaintext in database | Encrypted at rest / Externally stored |
| **Use Case** | API URLs, log levels, feature flags | API keys, passwords, private keys |
| **Injection** | Environment variables only | Environment, files, or JSON variables |

---

## Scoping

Both variables and secrets can be scoped to different levels. Scion resolves these hierarchically when an agent starts:

1.  **User Scope**: Personal secrets for a specific user. Applied to all agents owned by that user.
2.  **Grove Scope**: Project-level secrets. Available to all agents running in a specific Grove.
3.  **Broker Scope**: Infrastructure-level secrets. Available only to agents running on a specific Runtime Broker (e.g., for hardware-specific config).

**Resolution Priority:** When multiple scopes define the same secret key, the more specific scope wins. Broker scope has the highest priority, followed by Grove, then User, then Hub. Template `env` blocks and CLI `--env` flags are layered on top of resolved secrets.

---

## Managing Environment Variables

Use the `scion hub env` command suite to manage non-sensitive configuration.

### Setting Variables
```bash
# Set a user-scoped variable
scion hub env set API_URL=https://api.example.com

# Set a grove-scoped variable (inferred from current directory)
scion hub env set --grove LOG_LEVEL=debug

# Set a variable only for a specific broker
scion hub env set --broker=my-gpu-node CUDA_VISIBLE_DEVICES=0
```

### Injection Modes
- **As Needed (Default)**: The variable is only injected if it is explicitly requested in the agent's template (`scion-agent.yaml`).
- **Always**: The variable is injected into *every* agent started within that scope.
  ```bash
  scion hub env set --always GLOBAL_TRACE_ID=scion-production
  ```

---

## Managing Secrets

Secrets are write-only. Once set, their values cannot be retrieved via the CLI or API; they are only decrypted and injected into the agent container at runtime.

### Setting Secrets
```bash
# Set a user-scoped secret
scion hub secret set ANTHROPIC_API_KEY sk-ant-api01-...

# Set a grove-scoped secret
scion hub secret set --grove DB_PASSWORD my-secure-password
```

### Secret Types
Secrets can be projected into the agent container in three ways:

1.  **Environment** (Default): Injected as a standard environment variable.
2.  **File**: Written to a specific path on the agent's filesystem.
3.  **Variable**: Added to a JSON file at `~/.scion/secrets.json` for programmatic access by the harness.

### Mounting Files as Secrets
You can use the `@` prefix to read a secret's value from a local file. This is particularly useful for SSH keys or service account JSONs.

```bash
# Upload an SSH private key and mount it to the standard location in the agent
scion hub secret set --type file --target ~/.ssh/id_rsa SSH_KEY @~/.ssh/id_rsa
```

---

## Administrator Configuration (Hub)

To use secrets in production, the Hub must be configured with a production-grade secrets backend.

### Secrets Backend (Required)

Scion requires a secrets backend to store secret values. The recommended backend is **GCP Secret Manager**.

:::caution[No Plaintext Storage]
The Hub does not store secret values in its database. Attempting to create or update secrets without a configured backend (e.g., using the default `local` backend) will return an error. You must configure GCP Secret Manager to use secret management features.
:::

#### Configuring GCP Secret Manager

Set the backend in your `settings.yaml`:

```yaml
server:
  secrets:
    backend: gcpsm
    gcp_project_id: "my-gcp-project"
    gcp_credentials: "/path/to/service-account.json"  # Optional if using ADC
```

Or via environment variables:

```bash
export SCION_SERVER_SECRETS_BACKEND=gcpsm
export SCION_SERVER_SECRETS_GCP_PROJECT_ID=my-gcp-project
export SCION_SERVER_SECRETS_GCP_CREDENTIALS=/path/to/service-account.json
```

When GCP Secret Manager is configured, Scion uses a **hybrid storage** model:
- **Metadata** (name, type, scope) is stored in the Hub database.
- **Secret values** are stored in GCP Secret Manager with automatic versioning.

---

## Technical Details

### Resolution Hierarchy
When an agent starts, the Runtime Broker requests a "Resolved Environment" from the Hub. The Hub merges secret values in this order (last one wins for the same key):
1. Hub Secrets (global defaults)
2. User Secrets
3. Grove Secrets
4. Broker Secrets
5. Template `env` block
6. CLI `--env` flags

### Security
Secrets are transmitted over TLS between the Hub and Runtime Brokers. They are only decrypted by the Hub during the dispatch process and sent over an encrypted channel to the Runtime Broker. The Broker then injects them directly into the container's memory space. Brokers never persist agent secrets to disk.

For a detailed overview of the security architecture, see the [Security Architecture Reference](/reference/security).
