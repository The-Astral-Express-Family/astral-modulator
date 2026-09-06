# Platform Installation Reference

## Global preference

1. Verified official package manager entry.
2. Verified official release archive.
3. Source build as a development fallback.

Always verify what is currently published before giving concrete package IDs or URLs.

## Windows

Preferred long-term order:

1. Scoop bucket, when officially published.
2. WinGet package, when officially published.
3. Signed ZIP release asset.
4. MSI/MSIX only if the project later publishes it.

For a ZIP install:

- select the correct architecture;
- verify SHA-256 and signature when available;
- extract `astral.exe` to a user-controlled install directory;
- add that directory to PATH;
- run `astral version --json` and `astral doctor --json`.

Do not assume Visual C++ Runtime availability; follow the release notes for the chosen artifact.

## macOS

Preferred long-term order:

1. Official Homebrew tap/formula.
2. Signed/notarized tar.gz release asset.

Choose arm64 on Apple Silicon and x86_64 on Intel unless the project publishes and recommends universal2.

After a direct install, verify code signing/notarization according to project release instructions and run `astral doctor --json`.

## Linux

Preferred long-term order:

1. Official Homebrew/Linuxbrew formula if published and suitable.
2. Native deb/rpm repository if published for the distribution.
3. GNU/glibc tar.gz asset compatible with the host baseline.
4. musl static asset when explicitly supported and appropriate.

Do not assume a glibc build works on every distribution. If compatibility is unclear, inspect release requirements or use the supported source-build workflow.

## Source build

Use only when prebuilt distribution is unavailable or the user is developing Astral.

Expected project toolchain direction:

- C++20 compiler;
- CMake;
- vcpkg manifest mode;
- platform build preset.

Follow the repository's actual README/CMakePresets rather than inventing preset names.

## Verification

When available:

```text
astral version --json
astral doctor --json
```

Verify at minimum:

- binary starts;
- expected architecture/version;
- server configuration is explicit;
- TLS/DNS works when server is configured;
- credential store availability is understood;
- current workspace is not accidentally inherited from another project.
