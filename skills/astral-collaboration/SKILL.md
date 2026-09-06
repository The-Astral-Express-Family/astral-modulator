---
name: astral-collaboration
description: Follow Astral Modulator's multi-Agent collaboration protocol when several LLM Agents and humans share tasks, Markdown workspaces, live status, and messages. Use when deciding how to claim work, coordinate ownership, heartbeat, report blockers, hand off tasks, resolve document conflicts, communicate with peers, or respond to human pause/revoke/override instructions. This skill defines behavior, not installation or CLI syntax.
---

# Astral Collaboration

Coordinate through explicit state. Do not infer ownership from conversation alone.

## Standard Work Loop

1. Read the current Workspace state, relevant messages, and Task details.
2. Synchronize relevant managed documents before editing.
3. Claim exactly the Task you intend to work on.
4. Confirm the claim/lease succeeded before doing significant work.
5. Set Presence to the factual current state and link the current Task.
6. Perform the work within granted scopes.
7. During long work, renew heartbeat/lease and read new messages/events.
8. Before publishing results, sync again and resolve any document conflict explicitly.
9. Finish as `done`, `review`, or `blocked`; include a concise result/blocker summary.
10. Release ownership when work is no longer active and set Presence appropriately.

Read [references/rules.md](references/rules.md) for conflict, messaging, security, and handoff rules.

## Non-Negotiable Rules

- Claim before work when Task ownership is available.
- Never treat Presence as ownership; a valid Task Lease is ownership.
- Never steal/force-release another Actor's Task without explicit authorization.
- Never silently overwrite a Document conflict.
- Never send or store credentials in Task text, messages, or Markdown.
- Report blockers as blockers; do not keep a Task indefinitely `working` when progress cannot continue.
- Human pause/revoke/override takes precedence over Agent coordination.
- Do not claim more Tasks than you can actively advance unless the Workspace policy explicitly allows batching.
- Keep status notes factual and concise.

## Completion Standard

Mark work complete only when the requested artifact/change exists, relevant checks have run where applicable, managed documents are synchronized or conflicts reported, and the next Actor can understand the outcome from the Task summary without reconstructing hidden reasoning.
