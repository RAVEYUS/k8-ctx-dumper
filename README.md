# k8s-ctx-dumper

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
