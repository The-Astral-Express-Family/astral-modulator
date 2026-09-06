---
name: astral-install
description: Install, verify, upgrade, repair, or uninstall the Astral Modulator `astral` CLI on Windows, macOS, or Linux. Use when a user or Agent needs to set up Astral on a machine, choose between package-manager and direct-release installation, verify checksums/signatures, diagnose PATH/TLS/runtime issues, or determine whether an installation method is actually published. Never invent unpublished package IDs, tap names, buckets, release assets, or versions.
---

# Astral Install

Install the Astral CLI conservatively and verify the result.

## Workflow

1. Determine OS, architecture, shell, and whether the environment is desktop or headless.
2. Determine the requested channel: stable by default; beta/nightly only when explicitly requested.
3. Verify which installation methods are actually published. Do not assume a Homebrew tap, Scoop bucket, WinGet ID, deb/rpm repository, or release asset exists.
4. Prefer a supported package manager when available; otherwise use an official release archive.
5. Verify integrity before placing a direct-download binary on `PATH`.
6. Run `astral version --json` and `astral doctor --json` after installation when the installed CLI supports them.
7. Report the installed version, binary path, server/config state, and any remaining warning.

Read [references/platforms.md](references/platforms.md) for platform-specific order and verification rules.

## Safety Rules

- Never ask the user to paste an access token into chat.
- Never place an Astral token in a repository file.
- Do not disable TLS verification as a routine fix.
- Do not recommend `curl | sh` as the only path. If an installer script is officially published, prefer downloading/inspecting it and ensure it verifies the artifact.
- Never invent a package identifier or download URL. Verify it from official Astral release/package metadata first.
- On headless Linux/CI, use the environment/secret mechanism documented by the project; do not pretend an OS desktop keychain is available.

## Output

Keep installation responses executable and ordered:

1. chosen method;
2. exact commands that are verified to exist;
3. verification commands;
4. upgrade/uninstall commands when requested;
5. fallback only if the preferred method is unavailable.

If Astral has not published binaries for the requested platform, state that clearly and switch to the documented source-build path rather than fabricating an installer.
