# EXPLANATION.md — k8s-ctx-dumper Codebase Documentation


---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [Repository Layout](#2-repository-layout)
3. [Dependency Graph](#3-dependency-graph)
4. [Module: `main.go`](#4-module-maingo)
5. [Package: `cmd/`](#5-package-cmd)
   - [5.1 `cmd/root.go`](#51-cmdrootgo)
   - [5.2 `cmd/dump.go`](#51-cmddumpgo)
   - [5.3 `cmd/version.go`](#53-cmdversiongo)
6. [Package: `pkg/k8s/`](#6-package-pkgk8s)
   - [6.1 `pkg/k8s/client.go`](#61-pkgk8sclientgo)
   - [6.2 `pkg/k8s/fetcher.go`](#62-pkgk8sfetchergo)
7. [Package: `pkg/sanitizer/`](#7-package-pkgsanitizer)
   - [7.1 `pkg/sanitizer/sanitizer.go`](#71-pkgsanitizersanitizergo)
8. [Package: `pkg/formatter/`](#8-package-pkgformatter)
   - [8.1 `pkg/formatter/interface.go`](#81-pkgformatterinterfacego)
   - [8.2 `pkg/formatter/markdown.go`](#82-pkgformattermarkdowngo)
   - [8.3 `pkg/formatter/json.go`](#83-pkgformatterjsongo)
9. [Tests & Fuzzing](#9-tests--fuzzing)
10. [CI/CD Workflows](#10-cicd-workflows)
11. [Containerization](#11-containerization)
12. [Security & Supply Chain](#12-security--supply-chain)
13. [Configuration Files](#13-configuration-files)
14. [Data Flow: End-to-End](#14-data-flow-end-to-end)
15. [Design Decisions](#15-design-decisions)

---

## 1. Project Overview

**k8s-ctx-dumper** is a lightweight Go CLI that queries a Kubernetes cluster via
`client-go`, strips API noise (`managedFields`, `uid`, `resourceVersion`,
low-value annotations), and renders the active cluster state as **token-optimized
Markdown or JSON** — purpose-built for LLM context windows.

### Key characteristics

| Aspect | Value |
| --- | --- |
| Language | Go 1.26.2 |
| CLI framework | `spf13/cobra` v1.10.2 |
| K8s SDK | `k8s.io/client-go` v0.36.3 |
| Clipboard | `atotto/clipboard` v0.1.4 |
| Output formats | Markdown (tables), JSON (compact, snake_case) |
| Resources | Pods, Services, Deployments, Events |
| Concurrency | `sync.WaitGroup` + goroutines |
| Distribution | GitHub Releases (binaries), Docker Hub (multi-arch image) |
| CI/CD | GitHub Actions (4 workflows) |
| Security | OpenSSF Scorecard, CodeQL, CodeRabbit, govulncheck, fuzzing |

---

## 2. Repository Layout

```
k8s-ctx-dumper/
├── main.go                          # Entrypoint → cmd.Execute()
├── go.mod / go.sum                  # Go module definition & checksums
├── cmd/
│   ├── root.go                      # Root command + persistent flags
│   ├── dump.go                      # 'dump' subcommand + pipeline orchestration
│   └── version.go                   # 'version' subcommand + build metadata
├── pkg/
│   ├── k8s/
│   │   ├── client.go                # K8s client initialization (kubeconfig → in-cluster)
│   │   ├── fetcher.go               # Concurrent resource fetching → ClusterSnapshot
│   │   ├── fetcher_test.go          # Unit tests for ParseResources
│   │   └── fuzz_test.go             # Fuzz target: FuzzParseResources
│   ├── sanitizer/
│   │   ├── sanitizer.go             # Strips managedFields, metadata noise, annotations
│   │   ├── sanitizer_test.go        # Unit tests for sanitization
│   │   └── fuzz_test.go             # Fuzz target: FuzzSanitize
│   └── formatter/
│       ├── interface.go             # Formatter interface + New() factory
│       ├── markdown.go              # MarkdownFormatter (pipe tables)
│       ├── json.go                  # JSONFormatter (compact snake_case DTOs)
│       ├── markdown_test.go         # Unit tests for both formatters
│       └── fuzz_test.go             # Fuzz target: FuzzMarkdownFormat
├── .github/
│   ├── workflows/
│   │   ├── ci.yml                   # CI: test, fuzz, build (push + PR)
│   │   ├── release.yml               # CD: binaries + Docker Hub (on v* tag)
│   │   ├── codeql.yml                # SAST: CodeQL Go analysis
│   │   └── scorecard.yml             # OpenSSF Scorecard supply-chain assessment
│   └── dependabot.yml               # Weekly dependency updates
├── Dockerfile                       # Multi-stage: golang → distroless
├── docker-compose.yml               # Example Compose service
├── .dockerignore                     # Docker build context exclusions
├── .coderabbit.yaml                 # CodeRabbit AI review config (free tier)
├── .gitignore                        # Git exclusions
├── LICENSE                           # Apache 2.0
├── README.md                         # Project documentation
└── SECURITY.md                       # Security policy
```

---

## 3. Dependency Graph

```
main.go
  └── cmd (package)
        ├── root.go    → defines rootCmd, persistent flags
        ├── dump.go    → imports pkg/k8s, pkg/sanitizer, pkg/formatter
        └── version.go → build metadata

pkg/k8s
  ├── client.go   → k8s.io/client-go (kubernetes, rest, clientcmd)
  └── fetcher.go  → k8s.io/api (apps/v1, core/v1, events/v1), apimachinery

pkg/sanitizer
  └── sanitizer.go → k8s.io/api, apimachinery, pkg/k8s (ClusterSnapshot)

pkg/formatter
  ├── interface.go  → pkg/k8s (ClusterSnapshot)
  ├── markdown.go   → k8s.io/api, apimachinery, pkg/k8s, pkg/sanitizer (Age)
  └── json.go       → encoding/json, k8s.io/api, pkg/k8s, pkg/sanitizer (Age)
```

**Import direction:** `main → cmd → {k8s, sanitizer, formatter}`. The `pkg/`
packages never import `cmd`, and `sanitizer`/`formatter` import `pkg/k8s` only
for the `ClusterSnapshot` type — no circular dependencies.

---

## 4. Module: `main.go`

```go
package main

import (
	"os"
	"k8s-ctx-dumper/cmd"
)

func main() {
	if code := cmd.Execute(); code != 0 {
		os.Exit(code)
	}
}
```

**Purpose:** Thin entrypoint. Delegates entirely to `cmd.Execute()` and exits
with the returned code. No business logic here — it exists so the `cmd` package
stays importable and testable.

**Design note:** `os.Exit()` is only called when the code is non-zero, so a
successful run (`code == 0`) returns normally from `main()` — this ensures
deferred functions in the standard library flush correctly.

---

## 5. Package: `cmd/`

The `cmd` package wires the CLI using Cobra. It defines the root command with
persistent flags and two subcommands: `dump` (the main pipeline) and `version`.

### 5.1 `cmd/root.go`

**Responsibility:** Define the root Cobra command and the three persistent flags
shared by all subcommands.

**Key declarations:**

- `rootCmd *cobra.Command` — the root command with `SilenceUsage: true` and
  `SilenceErrors: true` (so runtime errors don't dump the full help text, and
  `main()` decides how to present errors).
- `kubeconfig`, `namespace`, `kubeContext` — package-level variables bound to
  persistent flags. `kubeContext` is named distinctly from the `context` package
  to avoid a collision (the `context` package is imported by `dump.go`).
- `Execute() int` — runs the root command, prints errors to stderr, returns
  exit code (0 success, 1 any error).

**Flags:**

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--kubeconfig` | `-k` | `""` | Path to kubeconfig (default: `$KUBECONFIG` or `~/.kube/config`) |
| `--namespace` | `-n` | `default` | Namespace to dump; `all` for all namespaces |
| `--context` | | `""` | Kubeconfig context to use |

**Error handling:** `Execute()` wraps `rootCmd.Execute()` and prints
`error: <err>` to stderr on failure, returning `1`. This is the single exit
path — subcommands return errors via `RunE`, not by calling `os.Exit`.

### 5.2 `cmd/dump.go`

**Responsibility:** Define the `dump` subcommand and orchestrate the entire
pipeline: parse flags → build client → fetch → sanitize → format → output.

**Flags:**

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--resources` | `-r` | `pods,services,deployments` | Comma-separated resources |
| `--format` | `-f` | `markdown` | Output format: `markdown` or `json` |
| `--copy` | `-c` | `false` | Copy output to system clipboard |
| `--output` | `-o` | `""` | Also write output to a file |

**`runDump()` walkthrough:**

1. **Parse resources** — `k8s.ParseResources(resourcesFlag)` validates the
   comma-separated list and returns a de-duplicated, stable-ordered slice.
   Unknown names produce an error (fail loud, not silent).
2. **Resolve namespace** — `"all"` is a user-facing alias for all namespaces;
   internally an empty string means `NamespaceAll` to client-go listers.
3. **Context with timeout** — `context.WithTimeout(cmd.Context(), 60s)` bounds
   the whole fetch+format pipeline so a hung cluster fails fast.
4. **Build client** — `k8s.NewClient(kubeconfig, kubeContext)` returns a
   `*kubernetes.Clientset` and `*rest.Config`.
5. **Fetch** — `k8s.NewFetcher(client, restCfg, ns, resources).Fetch(ctx)`
   concurrently queries the cluster and returns a `*k8s.ClusterSnapshot`.
6. **Resolve context name** — `resolvedCurrentContext()` reads the kubeconfig
   to surface the resolved context name (explicit flag or current-context).
   Failure is non-fatal.
7. **Sanitize** — `sanitizer.Sanitize(snapshot)` strips API noise in place.
8. **Format** — `formatter.New(formatFlag)` returns a `Formatter`; `.Format()`
   renders the snapshot to a string.
9. **Output** — write to file (`--output`), stdout, and optionally clipboard
   (`--copy` via `atotto/clipboard`).

**`resolvedCurrentContext()`** uses `clientcmd.NewDefaultClientConfigLoadingRules()`
to load the kubeconfig, honoring the `--kubeconfig` flag if set. It returns the
`--context` flag value if set, otherwise the kubeconfig's `currentContext`.

### 5.3 `cmd/version.go`

**Responsibility:** Define the `version` subcommand and hold build metadata
variables injected via `-ldflags` at build time.

```go
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
```

These are overridden during CI builds:

```bash
go build -ldflags "-X k8s-ctx-dumper/cmd.version=v0.0.1 \
  -X k8s-ctx-dumper/cmd.commit=$(git rev-parse --short HEAD) \
  -X k8s-ctx-dumper/cmd.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

The `version` command prints: `k8s-ctx-dumper <version> (commit <commit>, built <date>)`.

---

## 6. Package: `pkg/k8s/`

This package wraps the Kubernetes `client-go` SDK: it builds a clientset from a
kubeconfig or in-cluster configuration, then concurrently fetches the requested
resource lists into a `ClusterSnapshot`.

### 6.1 `pkg/k8s/client.go`

**Responsibility:** Build a `*kubernetes.Clientset` and `*rest.Config` from a
kubeconfig or in-cluster fallback.

**`NewClient(kubeconfigPath, contextName string)`** resolution order:

1. **Out-of-cluster:** `outOfClusterConfig()` loads a kubeconfig using kubectl
   precedence:
   - Explicit `--kubeconfig` flag path
   - `$KUBECONFIG` environment variable
   - `~/.kube/config` (via `homedir.HomeDir()`)
2. **In-cluster fallback:** If out-of-cluster fails, `rest.InClusterConfig()`
   is attempted — this makes the binary work inside a Pod with a ServiceAccount.
3. **Warning handler:** `restCfg.WarningHandler = rest.NewWarningWriter(os.Stderr, ...)`
   with `Deduplicate: true` surfaces non-fatal API warnings (deprecations) to
   stderr instead of dropping them silently.
4. **Clientset:** `kubernetes.NewForConfig(restCfg)` builds the typed clientset.

**Error messages** are descriptive: if both paths fail, the combined error names
both the out-of-cluster and in-cluster failures and tells the user to set
`--kubeconfig` or `KUBECONFIG`.

**`clientConfig` struct** holds `KubeconfigPath` and `ContextName`. It's
internal (unexported) — the public API is `NewClient(kubeconfigPath, contextName)`.

**`outOfClusterConfig()`** uses `clientcmd.NewNonInteractiveDeferredLoadingClientConfig()`
with `ConfigOverrides` to pin the context when `--context` is set. This mirrors
kubectl's own context resolution.

### 6.2 `pkg/k8s/fetcher.go`

**Responsibility:** Concurrently fetch Pods, Services, Deployments, and Events
into a `ClusterSnapshot`.

#### Types

**`ResourceKind`** — string enum: `pods`, `services`, `deployments`, `events`.

**`ParseResources(raw string)`** — parses a comma-separated flag value into a
de-duplicated, stable-ordered `[]ResourceKind`. Unknown names produce an error.
Empty input returns the default set (`pods,services,deployments`).

**`ClusterSnapshot`** — the sanitizer-ready view of a cluster:

| Field | Type | Purpose |
| --- | --- | --- |
| `Context` | `string` | Kubeconfig context name |
| `Namespace` | `string` | Namespace filter (`""` = all) |
| `Pods` | `[]corev1.Pod` | Raw pod list |
| `Services` | `[]corev1.Service` | Raw service list |
| `Deployments` | `[]appsv1.Deployment` | Raw deployment list |
| `HasPods/HasServices/HasDeployments` | `bool` | Which were requested |
| `Events` | `[]corev1.Event` | Raw event list (normalized from events/v1 or core/v1) |

**`Fetcher`** — holds `client kubernetes.Interface`, `restCfg *rest.Config`,
`namespace string`, `resources []ResourceKind`.

#### `Fetch(ctx)` — the concurrent fetch

1. Creates a `sync.WaitGroup` and a buffered error channel (`errCh`, capacity =
   number of resources).
2. Iterates `f.resources`, spawning a goroutine per resource type:
   - **Pods:** `f.client.CoreV1().Pods(f.namespace).List(ctx, ...)`
   - **Services:** `f.client.CoreV1().Services(f.namespace).List(ctx, ...)`
   - **Deployments:** `f.client.AppsV1().Deployments(f.namespace).List(ctx, ...)`
   - **Events:** `f.fetchEvents(ctx)` (see below)
3. `wg.Wait()` blocks until all goroutines complete.
4. Checks `errCh` — if any goroutine wrote an error, returns it.
5. Sorts results deterministically:
   - Pods, Services, Deployments: by `Name` ascending
   - Events: by `LastTimestamp` ascending
6. Returns the populated `*ClusterSnapshot`.

**Concurrency note:** Each goroutine writes to a distinct field of the snapshot
(`snapshot.Pods`, `snapshot.Services`, etc.), so no mutex is needed — the fields
are independent. The `errCh` is buffered to `len(f.resources)` so all sends are
non-blocking.

#### `fetchEvents(ctx)` — events API with fallback

1. **Try `events.k8s.io/v1` first** via `typedeventsv1.NewForConfig(f.restCfg)`.
   This API has richer metadata and no TTL-based pruning.
2. **Fallback to `core/v1` Events** if the v1 API fails (very old clusters).
3. Both sources are normalized to `corev1.Event` via `convertEventsV1()`.

**`convertEventsV1(in []eventsv1.Event)`** maps:
- `Regarding` → `InvolvedObject`
- `Note` → `Message`
- `Series.Count` (or `DeprecatedCount`) → `Count`
- `DeprecatedLastTimestamp` (or `EventTime`) → `LastTimestamp`

---

## 7. Package: `pkg/sanitizer/`

### 7.1 `pkg/sanitizer/sanitizer.go`

**Responsibility:** Strip Kubernetes API noise from a `ClusterSnapshot` so the
downstream formatters render token-efficient output.

#### Public API

- **`Sanitize(s *k8s.ClusterSnapshot) *k8s.ClusterSnapshot`** — normalizes every
  object in the snapshot in place. All filtering decisions are concentrated here
  so the formatters never see raw API noise.
- **`DurationToHuman(t metav1.Time, ref time.Time) string`** — formats a time
  relative to a reference into a short human string (`"3d"`, `"4h"`, `"120m"`).
  Uses `k8s.io/apimachinery/pkg/util/duration.HumanDuration` (same as kubectl).
  Zero time yields `"unknown"`.
- **`Age(o metav1.ObjectMeta, ref time.Time) string`** — convenience wrapper
  around `DurationToHuman` for an object's `CreationTimestamp`.

#### Sanitization functions

**`sanitizeMeta(meta *metav1.ObjectMeta)`** strips:
- `ManagedFields` (the biggest token hog — multi-KB of controller bookkeeping)
- `UID`, `ResourceVersion`, `Generation`, `SelfLink`
- `DeletionTimestamp`, `DeletionGracePeriodSeconds`
- `OwnerReferences`, `Finalizers`
- Rewrites `Annotations` via `sanitizeAnnotations()`

Labels are kept as-is — they're the primary selector mechanism for services and
deployments.

**`sanitizeAnnotations(in map[string]string)`** — annotation rewriting engine:

| Action | Keys |
| --- | --- |
| **Drop** (noise) | `kubectl.kubernetes.io/last-applied-configuration`, `deployment.kubernetes.io/revision`, `kubernetes.io/config.seen`, `kubernetes.io/config.hash`, `kubernetes.io/config.mirror`, `pv.kubernetes.io/bind-completed`, `volume.kubernetes.io/selected-node` |
| **Alias** | `kubernetes.io/service-account.name` → `serviceaccount`, `kubernetes.io/ingress.class` → `ingress-class`, `kubernetes.io/change-cause` → `change-cause`, `deployment.kubernetes.io/revision` → `revision`, `kubectl.kubernetes.io/default-container` → `default-container` |
| **Keep** | `checksum/*` (Helm ConfigMap/Secret pins), `helm.sh/*` (Helm release bookkeeping) |
| **Drop** (unknown) | Any annotation not matching the above |

**`sanitizePod(p *corev1.Pod, now time.Time)`**:
- Keeps only `Name` and `Image` per container (strips the rest of `Spec.Containers`)
- Clears `InitContainers`, `Tolerations`, `Volumes`
- Clears status noise: `Conditions`, `QOSClass`, `StartTime`, `Reason`, `Message`
- Preserves `ContainerStatuses` but strips `Image`, `ImageID`, `ContainerID`,
  `LastTerminationState`, `Started`
- **Crucially preserves `State.Waiting.Reason` and `State.Terminated.Reason`**
  — the formatter surfaces these as the pod's effective status (e.g.
  `CrashLoopBackOff`)

**`sanitizeService(s *corev1.Service)`**:
- Keeps: `Type`, `ClusterIP`, `ExternalIPs`, `Ports`
- Strips: `Selector`, `SessionAffinity`, `PublishNotReadyAddresses`,
  `LoadBalancerIP`, `IPFamilies`, `IPFamilyPolicy`, `InternalTrafficPolicy`,
  `TrafficDistribution`, `LoadBalancer` status

**`sanitizeDeployment(d *appsv1.Deployment, now time.Time)`**:
- Strips: `Template` (the full pod spec — huge), `RevisionHistoryLimit`,
  `Paused`, `ProgressDeadlineSeconds`, `MinReadySeconds`, `Conditions`,
  `CollisionCount`, `ObservedGeneration`
- Keeps: `Replicas`, `Strategy`, `Selector`, status replica counts

**`sanitizeEvent(e *corev1.Event)`**:
- Normalizes empty `Type` to `"Normal"`
- Strips: `FirstTimestamp`, `Source`, `Related`, `Series`, `Action`,
  `ReportingController`, `ReportingInstance`, `EventTime`
- Keeps: `Type`, `Reason`, `Message`, `Count`, `LastTimestamp`, `InvolvedObject`

---

## 8. Package: `pkg/formatter/`

### 8.1 `pkg/formatter/interface.go`

**`Formatter` interface:**

```go
type Formatter interface {
    Format(s *k8s.ClusterSnapshot) (string, error)
}
```

**`New(name string) (Formatter, error)`** — factory:
- `"markdown"` → `MarkdownFormatter{}`
- `"json"` → `JSONFormatter{}`
- anything else → `ErrUnknownFormat`

Implementations are zero-value structs, safe for concurrent use (no mutable state).

### 8.2 `pkg/formatter/markdown.go`

**`MarkdownFormatter`** renders a sanitized snapshot into token-efficient
Markdown pipe tables.

**`renderMarkdown(s)`** structure:
1. **Header:** `# Cluster Snapshot`, `**Context:**`, `**Scope:**`
2. **Pods table** (if `HasPods`): `| Name | Status | Restarts | IP | Node | Age | Containers |`
3. **Services table** (if `HasServices`): `| Name | Type | ClusterIP | ExternalIPs | Ports |`
4. **Deployments table** (if `HasDeployments`): `| Name | Ready | Up-to-date | Available | Strategy | Age |`
5. **Events section** (if any): `## Recent Events / Errors` with bullet lines

**Key helpers:**

- **`podStatus(p *corev1.Pod)`** — derives the human summary: if phase is
  `Running`, inspects `ContainerStatuses` for a `Waiting.Reason` (e.g.
  `CrashLoopBackOff`) or `Terminated.Reason`; otherwise returns the phase.
- **`imageTail(image string)`** — strips registry prefix and digest, keeping
  only `name:tag` (e.g. `registry.example.com/pay:2.1.0` → `pay:2.1.0`).
- **`trimEvents(events)`** — keeps only the most recent `maxEvents` (25) events.
- **`md(s string)`** — escapes pipe characters (`|` → `\|`) and flattens newlines
  for safe Markdown table cells.
- **`servicePorts(ports)`** — renders port list as `name:port/protocol` or
  `name:port:nodeport/protocol`.

**Event rendering format:**

```
- [Warning] BackOff (12x, 10m ago): Back-off restarting failed container on Pod pay-98ab
```

### 8.3 `pkg/formatter/json.go`

**`JSONFormatter`** renders a snapshot as a single-line, compact JSON document
using explicit snake_case DTOs.

**DTO structure:**

| DTO | Fields |
| --- | --- |
| `snapshotDTO` | `context`, `namespace`, `pods`, `services`, `deployments`, `events` |
| `podDTO` | `name`, `namespace`, `phase`, `podIP`, `nodeName`, `restartCount`, `containers`, `age` |
| `containerDTO` | `name`, `image` |
| `serviceDTO` | `name`, `namespace`, `type`, `clusterIP`, `externalIP`, `ports` |
| `servicePort` | `name`, `port`, `protocol` |
| `deploymentDTO` | `name`, `namespace`, `readyReplicas`, `updatedReplicas`, `availableReplicas`, `desiredReplicas`, `strategy`, `age` |
| `eventDTO` | `type`, `reason`, `message`, `count`, `lastSeen`, `objectKind`, `objectName` |

All DTOs use `omitempty` on optional fields to keep the payload compact. The
`now` variable is a function (`var now = func() time.Time { return time.Now() }`)
so tests can pin the reference time.

**`dtoFromSnapshot(s)`** converts the sanitized snapshot into its wire shape,
calling `sanitizer.Age()` for age strings and `podRestarts()` to sum restart
counts across container statuses.

---

## 9. Tests & Fuzzing

### Unit tests

| File | Tests |
| --- | --- |
| `pkg/k8s/fetcher_test.go` | `TestParseResources` — 7 cases: empty, single, all four, whitespace, dedup, unknown, only-commas |
| `pkg/sanitizer/sanitizer_test.go` | `TestSanitizeStripsManagedFieldsAndMetadata`, `TestSanitizeDropsEmptyContainers`, `TestSanitizeNormalizesEmptyEventType`, `TestDurationToHuman` |
| `pkg/formatter/markdown_test.go` | `TestMarkdownFormatter` (17 assertions), `TestMarkdownFormatterEmptySnapshot`, `TestJSONFormatterRoundTrip` (9 present + 4 noise-absent checks), `TestFormatterNew`, `TestImageTail` |

### Fuzz targets

| File | Target | What it fuzzes |
| --- | --- | --- |
| `pkg/k8s/fuzz_test.go` | `FuzzParseResources` | Resource parser never panics on arbitrary input |
| `pkg/sanitizer/fuzz_test.go` | `FuzzSanitize` | Sanitizer never panics on arbitrary metadata/annotations/status |
| `pkg/formatter/fuzz_test.go` | `FuzzMarkdownFormat` | Markdown formatter never panics on arbitrary snapshot contents |

Fuzz targets run in CI for 30 seconds each via `go test -fuzz -fuzztime 30s`.

---

## 10. CI/CD Workflows

### `ci.yml` — CI (push + PR)

**Triggers:** push to any branch, pull request, manual dispatch.

**Jobs:**

1. **`test`** (Go 1.26): `gofmt` check → `go vet` → `go test -race` → `govulncheck@v1.6.0`
2. **`fuzz`** (needs `test`): 3-matrix fuzz job, each running `go test <pkg> -fuzz <target> -fuzztime 30s`
3. **`build`** (Go 1.26): version-stamped build with `-ldflags` + smoke test (`./k8s-ctx-dumper version`)

**Permissions:** `contents: read` (least privilege).

**Concurrency:** `ci-${{ github.ref }}` with `cancel-in-progress: true` —
cancels superseded runs on the same ref.

### `release.yml` — CD (on `v*` tag)

**Triggers:** push of a `v*` tag, manual dispatch.

**Jobs:**

1. **`release`** (×5 matrix): cross-compiles binaries for
   `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.
   Uses `CGO_ENABLED=0` and `-ldflags "-s -w -X ..."` for stripped, static binaries.
   Attaches to a **draft** GitHub Release via `softprops/action-gh-release@v3`.
2. **`docker`**: QEMU + Buildx multi-arch build (`linux/amd64,linux/arm64`),
   pushes to Docker Hub with tags `vX.Y.Z` and `latest` (for stable releases).
   Uses `docker/metadata-action` for tag generation and a "Compute bare version"
   step that strips the leading `v` from the tag for the `VERSION` build-arg.

**Permissions:** `contents: read` at workflow level; `contents: write` on the
`release` job (to attach assets); `contents: read` on the `docker` job.

### `codeql.yml` — SAST

**Triggers:** push to `main`, PR to `main`, weekly (Monday 02:22 UTC), manual.

**Jobs:** `analyze` (Go) — initializes CodeQL, autobuilds, performs analysis,
uploads SARIF to the Security tab.

### `scorecard.yml` — OpenSSF Scorecard

**Triggers:** push to `main`, weekly (Monday 00:33 UTC), manual.

**Jobs:** `analysis` — runs `ossf/scorecard-action@v2.4.0`, uploads SARIF to
the security dashboard via `github/codeql-action/upload-sarif@v3`.

Uses `step-security/harden-runner@v2` with `egress-policy: audit` and
`persist-credentials: false` on checkout.

---

## 11. Containerization

### `Dockerfile` (multi-stage)

**Stage 1: `build`**
- Base: `golang:1.26` (pinned to digest `sha256:3aff6657...`)
- Caches `go mod download` separately from source changes
- Builds with `CGO_ENABLED=0` and `-ldflags "-s -w -X ..."` (stripped, static)
- Accepts `VERSION`, `COMMIT`, `BUILD_DATE` build args

**Stage 2: runtime**
- Base: `gcr.io/distroless/static-debian12:nonroot` (pinned to digest
  `sha256:f5b485ea...`)
- Copies the binary to `/usr/local/bin/k8s-ctx-dumper`
- `ENTRYPOINT` runs the binary directly (no shell)
- Result: ~15MB image, no shell, no package manager

### `docker-compose.yml`

Example service mounting `~/.kube` read-only with `HOME=/root` so client-go's
default config lookup works.

### `.dockerignore`

Excludes `.git`, `.commandcode`, `*.md`, `Dockerfile`, `docker-compose.yml`,
`.github`, the built binary, `dist`, `LICENSE`, `SECURITY.md` from the build
context.

---

## 12. Security & Supply Chain

### Pinned dependencies

All GitHub Actions are pinned to full commit SHAs with version comments:

```yaml
uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4
```

Dockerfile base images are pinned to digests:

```dockerfile
FROM golang:1.26@sha256:3aff6657219a4d9c14e27fb1d8976c49c29fddb70ba835014f477e1c70636647 AS build
```

### Vulnerability scanning

- **`govulncheck@v1.6.0`** runs in CI on every push/PR — reports zero
  vulnerabilities after bump of `golang.org/x/net` → v0.56.0 and
  `golang.org/x/text` → v0.39.0.
- **CodeQL** SAST analysis on Go code (push to main, PR, weekly).

### CodeRabbit AI review

`.coderabbit.yaml` configures the free tier:
- Profile: `chill` (balanced feedback)
- Auto-review on every push with incremental re-review
- Go tooling: `golangci-lint`, `gitleaks`, `actionlint`, `zizmor`

### Dependabot

`.github/dependabot.yml` opens weekly PRs for:
- `gomod` dependencies
- `github-actions` dependencies

---

## 13. Configuration Files

| File | Purpose |
| --- | --- |
| `go.mod` | Module `k8s-ctx-dumper`, Go 1.26.2, direct deps: cobra, atotto/clipboard, k8s.io/api, apimachinery, client-go |
| `.github/dependabot.yml` | Weekly Dependabot for `gomod` and `github-actions` |
| `.coderabbit.yaml` | CodeRabbit free-tier config (chill profile, Go tools) |
| `.gitignore` | Ignores built binary, `dist/`, `*.exe`, coverage, editor cruft, `.commandcode/` |
| `.dockerignore` | Excludes non-essential files from Docker build context |
| `SECURITY.md` | Vulnerability reporting policy (private advisories) |
| `LICENSE` | Apache 2.0 |

---

## 14. Data Flow: End-to-End

```
User runs: k8s-ctx-dumper dump -n default --format markdown
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 1. cmd/dump.go: runDump()                               │
│    ParseResources("pods,services,deployments")           │
│    namespace = "default"                                 │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 2. pkg/k8s/client.go: NewClient(kubeconfig, context)     │
│    → *rest.Config (kubeconfig or InClusterConfig)       │
│    → *kubernetes.Clientset                              │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 3. pkg/k8s/fetcher.go: Fetcher.Fetch(ctx)               │
│    4 goroutines (WaitGroup) → ClusterSnapshot           │
│    • Pods: CoreV1().Pods().List()                       │
│    • Services: CoreV1().Services().List()              │
│    • Deployments: AppsV1().Deployments().List()        │
│    • Events: events.k8s.io/v1 → core/v1 fallback        │
│    Sort: Pods/Svc/Dep by Name, Events by LastTimestamp  │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 4. pkg/sanitizer/sanitizer.go: Sanitize(snapshot)      │
│    • sanitizeMeta() — strips managedFields, uid, etc.   │
│    • sanitizeAnnotations() — drops noise, aliases high  │
│    • sanitizePod() — keeps name, image, phase, restarts  │
│    • sanitizeService/Deployment() — keeps type, ports   │
│    • sanitizeEvent() — normalizes type, strips source   │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 5. pkg/formatter: New("markdown").Format(snapshot)      │
│    MarkdownFormatter → pipe tables                      │
│    JSONFormatter → compact snake_case JSON              │
└─────────────────────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────────────┐
│ 6. Output: stdout · --output file · --copy clipboard    │
└─────────────────────────────────────────────────────────┘
```

---

## 15. Design Decisions

### Why goroutines for fetching?

Each Kubernetes API call is an independent network round-trip. Fetching 4
resource types sequentially would add latency proportional to the sum of all
calls. Concurrent fetching reduces total latency to the slowest single call.
The `WaitGroup` + buffered `errCh` pattern is simple and correct: each goroutine
writes to a distinct snapshot field (no shared state), and the error channel is
buffered so sends never block.

### Why preserve `State.Waiting.Reason` in the sanitizer?

The sanitizer strips most of `ContainerStatuses` (image, imageID, containerID,
lastTerminationState, started). But `State.Waiting.Reason` (e.g.
`CrashLoopBackOff`) is the single highest-signal field for diagnosing pod
issues — it's what `kubectl get pods` shows in the STATUS column. The Markdown
formatter's `podStatus()` function reads this to surface the effective status
even when the pod phase is `Running`.

### Why two event sources?

`events.k8s.io/v1` is the recommended, richer source (no TTL-based pruning,
better aggregation via `Series`). But very old clusters may not have it, so
`core/v1` Events are the compatibility fallback. Both are normalized to
`corev1.Event` so the rest of the pipeline (sanitizer, formatters) sees a single
event shape.

### Why snake_case JSON DTOs?

The default `json.Marshal` of a `ClusterSnapshot` would produce camelCase keys
matching the Go struct field names, and would include all the sanitized-away
fields as zero values. The explicit `snapshotDTO` with `json:"snake_case"` tags
gives a clean, compact, machine-parseable wire shape with `omitempty` on
optional fields — strictly smaller than what the API returned.

### Why pin Actions to SHAs?

GitHub Actions tags are mutable — a compromised action could push a new commit
to an existing tag. Pinning to the full commit SHA makes the workflow
non-reproducible-by-attacker. The version comment (`# v4`) preserves readability
for humans. This is an OpenSSF Scorecard best practice.

### Why a draft GitHub Release?

The release workflow creates a **draft** release so a human can review the
binaries and release notes before publishing. This prevents an accidental tag
push from immediately shipping a release to users.

---


