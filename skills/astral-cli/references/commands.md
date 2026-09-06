# Astral CLI Target Command Reference

> Draft target contract. Confirm against the installed CLI before execution.

## Auth

```text
astral auth login
astral auth status
astral auth logout
```

## Workspace

```text
astral workspace list
astral workspace create <name>
astral workspace use <workspace>
astral workspace status
astral workspace init [path]
astral workspace pull
astral workspace push
astral workspace sync
```

## TODO / Task

```text
astral todo list
astral todo add <title>
astral todo show <id>
astral todo claim <id>
astral todo start <id>
astral todo block <id> --reason <text>
astral todo review <id>
astral todo done <id> --summary <text>
astral todo release <id>
```

A Task Claim is exclusive and lease-based. If claim fails because another Actor holds the Task, do not override unless the user has explicitly requested an authorized force operation.

## Presence

```text
astral status show
astral status set idle
astral status set planning --note <text>
astral status set working --task <id> --note <text>
astral status set blocked --task <id> --note <text>
astral status watch
```

Presence does not prove Task ownership. Check the Task/lease separately.

## Messaging

```text
astral msg send <actor> <message>
astral msg send --workspace <message>
astral msg send --task <id> <message>
astral msg inbox
astral msg tail
```

Use concise messages. Never send credentials, API keys, private keys, or passwords.

## Documents

```text
astral document list
astral document get <path>
astral document put <path>
astral document diff <path>
astral document conflicts
astral document resolve <conflict-id>
```

Most workflows should use `astral workspace sync`; document commands are for precise control or diagnostics.

## Events

```text
astral event watch
astral event watch --type <types>
astral event watch --json
```

Treat event consumption as idempotent and resume from the last processed event when the CLI supports cursors.

## Agent administration

```text
astral agent list
astral agent create <name>
astral agent credential create <agent-id> --scope <scope>
astral agent credential revoke <credential-id>
```

Credential creation is a sensitive operation; secret material should be shown once and stored in an appropriate secret store.

## Diagnostics

```text
astral config ...
astral completion <shell>
astral doctor
astral version
```

## Exit-code target

```text
0 success
1 generic failure
2 invalid CLI usage
3 authentication/authorization failure
4 not found
5 conflict/precondition failure
6 network/server unavailable
7 timeout
8 local workspace/sync error
9 client/server version unsupported
```

Business detail should come from structured `error.code`, not from inventing more process exit codes.
