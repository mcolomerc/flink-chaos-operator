# Release Process

## Prerequisites

### GitHub Repository Secrets

Before the release workflow can publish images and create releases, two secrets must be configured in the GitHub repository:

**Settings → Secrets and variables → Actions → New repository secret**

| Secret name | Value |
|-------------|-------|
| `DOCKERHUB_USERNAME` | `mcolomervv` |
| `DOCKERHUB_TOKEN` | A Docker Hub **Access Token** (not your password) |

**Creating a Docker Hub Access Token:**

1. Log in to [hub.docker.com](https://hub.docker.com)
2. Account Settings → Security → **New Access Token**
3. Description: `flink-chaos-operator-ci`
4. Permissions: **Read & Write**
5. Copy the token and paste it as `DOCKERHUB_TOKEN` in GitHub

---

## Triggering a Release

Releases are triggered by pushing a semver tag:

```bash
# Tag the commit
git tag v0.1.0

# Push the tag — this starts the release workflow
git push origin v0.1.0
```

For a pre-release (alpha/beta/rc):

```bash
git tag v0.2.0-beta.1
git push origin v0.2.0-beta.1
```

Tags containing `-` are automatically marked as pre-releases in GitHub.

---

## What the Workflow Does

```
push tag v*.*.*
    │
    ├── test          Run unit tests (go test -race)
    │
    ├── docker        Build linux/amd64 + linux/arm64 image
    │                 Push to mcolomervv/flink-chaos-operator:{version,major.minor,latest}
    │
    ├── cli           Build kubectl-fchaos for 5 platforms
    │                 linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64
    │
    ├── helm          Package Helm chart with updated appVersion
    │
    └── release       Create GitHub Release with:
                      - Changelog (commits since previous tag)
                      - All CLI binaries as downloadable assets
                      - Helm chart tarball as downloadable asset
                      - Install instructions in release body
```

All build jobs run in parallel after `test` passes.

---

## Published Artifacts

After a successful release of `v0.1.0`:

| Artifact | Location |
|----------|----------|
| Operator image | `mcolomervv/flink-chaos-operator:0.1.0` |
| Operator image (minor) | `mcolomervv/flink-chaos-operator:0.1` |
| Operator image (latest) | `mcolomervv/flink-chaos-operator:latest` |
| CLI binary (Linux amd64) | GitHub Release assets |
| CLI binary (Linux arm64) | GitHub Release assets |
| CLI binary (macOS amd64) | GitHub Release assets |
| CLI binary (macOS arm64) | GitHub Release assets |
| CLI binary (Windows amd64) | GitHub Release assets |
| Helm chart tarball | GitHub Release assets |

---

## CI on Pull Requests

Every pull request and push to `main` runs the CI workflow:

- `go mod verify` — ensure module checksums are intact
- `go test -race` — unit tests with race detector
- Build check for controller and CLI
- Dockerfile lint (hadolint)
