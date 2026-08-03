// Package sanitizer strips Kubernetes API noise from a ClusterSnapshot so the
// downstream formatters can render token-efficient output. It removes
// managedFields, low-value object metadata, redundant status transition data
// and empty fields, and rewrites common "kubectl.kubernetes.io/*" annotations
// into short, readable aliases (e.g. the last-applied-configuration blob).
package sanitizer

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/duration"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-ctx-dumper/pkg/k8s"
)

// DurationToHuman formats a metav1.Time relative to a reference time into a
// short human string ("3d", "4h", "12m", "35s"). A zero time yields "unknown".
// The reference is usually time.Now(), which keeps ages consistent within one
// snapshot render.
func DurationToHuman(t v1.Time, ref time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := ref.Sub(t.Time)
	if d < 0 {
		d = 0
	}
	return duration.HumanDuration(d)
}

// Age formats the age of an object relative to the given reference time.
func Age(o v1.ObjectMeta, ref time.Time) string {
	return DurationToHuman(o.CreationTimestamp, ref)
}

// Sanitize normalizes every object in the snapshot in place (the returned
// snapshot shares backing arrays with the input). All filtering decisions are
// concentrated here so the formatters never see raw API noise.
func Sanitize(s *k8s.ClusterSnapshot) *k8s.ClusterSnapshot {
	now := time.Now()

	for i := range s.Pods {
		sanitizeMeta(&s.Pods[i].ObjectMeta)
		sanitizePod(&s.Pods[i], now)
	}
	for i := range s.Services {
		sanitizeMeta(&s.Services[i].ObjectMeta)
		sanitizeService(&s.Services[i])
	}
	for i := range s.Deployments {
		sanitizeMeta(&s.Deployments[i].ObjectMeta)
		sanitizeDeployment(&s.Deployments[i], now)
	}
	for i := range s.Events {
		sanitizeEvent(&s.Events[i])
	}
	return s
}

// sanitizeMeta strips the fields that carry no signal for an LLM context
// dump: managedFields, system identifiers, and patch/revision bookkeeping.
func sanitizeMeta(meta *v1.ObjectMeta) {
	meta.ManagedFields = nil
	meta.UID = ""
	meta.ResourceVersion = ""
	meta.Generation = 0
	meta.SelfLink = ""
	meta.DeletionTimestamp = nil
	meta.DeletionGracePeriodSeconds = nil
	meta.OwnerReferences = nil
	meta.Finalizers = nil
	// Annotations are heavily rewritten below; labels are kept as-is because
	// they are the primary selector mechanism for services and deployments.
	meta.Annotations = sanitizeAnnotations(meta.Annotations)
}

// noiseAnnotationKeys are annotation keys that are dropped entirely. They are
// bookkeeping or internal plumbing rather than user intent.
var noiseAnnotationKeys = map[string]bool{
	// kubectl's last-applied-configuration is a multi-KB JSON blob with no
	// informational value in a context dump.
	"kubectl.kubernetes.io/last-applied-configuration": true,
	// Deployment revision bookkeeping.
	"deployment.kubernetes.io/revision": true,
	// Internal state carried by kubelet/controller-manager.
	"kubernetes.io/config.seen":          true,
	"kubernetes.io/config.hash":          true,
	"kubernetes.io/config.mirror":        true,
	"pv.kubernetes.io/bind-completed":    true,
	"volume.kubernetes.io/selected-node": true,
}

// sanitizeAnnotations rewrites high-value but verbose annotations into short
// aliases, keeps selector/limit annotations, and drops everything else.
func sanitizeAnnotations(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		if noiseAnnotationKeys[k] {
			continue
		}
		switch {
		case k == "kubernetes.io/service-account.name":
			out["serviceaccount"] = v
		case k == "kubernetes.io/ingress.class":
			out["ingress-class"] = v
		case k == "kubernetes.io/change-cause":
			out["change-cause"] = v
		case k == "deployment.kubernetes.io/revision":
			out["revision"] = v
		case k == "kubectl.kubernetes.io/default-container":
			out["default-container"] = v
		case strings.HasPrefix(k, "checksum/"):
			// ConfigMap/Secret checksum pins (common in Helm charts) are
			// relevant rollout triggers; keep them short.
			out[k] = v
		case strings.HasPrefix(k, "helm.sh/"):
			// Helm release bookkeeping is high-signal: it tells you which
			// chart and revision a workload came from.
			out[k] = v
		default:
			// Unknown annotations are dropped to save tokens.
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sanitizePod condenses a Pod's status into just the fields the formatter
// renders: phase, readiness, restarts, pod IP, node, and the per-container
// summary lines. It clears the bulk of status internals (conditions, QOS,
// volume mounts, tolerations).
func sanitizePod(p *corev1.Pod, now time.Time) {
	// Keep only image per container; name and image are the high-signal bits.
	containers := make([]corev1.Container, 0, len(p.Spec.Containers))
	for _, c := range p.Spec.Containers {
		containers = append(containers, corev1.Container{Name: c.Name, Image: c.Image})
	}
	p.Spec.Containers = containers
	p.Spec.InitContainers = nil
	p.Spec.Tolerations = nil
	p.Spec.Volumes = nil

	// Condense status: keep only what the table shows.
	p.Status.Conditions = nil
	p.Status.QOSClass = ""
	p.Status.StartTime = nil
	p.Status.Reason = ""
	p.Status.Message = ""
	// Keep ContainerStatuses but strip their verbose fields. The container
	// state reason (e.g. CrashLoopBackOff) is preserved because the formatter
	// surfaces it as the pod's effective status; everything else is dropped.
	for i := range p.Status.ContainerStatuses {
		cs := &p.Status.ContainerStatuses[i]
		cs.Image = ""       // duplicated from spec
		cs.ImageID = ""     // long digest, no signal
		cs.ContainerID = "" // runtime id, no signal
		cs.LastTerminationState = corev1.ContainerState{}
		cs.Started = nil
	}
	_ = now // reserved for future per-pod age fields
}

// sanitizeService keeps the external entry points of a Service (type, cluster
// IP, external IP, ports) and drops internal selector/status noise.
func sanitizeService(s *corev1.Service) {
	s.Spec.Selector = nil
	s.Spec.SessionAffinity = ""
	s.Spec.PublishNotReadyAddresses = false
	s.Spec.LoadBalancerIP = ""
	s.Spec.IPFamilies = nil
	s.Spec.IPFamilyPolicy = nil
	s.Spec.InternalTrafficPolicy = nil
	s.Spec.TrafficDistribution = nil
	s.Status.LoadBalancer = corev1.LoadBalancerStatus{}
}

// sanitizeDeployment condenses a Deployment to spec (replicas, strategy) and
// the status summary line (ready/updated/available replicas).
func sanitizeDeployment(d *appsv1.Deployment, now time.Time) {
	d.Spec.Template = corev1.PodTemplateSpec{}
	d.Spec.RevisionHistoryLimit = nil
	d.Spec.Paused = false
	d.Spec.ProgressDeadlineSeconds = nil
	d.Spec.MinReadySeconds = 0
	d.Status.Conditions = nil
	d.Status.CollisionCount = nil
	d.Status.ObservedGeneration = 0
	_ = now
}

// sanitizeEvent strips metadata that duplicates the message (event type,
// involved object, count and timestamps are kept) and normalizes empty types.
func sanitizeEvent(e *corev1.Event) {
	e.Type = strings.TrimSpace(e.Type)
	if e.Type == "" {
		e.Type = "Normal"
	}
	e.FirstTimestamp = v1.Time{}
	e.Source = corev1.EventSource{}
	e.Related = nil
	e.Series = nil
	e.Action = ""
	e.ReportingController = ""
	e.ReportingInstance = ""
	e.EventTime = v1.MicroTime{}
	// Keep: Type, Reason, Message, Count, LastTimestamp, InvolvedObject
}
