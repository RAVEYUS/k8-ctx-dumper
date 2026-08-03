package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedeventsv1 "k8s.io/client-go/kubernetes/typed/events/v1"
	"k8s.io/client-go/rest"
)

// ResourceKind enumerates the resource types the fetcher understands.
type ResourceKind string

const (
	ResourcePods        ResourceKind = "pods"
	ResourceServices    ResourceKind = "services"
	ResourceDeployments ResourceKind = "deployments"
	ResourceEvents      ResourceKind = "events"
)

// AllResources is the default resource set, in a stable display order.
var AllResources = []ResourceKind{
	ResourcePods,
	ResourceServices,
	ResourceDeployments,
	ResourceEvents,
}

// ParseResources converts a comma-separated flag value (e.g. "pods,services")
// into a de-duplicated, stable-ordered list of ResourceKind values. Unknown
// names produce an error so typos fail loudly instead of silently dumping
// nothing.
func ParseResources(raw string) ([]ResourceKind, error) {
	if strings.TrimSpace(raw) == "" {
		return []ResourceKind{ResourcePods, ResourceServices, ResourceDeployments}, nil
	}

	seen := make(map[ResourceKind]bool)
	var out []ResourceKind
	for _, part := range strings.Split(raw, ",") {
		kind := ResourceKind(strings.ToLower(strings.TrimSpace(part)))
		if kind == "" {
			continue
		}
		switch kind {
		case ResourcePods, ResourceServices, ResourceDeployments, ResourceEvents:
			if !seen[kind] {
				seen[kind] = true
				out = append(out, kind)
			}
		default:
			return nil, fmt.Errorf("unknown resource %q (valid: pods, services, deployments, events)", part)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no resources requested")
	}
	return out, nil
}

// ClusterSnapshot is the sanitizer-ready view of a cluster. Raw API objects
// are stored here (unsanitized); the sanitizer package normalizes them before
// formatting. The events carry the namespace of the object they describe so a
// per-namespace dumper can still surface cluster-wide issues (e.g. node events).
type ClusterSnapshot struct {
	// Context is the kubeconfig context name the snapshot was taken from.
	Context string

	// Namespace is the namespace filter that was applied ("all" for every
	// namespace), matching the field the formatters print in headings.
	Namespace string

	// Pods, Services and Deployments are the resource lists requested by the
	// user. The booleans track which were requested so formatters can render
	// only what was fetched.
	Pods           []corev1.Pod
	Services       []corev1.Service
	Deployments    []appsv1.Deployment
	HasPods        bool
	HasServices    bool
	HasDeployments bool

	// Events are kept verbatim (count, reason, message) so the formatter can
	// decide how much detail to surface; only the list is trimmed.
	Events []corev1.Event
}

// EventNamespace is a helper for formatters that bucket events by the
// namespace of their involved object.
func (s *ClusterSnapshot) EventNamespace(e corev1.Event) string {
	if ns := e.InvolvedObject.Namespace; ns != "" {
		return ns
	}
	return s.Namespace
}

// Fetcher queries the cluster for the requested resources. Fetch is
// concurrency-safe: each requested resource type is queried in its own
// goroutine and results are joined with a WaitGroup.
type Fetcher struct {
	client    kubernetes.Interface
	restCfg   *rest.Config
	namespace string
	resources []ResourceKind
}

// NewFetcher builds a Fetcher from a clientset and its config. namespace
// follows kubectl semantics: an empty string means all namespaces.
func NewFetcher(client kubernetes.Interface, restCfg *rest.Config, namespace string, resources []ResourceKind) *Fetcher {
	return &Fetcher{
		client:    client,
		restCfg:   restCfg,
		namespace: namespace,
		resources: resources,
	}
}

// Fetch concurrently queries every requested resource type and assembles the
// results into a ClusterSnapshot. A resource list is fetched for the full
// cluster when namespace is empty, or scoped to one namespace otherwise.
func (f *Fetcher) Fetch(ctx context.Context) (*ClusterSnapshot, error) {
	snapshot := &ClusterSnapshot{Namespace: f.namespace}

	var (
		wg                                   sync.WaitGroup
		errCh                                = make(chan error, len(f.resources))
		hasPods, hasServices, hasDeployments bool
	)

	for _, kind := range f.resources {
		// Capture the loop variable so the goroutine sees this iteration's
		// value, not the value from the final iteration.
		switch kind {
		case ResourcePods:
			wg.Add(1)
			hasPods = true
			go func() {
				defer wg.Done()
				list, err := f.client.CoreV1().Pods(f.namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					errCh <- fmt.Errorf("listing pods: %w", err)
					return
				}
				snapshot.Pods = list.Items
			}()
		case ResourceServices:
			wg.Add(1)
			hasServices = true
			go func() {
				defer wg.Done()
				list, err := f.client.CoreV1().Services(f.namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					errCh <- fmt.Errorf("listing services: %w", err)
					return
				}
				snapshot.Services = list.Items
			}()
		case ResourceDeployments:
			wg.Add(1)
			hasDeployments = true
			go func() {
				defer wg.Done()
				list, err := f.client.AppsV1().Deployments(f.namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					errCh <- fmt.Errorf("listing deployments: %w", err)
					return
				}
				snapshot.Deployments = list.Items
			}()
		case ResourceEvents:
			wg.Add(1)
			go func() {
				defer wg.Done()
				events, err := f.fetchEvents(ctx)
				if err != nil {
					errCh <- fmt.Errorf("listing events: %w", err)
					return
				}
				snapshot.Events = events
			}()
		}
	}

	wg.Wait()
	close(errCh)

	if err := <-errCh; err != nil {
		return nil, err
	}

	snapshot.HasPods = hasPods
	snapshot.HasServices = hasServices
	snapshot.HasDeployments = hasDeployments

	// Deterministic ordering keeps diffs between runs meaningful.
	sort.Slice(snapshot.Pods, func(i, j int) bool {
		return snapshot.Pods[i].Name < snapshot.Pods[j].Name
	})
	sort.Slice(snapshot.Services, func(i, j int) bool {
		return snapshot.Services[i].Name < snapshot.Services[j].Name
	})
	sort.Slice(snapshot.Deployments, func(i, j int) bool {
		return snapshot.Deployments[i].Name < snapshot.Deployments[j].Name
	})
	sort.Slice(snapshot.Events, func(i, j int) bool {
		return snapshot.Events[i].LastTimestamp.Before(&snapshot.Events[j].LastTimestamp)
	})

	return snapshot, nil
}

// fetchEvents prefers the events.k8s.io/v1 API (richer metadata, no TTL-based
// pruning) and falls back to the core/v1 Events when the former is unavailable
// (very old clusters). Both sources are normalized to corev1.Event so the rest
// of the pipeline stays source-agnostic.
func (f *Fetcher) fetchEvents(ctx context.Context) ([]corev1.Event, error) {
	// Try the events API group first: it is the recommended, more complete
	// source. core/v1 events are the compatibility fallback.
	var v1Err error
	typed, err := typedeventsv1.NewForConfig(f.restCfg)
	if err == nil {
		var evList *eventsv1.EventList
		if f.namespace == "" {
			evList, err = typed.Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		} else {
			evList, err = typed.Events(f.namespace).List(ctx, metav1.ListOptions{})
		}
		if err == nil {
			return convertEventsV1(evList.Items), nil
		}
		v1Err = err
	}

	// Fall back to core/v1 events (available on every cluster).
	var coreList *corev1.EventList
	if f.namespace == "" {
		coreList, err = f.client.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	} else {
		coreList, err = f.client.CoreV1().Events(f.namespace).List(ctx, metav1.ListOptions{})
	}
	if err == nil {
		return coreList.Items, nil
	}
	return nil, fmt.Errorf("events.k8s.io/v1 (%v) and core/v1 events (%v) both failed", v1Err, err)
}

// convertEventsV1 normalizes events.k8s.io/v1 events into corev1.Event so the
// rest of the pipeline (sanitizer, formatters) sees a single event shape.
func convertEventsV1(in []eventsv1.Event) []corev1.Event {
	out := make([]corev1.Event, 0, len(in))
	for _, e := range in {
		ev := corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      e.Name,
				Namespace: e.Namespace,
			},
			InvolvedObject: e.Regarding,
			Type:           e.Type,
			Reason:         e.Reason,
			Message:        e.Note,
		}
		// events.k8s.io/v1 splits the count across the series (aggregated)
		// and the deprecated per-event counters.
		switch {
		case e.Series != nil && e.Series.Count > 0:
			ev.Count = e.Series.Count
		case e.DeprecatedCount > 0:
			ev.Count = e.DeprecatedCount
		default:
			ev.Count = 1
		}
		if !e.DeprecatedLastTimestamp.IsZero() {
			ev.LastTimestamp = e.DeprecatedLastTimestamp
		} else if !e.EventTime.IsZero() {
			ev.LastTimestamp = metav1.NewTime(e.EventTime.Time)
		}
		out = append(out, ev)
	}
	return out
}
