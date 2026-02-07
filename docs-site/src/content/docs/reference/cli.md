---
title: Scion CLI Reference
---

## `scion start` (or `run`)

Starts a new agent or resumes an existing one.

**Usage:** `scion start <agent-name> [task] [flags]`

- **Arguments:**
    - `<agent-name>`: Unique name for the agent instance.
    - `[task]`: (Optional) The initial instruction/task for the agent.
- **Flags:**
    - `-t, --type <string>`: Template to use (default "gemini").
    - `-i, --image <string>`: Override container image.
    - `-a, --attach`: Attach to the agent immediately after starting.
    - `--no-auth`: Disable authentication propagation.
    - `-d, --detached`: Run in detached mode (default true).

## `scion stop`

Stops a running agent.

**Usage:** `scion stop <agent-name>`

## `scion resume`

Resumes a stopped agent.

**Usage:** `scion resume <agent-name> [flags]`

- **Flags:**
    - `-a, --attach`: Attach to the agent immediately.

## `scion attach`

Connects to the interactive session of a running agent.

**Usage:** `scion attach <agent-name>`

- **Key Bindings:**
    - `Ctrl+P, Ctrl+Q`: Detach from the session without stopping the agent.

## `scion message` (or `msg`)

Sends a message to a running agent's harness by enqueuing it into its input stream (requires Tmux).

**Usage:** `scion message [agent] <message> [flags]`

- **Arguments:**
    - `[agent]`: The name of the agent (optional if `--broadcast` is used).
    - `<message>`: The text to send to the agent.
- **Flags:**
    - `-i, --interrupt`: Interrupt the harness before sending the message.
    - `-b, --broadcast`: Send the message to all running agents in the current grove.
    - `-a, --all`: Send the message to all running agents across all groves.

## `scion logs`

Displays the logs of an agent.

**Usage:** `scion logs <agent-name> [flags]`

- **Flags:**
    - `-f, --follow`: Stream logs.

## `scion list` (or `ps`)

Lists all agents and their status.

**Usage:** `scion list [flags]`

- **Flags:**
    - `-a, --all`: Show all agents (including stopped ones).

## `scion delete` (or `rm`)

Deletes an agent, removing its container, home directory, and worktree.

**Usage:** `scion delete <agent-name> [flags]`

- **Flags:**
    - `-b, --preserve-branch`: Preserve the git branch associated with the worktree (default: deleted).
    - `--stopped`: Delete all agents with stopped containers.

## `scion grove`

Manages the Scion workspace (Grove).

- `scion grove init`: Initialize a new grove. By default, creates a `.scion` directory in the current directory or the root of the current git repository.
    - Flags: `--global` (Initialize the global grove in the home directory)
    - **Note:** If you are in a git repository, add `.scion/agents` to your `.gitignore` to avoid issues with nested git worktrees: `echo ".scion/agents" >> .gitignore`
    - **Hub Integration:** If a Hub endpoint is configured, `init` will prompt to register the new grove with the Hub.

## `scion templates`

Manages agent templates.

- `list`: List available templates.
- `show <name>`: Show configuration of a template.
- `create <name> [--harness <type>]`: Create a new template.
- `clone <src> <dest>`: Clone a template.
- `delete <name>` (alias `rm`): Delete a template. Checks both local and Hub for the template and prompts for confirmation before deleting.
    - If the template exists **locally only**, prompts `Delete local template '<name>'? (Y/n)`.
    - If the template exists **on the Hub only**, prompts `Delete remote template '<name>'? (Y/n)`.
    - If the template exists **both locally and on the Hub**, presents a choice: `[L]` local, `[R]` remote, `[B]` both, `[C]` cancel.
    - Use `--yes` / `-y` to skip confirmation (deletes both when template exists in both locations).
    - Use `--no-hub` to skip the Hub check and treat as local-only.
- `update-default`: Update default templates from the binary.

## `scion hub`

Manages connection to and interaction with a Scion Hub.

- `scion hub status`: Show the current Hub connection status and authentication details.
    - Flags: `--json` (Output in JSON format)
- `scion hub auth login`: Authenticate against the Hub (opens a browser).
- `scion hub register [grove-path]`: Register the current broker and grove with the Hub.
- `scion hub enable`: Enable Hub integration for agent operations.
- `scion hub disable`: Disable Hub integration, falling back to local-only mode.
- `scion hub groves`: List all groves registered on the Hub.
- `scion hub brokers`: List all runtime brokers registered on the Hub.
- `scion hub secret`: Manage secrets on the Hub.
    - `set <key>=<value>`: Set a secret for the current grove.
    - `list`: List secrets for the current grove.
    - `remove <key>`: Remove a secret.
