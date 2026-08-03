package formatter

import (
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s-ctx-dumper/pkg/k8s"
	"k8s-ctx-dumper/pkg/sanitizer"
)

// ErrUnknownFormat is returned by New for an unsupported format name.
var ErrUnknownFormat = errors.New("unknown output format (supported: markdown, json)")

// MarkdownFormatter is a zero-value Formatter implementation.
type MarkdownFormatter struct{}

// Format implements Formatter for the Markdown backend.
func (MarkdownFormatter) Format(s *k8s.ClusterSnapshot) (string, error) {
	if s == nil {
		return "", fmt.Errorf("cannot format a nil snapshot")
	}
	return renderMarkdown(s), nil
}

// renderMarkdown renders a sanitized snapshot into token-efficient Markdown.
// The header identifies the context and namespace scope, then each requested
// resource appears as a compact table, and the output closes with a condensed
// list of the most recent non-trivial events.
func renderMarkdown(s *k8s.ClusterSnapshot) string {
	var b strings.Builder

	scope := s.Namespace
	if scope == "" {
		scope = "all namespaces"
	}
	fmt.Fprintf(&b, "# Cluster Snapshot\n\n")
	fmt.Fprintf(&b, "- **Context:** `%s`\n", s.Context)
	fmt.Fprintf(&b, "- **Scope:** `%s`\n", scope)

	if s.HasPods {
		renderPods(&b, s.Pods)
	}
	if s.HasServices {
		renderServices(&b, s.Services)
	}
	if s.HasDeployments {
		renderDeployments(&b, s.Deployments)
	}

	events := trimEvents(s.Events)
	if len(events) > 0 {
		b.WriteString("\n## Recent Events / Errors\n")
		for _, e := range events {
			renderEvent(&b, e, s)
		}
	}

	return b.String()
}

// renderPods emits the Pods table. Each row shows phase, total restarts across
// containers, pod IP, node, age, and the container list ("app:1.2.3").
func renderPods(b *strings.Builder, pods []corev1.Pod) {
	now := time.Now()
	fmt.Fprintf(b, "\n## Pods (%d)\n\n", len(pods))
	b.WriteString("| Name | Status | Restarts | IP | Node | Age | Containers |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")

	for _, p := range pods {
		name := p.Name
		if ns := p.Namespace; ns != "" {
			name = ns + "/" + name
		}
		status := podStatus(&p)
		restarts := 0
		for _, cs := range p.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
		containers := make([]string, 0, len(p.Spec.Containers))
		for _, c := range p.Spec.Containers {
			// Format as "name:image-tail" so the container identity is kept
			// while the registry prefix is dropped ("pay:2.1.0").
			containers = append(containers, c.Name+":"+imageTail(c.Image))
		}
		fmt.Fprintf(b, "| %s | %s | %d | %s | %s | %s | %s |\n",
			md(name), md(status), restarts, md(p.Status.PodIP), md(p.Spec.NodeName),
			sanitizer.Age(p.ObjectMeta, now), md(strings.Join(containers, ", ")))
	}
}

// podStatus derives the human summary for a pod, matching kubectl's signal:
// phase, then the most severe container state.
func podStatus(p *corev1.Pod) string {
	switch p.Status.Phase {
	case corev1.PodRunning:
		// Report the worst container state; a Running phase can hide a
		// CrashLoopBackOff container.
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				return cs.State.Waiting.Reason
			}
			if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
				return cs.State.Terminated.Reason
			}
		}
		return string(corev1.PodRunning)
	case "":
		return "Pending"
	default:
		return string(p.Status.Phase)
	}
}

// renderServices emits the Services table: type, cluster IP, external IPs, ports.
func renderServices(b *strings.Builder, services []corev1.Service) {
	fmt.Fprintf(b, "\n## Services (%d)\n\n", len(services))
	b.WriteString("| Name | Type | ClusterIP | ExternalIPs | Ports |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")

	for _, svc := range services {
		name := svc.Name
		if ns := svc.Namespace; ns != "" {
			name = ns + "/" + name
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			md(name), md(string(svc.Spec.Type)), md(svc.Spec.ClusterIP),
			md(strings.Join(svc.Spec.ExternalIPs, ",")), md(servicePorts(svc.Spec.Ports)))
	}
}

// servicePorts renders the port list ("80:8080/TCP,443/TCP").
func servicePorts(ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.NodePort != 0 {
			parts = append(parts, fmt.Sprintf("%s:%d:%d/%s", p.Name, p.Port, p.NodePort, p.Protocol))
		} else {
			parts = append(parts, fmt.Sprintf("%s:%d/%s", p.Name, p.Port, p.Protocol))
		}
	}
	return strings.Join(parts, ", ")
}

// renderDeployments emits the Deployments table: ready/updated/available
// replica counts, strategy, and age.
func renderDeployments(b *strings.Builder, deployments []appsv1.Deployment) {
	now := time.Now()
	fmt.Fprintf(b, "\n## Deployments (%d)\n\n", len(deployments))
	b.WriteString("| Name | Ready | Up-to-date | Available | Strategy | Age |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")

	for _, d := range deployments {
		name := d.Name
		if ns := d.Namespace; ns != "" {
			name = ns + "/" + name
		}
		fmt.Fprintf(b, "| %s | %d/%d | %d | %d | %s | %s |\n",
			md(name),
			d.Status.ReadyReplicas, derefReplicas(d.Spec.Replicas),
			d.Status.UpdatedReplicas, d.Status.AvailableReplicas,
			md(string(d.Spec.Strategy.Type)), sanitizer.Age(d.ObjectMeta, now))
	}
}

// derefReplicas safely unwraps the optional replicas pointer.
func derefReplicas(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// renderEvent emits a single event line:
//
//   - [Warning] BackOff (12x): Back-off restarting failed container in pod payment-api-98ab
//
// The involved object name and the last-seen timestamp are folded in.
func renderEvent(b *strings.Builder, e corev1.Event, s *k8s.ClusterSnapshot) {
	kind := e.InvolvedObject.Kind
	if kind == "" {
		kind = "object"
	}
	obj := e.InvolvedObject.Name
	if obj == "" {
		obj = "(unknown)"
	}
	age := sanitizer.DurationToHuman(e.LastTimestamp, time.Now())

	var line string
	if e.Count > 1 {
		line = fmt.Sprintf("- [%s] %s (%dx, %s ago): %s on %s %s",
			e.Type, e.Reason, e.Count, age, e.Message, kind, obj)
	} else {
		line = fmt.Sprintf("- [%s] %s (%s ago): %s on %s %s",
			e.Type, e.Reason, age, e.Message, kind, obj)
	}
	b.WriteString(line + "\n")
}

// trimEvents keeps the most recent maxEvents events. Events are expected to be
// sorted by LastTimestamp ascending (the fetcher guarantees this).
func trimEvents(events []corev1.Event) []corev1.Event {
	if len(events) <= maxEvents {
		return events
	}
	return events[len(events)-maxEvents:]
}

// maxEvents caps the number of events rendered to keep the dump focused.
const maxEvents = 25

// imageTail shortens an image reference to its final "name:tag" segment.
func imageTail(image string) string {
	if image == "" {
		return ""
	}
	// Strip registry host prefixes like "docker.io/library/" or "ghcr.io/org/".
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	if i := strings.LastIndex(image, "@"); i >= 0 { // digest references
		image = image[:i]
	}
	return image
}

// md escapes a value for use inside a Markdown table cell: pipe characters
// become HTML entities (the only character that breaks pipe tables) and newlines
// are flattened.
func md(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
