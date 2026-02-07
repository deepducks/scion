---
title: Harness Development
description: How to add support for new LLM tools to Scion.
---

A harness is the bridge between the Scion orchestrator and a specific LLM-based tool (like Claude Code or Gemini CLI).

## Communication with Sciontool

Harnesses running inside the agent container interact with the orchestrator primarily through the `sciontool` utility.

### Reporting Agent Status

The `sciontool status` command should be used by the harness to signal state changes. This ensures that the Hub and Web Dashboard have accurate information about what the agent is doing.

#### Waiting for User Input
When the harness requires human intervention, it should call:
```bash
sciontool status ask_user "I need clarification on the requirements."
```
This updates the agent's state to `WAITING_FOR_INPUT` and logs the message.

#### Task Completion
When the harness has finished its work (successfully or with an error), it should call:
```bash
sciontool status task_completed "Implemented the requested feature."
```
This updates the state to `COMPLETED` and triggers final telemetry collection.

### Unified Logging

Harnesses should ideally write their logs to `/home/scion/agent.log`. Using `sciontool` for logging ensures that logs are structured correctly and forwarded to the cloud observability backend if configured.

```bash
# Example of using sciontool for logging from a shell script
sciontool --log-level debug some_command
```

## Lifecycle Hooks

Harnesses can implement lifecycle hooks to perform setup or cleanup:

- `pre-start`: Run before the agent process starts.
- `post-start`: Run after the agent process has started.
- `pre-stop`: Run before the agent is stopped.
- `session-end`: Run after the agent session finishes.

These are configured in the `scion-agent.json` template.
