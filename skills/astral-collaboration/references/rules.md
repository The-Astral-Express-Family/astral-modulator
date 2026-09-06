# Astral Collaboration Rules

## 1. Starting work

Before work:

- read Task description, dependencies, revision, and lease state;
- read messages/thread related to the Task;
- sync relevant documents;
- claim the Task;
- set Presence.

If claim fails, do not continue substantial duplicate work unless a Human explicitly requests parallel exploration.

## 2. Presence

Use factual states:

- `idle`: available, not actively advancing a Task;
- `planning`: reading/decomposing work;
- `working`: actively changing or producing output;
- `waiting`: waiting for a known external event/dependency;
- `blocked`: cannot proceed without intervention/input;
- `reviewing`: evaluating another Actor's output.

Status notes should say what is happening now, not optimistic completion claims.

## 3. Task ownership

A lease protects coordination, not data access. Continue to respect Workspace scope.

If a lease is near expiry during active work, renew it. If you cannot keep working, release it or mark the Task blocked according to policy.

## 4. Messages

Send a message when it changes another Actor's next action, such as:

- requesting review;
- reporting a blocker;
- announcing a dependency is ready;
- handing off work;
- warning about a conflict;
- responding to a direct request.

Avoid noisy heartbeat-style chat when Presence already conveys the information.

A good message contains:

```text
context -> change/result -> requested next action
```

## 5. Blockers

When blocked:

1. set Task/Presence to blocked if appropriate;
2. state the exact missing dependency or decision;
3. message the responsible Actor/Human when known;
4. avoid inventing a workaround that exceeds scope;
5. release the Task only if Workspace policy says another Actor should take over.

## 6. Handoff

A handoff summary should include:

- what changed;
- current Task state;
- relevant Document paths or commit/PR references;
- checks run and their result;
- unresolved risk/conflict;
- concrete next action.

Do not include private chain-of-thought; provide concise evidence and decisions.

## 7. Document conflicts

On conflict:

- preserve base/local/remote versions;
- inspect both sides;
- merge only when intent is clear;
- otherwise request review or mark blocked;
- never resolve by unconditional overwrite solely because your local version is newer.

## 8. Secrets and permissions

Never place secrets in:

- messages;
- Task descriptions/comments;
- Markdown documents;
- debug logs;
- commit messages.

If a needed action is outside current scope, request the minimum additional capability rather than asking for a broad human credential.

## 9. Human intervention

Human instructions such as pause, revoke, force-release, reassignment, or policy change have priority.

On loss of authorization:

- stop protected writes;
- do not loop retries on authorization failures;
- preserve local unsynced work safely;
- report the state and what remains unsynchronized.

## 10. Parallelism

Parallelize when Tasks are independent or explicitly designed for parallel exploration.

Do not create artificial parallelism by having multiple Agents edit the same Document without a coordination reason.

## 11. Definition of Done

A Task is done when:

- requested output exists;
- relevant validations/tests ran when possible;
- shared state is synchronized or conflict is explicitly recorded;
- result summary is sufficient for Human/Agent review;
- ownership no longer needs to be held.
