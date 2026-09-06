---
name: astral-cli
description: Operate the Astral Modulator collaboration hub through the `astral` CLI for authentication, workspaces, structured TODO/tasks, Agent presence, messaging, Markdown synchronization, events, diagnostics, and Agent credentials. Use when an Agent or user needs concrete Astral CLI commands or machine-readable automation. Verify command support with the installed CLI/help or project reference; do not invent commands that are not implemented.
---

# Astral CLI

Use Astral as a predictable machine interface, not as an interactive shell script.

## Workflow

1. Establish server and workspace context.
2. Check authentication with `astral auth status` when appropriate.
3. Prefer `--json` for Agent/automation use.
4. Read current state before mutation: Task revision, lease, messages, and sync status.
5. Execute the narrowest command that satisfies the request.
6. Inspect structured errors; retry only when the error is explicitly retryable or the operation is idempotent.
7. For long-running Agent work, keep Presence/heartbeat current and read incoming events/messages.

Read [references/commands.md](references/commands.md) for the target command contract.

## Machine-Mode Rules

- Treat stdout as result data and stderr as diagnostics.
- Prefer `--json`; for event streams prefer JSON Lines if supported.
- Never parse human tables when JSON is available.
- Use stable resource IDs rather than display names when ambiguity is possible.
- Do not infer success from exit code alone when structured output contains an error/result state.
- Do not automatically retry revision conflicts, insufficient scope, or an already-claimed Task.
- Never expose tokens in command output, logs, messages, or Markdown.
- Do not use `--yes` to bypass server-side approval or authorization; it only suppresses local confirmation where supported.

## Compatibility

The command reference describes the **target v0.1 contract**, not proof that every command exists in the current binary.

Before using a command against an unknown CLI version:

1. run `astral version --json` when available;
2. inspect `astral help` / subcommand help;
3. use server capability information when exposed;
4. if the command is missing, report the mismatch instead of fabricating behavior.
