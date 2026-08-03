package formatter

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-ctx-dumper/pkg/k8s"
	"k8s-ctx-dumper/pkg/sanitizer"
)

// testSnapshot builds a small but representative sanitized snapshot covering
// all resource types and one event.
func testSnapshot() *k8s.ClusterSnapshot {
	now := time.Now()
	s := &k8s.ClusterSnapshot{
		Context:        "prod-cluster",
		Namespace:      "default",
		HasPods:        true,
		HasServices:    true,
		HasDeployments: true,
		Pods: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "web-7f8d",
					Namespace:         "default",
					CreationTimestamp: metav1.NewTime(now.Add(-48 * time.Hour)),
				},
				Spec: corev1.PodSpec{
					NodeName: "node-1",
					Containers: []corev1.Container{
						{Name: "web", Image: "nginx:1.25"},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					PodIP: "10.244.0.5",
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "web", RestartCount: 0, Ready: true, State: corev1.ContainerState{}},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "pay-98ab",
					Namespace:         "default",
					CreationTimestamp: metav1.NewTime(now.Add(-4 * time.Hour)),
				},
				Spec: corev1.PodSpec{
					NodeName: "node-2",
					Containers: []corev1.Container{
						{Name: "pay", Image: "registry.example.com/pay:2.1.0"},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					PodIP: "10.244.0.8",
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:         "pay",
							RestartCount: 12,
							State:        corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
						},
					},
				},
			},
		},
		Services: []corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "web-svc", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.96.0.1",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
		Deployments: []appsv1.Deployment{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "web",
					Namespace:         "default",
					CreationTimestamp: metav1.NewTime(now.Add(-48 * time.Hour)),
				},
				Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
				Status: appsv1.DeploymentStatus{
					ReadyReplicas:     2,
					UpdatedReplicas:   2,
					AvailableReplicas: 2,
				},
			},
		},
		Events: []corev1.Event{
			{
				ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
				Type:           "Warning",
				Reason:         "BackOff",
				Message:        "Back-off restarting failed container",
				Count:          12,
				LastTimestamp:  metav1.NewTime(now.Add(-10 * time.Minute)),
				InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pay-98ab"},
			},
		},
	}
	return s
}

func int32Ptr(v int32) *int32 { return &v }

func TestMarkdownFormatter(t *testing.T) {
	s := testSnapshot()
	sanitizer.Sanitize(s)
	out, err := (MarkdownFormatter{}).Format(s)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}

	for _, want := range []string{
		"# Cluster Snapshot",
		"**Context:** `prod-cluster`",
		"**Scope:** `default`",
		"## Pods (2)",
		"| Name | Status | Restarts | IP | Node | Age | Containers |",
		"web-7f8d",
		"CrashLoopBackOff",
		"nginx:1.25",
		"pay:2.1.0",
		"## Services (1)",
		"web-svc",
		"10.96.0.1",
		"## Deployments (1)",
		"| Name | Ready | Up-to-date | Available | Strategy | Age |",
		"## Recent Events / Errors",
		"- [Warning] BackOff (12x, 10m ago): Back-off restarting failed container on Pod pay-98ab",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q\n---\n%s", want, out)
		}
	}
}

func TestMarkdownFormatterEmptySnapshot(t *testing.T) {
	s := &k8s.ClusterSnapshot{Context: "c", Namespace: "default", HasPods: true, HasServices: true, HasDeployments: true}
	out, err := (MarkdownFormatter{}).Format(s)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(out, "## Pods (0)") {
		t.Errorf("empty pods section missing: %s", out)
	}
	if strings.Contains(out, "## Recent Events") {
		t.Errorf("events section rendered when none present: %s", out)
	}
}

func TestJSONFormatterRoundTrip(t *testing.T) {
	s := testSnapshot()
	sanitizer.Sanitize(s)
	out, err := (JSONFormatter{}).Format(s)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.HasPrefix(out, "{") || !strings.HasSuffix(out, "}") {
		t.Errorf("output is not a JSON object: %q", out)
	}
	// Verify the snake_case wire shape and that noise fields are gone.
	for _, want := range []string{
		`"context":"prod-cluster"`,
		`"pods":[`,
		`"deployments":[`,
		`"restartCount":12`,
		`"phase":"Running"`,
		`"podIP":"10.244.0.8"`,
		`"nodeName":"node-1"`,
		`"clusterIP":"10.96.0.1"`,
		`"events":[`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output missing %q\n---\n%s", want, out)
		}
	}
	// Sanitized fields must not appear in the wire payload.
	for _, noise := range []string{`"managedFields"`, `"uid"`, `"resourceVersion"`, `"imageID"`} {
		if strings.Contains(out, noise) {
			t.Errorf("JSON output contains sanitized noise %q\n---\n%s", noise, out)
		}
	}
}

func TestFormatterNew(t *testing.T) {
	if _, err := New("markdown"); err != nil {
		t.Errorf("markdown: %v", err)
	}
	if _, err := New("json"); err != nil {
		t.Errorf("json: %v", err)
	}
	if _, err := New("yaml"); err == nil {
		t.Errorf("expected error for unknown format yaml")
	}
}

func TestImageTail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"nginx:1.25", "nginx:1.25"},
		{"registry.example.com/pay:2.1.0", "pay:2.1.0"},
		{"ghcr.io/org/app@sha256:abcd", "app"},
		{"", ""},
	}
	for _, c := range cases {
		if got := imageTail(c.in); got != c.want {
			t.Errorf("imageTail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
