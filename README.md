# k8s-ctx-dumper

[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![CI Build Passing](https://github.com/RAVEYUS/k8-ctx-dumper/actions/workflows/ci.yml/badge.svg)](https://github.com/RAVEYUS/k8-ctx-dumper/actions/workflows/ci.yml)
[![CodeRabbit Reviews](https://img.shields.io/badge/CodeRabbit-AI%20Reviews-800080?logo=data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSI2NCIgaGVpZ2h0PSI2NCIgdmlld0JveD0iMCAwIDY0IDY0Ij48cGF0aCBmaWxsPSIjZmZmIiBkPSJNMzIgMkMyIDE2IDMgMzkgMjIgNTRjMTkgMTUgNDAgLTggNDAgLTI4QzYyIDggNDYgMiAzMiAyeiIvPjwvc3ZnPg==)](https://www.coderabbit.ai/)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/RAVEYUS/k8-ctx-dumper/badge)](https://securityscorecards.dev/viewer/?uri=github.com/RAVEYUS/k8-ctx-dumper)

A lightweight, high-performance Go CLI that queries Kubernetes clusters via
client-go, extracts active cluster state (Pods, Services, Deployments, Events),
strips API noise (`managedFields`, timestamps, low-value annotations), and
formats the result into **token-optimized Markdown or JSON** suitable for LLM
context windows.

## Features

- Concurrent resource fetching via goroutines for low-latency dumps
- Sanitization engine that removes `managedFields`, `uid`, `resourceVersion`,
  `generation`, `selfLink`, owner references, and low-value annotations
- Token-efficient Markdown tables or compact single-line JSON output
- Out-of-cluster (kubeconfig) and in-cluster (ServiceAccount) client support
- Optional system-clipboard copy and file output
- Events from `events.k8s.io/v1` with automatic fallback to `core/v1`

## Installation

```bash
go install k8s-ctx-dumper@latest
# or build from source:
go build -o k8s-ctx-dumper .
```

### Docker

Pre-built multi-arch images (linux/amd64, linux/arm64) are published to
[Docker Hub](https://hub.docker.com/) on every version tag. Mount your
kubeconfig read-only to use it:

```bash
docker run --rm -v ~/.kube:/root/.kube:ro \
  raveyus/k8s-ctx-dumper:latest dump -n default
```

Or with docker compose:

```bash
docker compose up --build
```

> **Note:** the image runs as a non-root user and has no shell; `--copy`
> (clipboard) is unavailable inside a container.

Build metadata can be embedded at build time:

```bash
go build -ldflags "-X k8s-ctx-dumper/cmd.version=v1.0.0 \
  -X k8s-ctx-dumper/cmd.commit=$(git rev-parse --short HEAD) \
  -X k8s-ctx-dumper/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" .
```

## Usage

```bash
# Dump pods, services, and deployments in the default namespace
k8s-ctx-dumper dump

# Dump everything across all namespaces
k8s-ctx-dumper dump -n all --resources pods,services,deployments,events

# Compact JSON, copied straight to the clipboard
k8s-ctx-dumper dump --format json --copy

# Target a specific kubeconfig context
k8s-ctx-dumper dump --kubeconfig ~/.kube/other --context prod
```

### Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--kubeconfig` | `-k` | `$KUBECONFIG` or `~/.kube/config` | Path to a kubeconfig file |
| `--namespace` | `-n` | `default` | Namespace to dump; `all` for all namespaces |
| `--context` | | current context | Kubeconfig context to use |
| `--resources` | `-r` | `pods,services,deployments` | Comma-separated resources to dump |
| `--format` | `-f` | `markdown` | Output format: `markdown` or `json` |
| `--copy` | `-c` | `false` | Copy output to the system clipboard |
| `--output` | `-o` | | Also write output to a file |

## Example output

```markdown
# Cluster Snapshot

- **Context:** `prod-cluster`
- **Scope:** `default`

## Pods (2)

| Name | Status | Restarts | IP | Node | Age | Containers |
| --- | --- | --- | --- | --- | --- | --- |
| web-7f8d | Running | 0 | 10.244.0.5 | node-1 | 2d | web:nginx:1.25 |
| pay-98ab | CrashLoopBackOff | 12 | 10.244.0.8 | node-2 | 4h | pay:2.1.0 |

## Deployments (2)

| Name | Ready | Up-to-date | Available | Strategy | Age |
| --- | --- | --- | --- | --- | --- |
| auth-service | 1/1 | 1 | 1 | RollingUpdate | 2d |

## Recent Events / Errors

- [Warning] BackOff (12x, 10m ago): Back-off restarting failed container in pod payment-api-98ab
```

## Project layout

```
k8s-ctx-dumper/
├── cmd/
│   ├── root.go        # Global flags (--kubeconfig, --namespace, --context)
│   ├── dump.go        # Main subcommand ('dump') and flags
│   └── version.go     # Build/version information
├── pkg/
│   ├── k8s/           # K8s client initialization & concurrent fetching
│   ├── sanitizer/     # Strips managedFields, metadata noise & empty fields
│   └── formatter/     # Markdown / JSON output formatters
├── go.mod
├── go.sum
└── main.go            # Entrypoint executing cmd.Execute()
```

## License

Apache License 2.0

## Development & CI

- **AI reviews**: [CodeRabbit](https://www.coderabbit.ai/) (free tier) reviews
  every pull request with inline comments, a summary, and Go tooling
  (golangci-lint, gitleaks, actionlint, zizmor).
- CI runs on every push/PR: `gofmt`, `go vet`, `go test -race`, and a
  version-stamped build on Go 1.26.
- [OpenSSF Scorecard](https://securityscorecards.dev/) runs weekly to assess
  supply-chain security; results are published to the security dashboard.
- Tag a release with `git tag v1.0.0 && git push origin v1.0.0` to trigger the
  release workflow, which cross-compiles the binary for Linux, macOS, and
  Windows (amd64/arm64), attaches them to a draft GitHub Release, **and
  publishes a multi-arch Docker image to Docker Hub**.
- Docker Hub publishing requires the `DOCKERHUB_USERNAME` and
  `DOCKERHUB_TOKEN` repository secrets to be configured in
  [repo settings](https://github.com/RAVEYUS/k8-ctx-dumper/settings/secrets/actions).
- Dependabot keeps Go modules and GitHub Actions up to date weekly.
