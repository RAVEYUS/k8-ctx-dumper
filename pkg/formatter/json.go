package formatter

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"k8s-ctx-dumper/pkg/k8s"
	"k8s-ctx-dumper/pkg/sanitizer"
)

// JSONFormatter renders a snapshot as a single-line, compact JSON document.
// The structure mirrors the sanitized objects, so it is strictly smaller than
// what the API returned but remains machine-parseable.
type JSONFormatter struct{}

// Format implements Formatter for the JSON backend. The document is compact
// (no indentation) to keep the token cost of the dump low.
func (JSONFormatter) Format(s *k8s.ClusterSnapshot) (string, error) {
	if s == nil {
		return "", fmt.Errorf("cannot format a nil snapshot")
	}
	data, err := json.Marshal(dtoFromSnapshot(s))
	if err != nil {
		return "", fmt.Errorf("marshaling snapshot to JSON: %w", err)
	}
	return string(data), nil
}

// snapshotDTO is the JSON wire shape of a ClusterSnapshot. It uses explicit
// snake_case keys and omits empty collections, keeping the payload compact.
type snapshotDTO struct {
	Context     string          `json:"context"`
	Namespace   string          `json:"namespace,omitempty"`
	Pods        []podDTO        `json:"pods,omitempty"`
	Services    []serviceDTO    `json:"services,omitempty"`
	Deployments []deploymentDTO `json:"deployments,omitempty"`
	Events      []eventDTO      `json:"events,omitempty"`
}

// podDTO is the token-minimal representation of a pod.
type podDTO struct {
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace,omitempty"`
	Phase        string         `json:"phase"`
	PodIP        string         `json:"podIP,omitempty"`
	NodeName     string         `json:"nodeName,omitempty"`
	RestartCount int32          `json:"restartCount"`
	Containers   []containerDTO `json:"containers,omitempty"`
	Age          string         `json:"age"`
}

// containerDTO is the token-minimal representation of a container.
type containerDTO struct {
	Name  string `json:"name"`
	Image string `json:"image,omitempty"`
}

// serviceDTO is the token-minimal representation of a service.
type serviceDTO struct {
	Name       string        `json:"name"`
	Namespace  string        `json:"namespace,omitempty"`
	Type       string        `json:"type"`
	ClusterIP  string        `json:"clusterIP,omitempty"`
	ExternalIP []string      `json:"externalIP,omitempty"`
	Ports      []servicePort `json:"ports,omitempty"`
}

// servicePort is the token-minimal representation of a service port.
type servicePort struct {
	Name     string `json:"name,omitempty"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol,omitempty"`
}

// deploymentDTO is the token-minimal representation of a deployment.
type deploymentDTO struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace,omitempty"`
	ReadyReplicas     int32  `json:"readyReplicas"`
	UpdatedReplicas   int32  `json:"updatedReplicas"`
	AvailableReplicas int32  `json:"availableReplicas"`
	DesiredReplicas   int32  `json:"desiredReplicas"`
	Strategy          string `json:"strategy,omitempty"`
	Age               string `json:"age"`
}

// eventDTO is the token-minimal representation of an event.
type eventDTO struct {
	Type       string `json:"type"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	Count      int32  `json:"count"`
	LastSeen   string `json:"lastSeen"`
	ObjectKind string `json:"objectKind,omitempty"`
	ObjectName string `json:"objectName,omitempty"`
}

// dtoFromSnapshot converts a (sanitized) snapshot into its wire shape.
func dtoFromSnapshot(s *k8s.ClusterSnapshot) *snapshotDTO {
	d := &snapshotDTO{
		Context:   s.Context,
		Namespace: s.Namespace,
	}

	if s.HasPods {
		d.Pods = make([]podDTO, 0, len(s.Pods))
		for _, p := range s.Pods {
			d.Pods = append(d.Pods, podDTO{
				Name:         p.Name,
				Namespace:    p.Namespace,
				Phase:        string(p.Status.Phase),
				PodIP:        p.Status.PodIP,
				NodeName:     p.Spec.NodeName,
				RestartCount: podRestarts(p.Status.ContainerStatuses),
				Containers:   containersDTO(p.Spec.Containers),
				Age:          sanitizer.Age(p.ObjectMeta, now()),
			})
		}
	}
	if s.HasServices {
		d.Services = make([]serviceDTO, 0, len(s.Services))
		for _, svc := range s.Services {
			d.Services = append(d.Services, serviceDTO{
				Name:       svc.Name,
				Namespace:  svc.Namespace,
				Type:       string(svc.Spec.Type),
				ClusterIP:  svc.Spec.ClusterIP,
				ExternalIP: svc.Spec.ExternalIPs,
				Ports:      servicePortsDTO(svc.Spec.Ports),
			})
		}
	}
	if s.HasDeployments {
		d.Deployments = make([]deploymentDTO, 0, len(s.Deployments))
		for _, dep := range s.Deployments {
			d.Deployments = append(d.Deployments, deploymentDTO{
				Name:              dep.Name,
				Namespace:         dep.Namespace,
				ReadyReplicas:     dep.Status.ReadyReplicas,
				UpdatedReplicas:   dep.Status.UpdatedReplicas,
				AvailableReplicas: dep.Status.AvailableReplicas,
				DesiredReplicas:   derefReplicas(dep.Spec.Replicas),
				Strategy:          string(dep.Spec.Strategy.Type),
				Age:               sanitizer.Age(dep.ObjectMeta, now()),
			})
		}
	}
	if len(s.Events) > 0 {
		d.Events = make([]eventDTO, 0, len(s.Events))
		for _, e := range s.Events {
			d.Events = append(d.Events, eventDTO{
				Type:       e.Type,
				Reason:     e.Reason,
				Message:    e.Message,
				Count:      e.Count,
				LastSeen:   e.LastTimestamp.Format("2006-01-02T15:04:05Z07:00"),
				ObjectKind: e.InvolvedObject.Kind,
				ObjectName: e.InvolvedObject.Name,
			})
		}
	}
	return d
}

// podRestarts sums the restart count across all container statuses.
func podRestarts(statuses []corev1.ContainerStatus) int32 {
	var total int32
	for _, cs := range statuses {
		total += cs.RestartCount
	}
	return total
}

// containersDTO renders the sanitized container list (name + image only).
func containersDTO(containers []corev1.Container) []containerDTO {
	if len(containers) == 0 {
		return nil
	}
	out := make([]containerDTO, 0, len(containers))
	for _, c := range containers {
		out = append(out, containerDTO{Name: c.Name, Image: c.Image})
	}
	return out
}

// servicePortsDTO renders the sanitized port list.
func servicePortsDTO(ports []corev1.ServicePort) []servicePort {
	if len(ports) == 0 {
		return nil
	}
	out := make([]servicePort, 0, len(ports))
	for _, p := range ports {
		out = append(out, servicePort{Name: p.Name, Port: p.Port, Protocol: string(p.Protocol)})
	}
	return out
}

// now is a variable so tests can pin the reference time.
var now = func() time.Time { return time.Now() }
