package sanitizer

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-ctx-dumper/pkg/k8s"
)

func TestSanitizeStripsManagedFieldsAndMetadata(t *testing.T) {
	s := &k8s.ClusterSnapshot{
		Pods: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "web-1",
					Namespace:         "default",
					UID:               "abc-123",
					ResourceVersion:   "12345",
					Generation:        3,
					SelfLink:          "/api/v1/pods/web-1",
					ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
					CreationTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
					Annotations: map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": "{\"big\":\"blob\"}",
						"kubernetes.io/service-account.name":               "default",
						"some/internal-annotation":                         "noise",
					},
				},
			},
		},
	}

	Sanitize(s)

	p := s.Pods[0]
	if p.UID != "" || p.ResourceVersion != "" || p.Generation != 0 || p.SelfLink != "" {
		t.Errorf("metadata identifiers not stripped: uid=%q rv=%q gen=%d selflink=%q",
			p.UID, p.ResourceVersion, p.Generation, p.SelfLink)
	}
	if len(p.ManagedFields) != 0 {
		t.Errorf("managedFields not stripped: %+v", p.ManagedFields)
	}
	// The last-applied-configuration blob is dropped, service-account.name is
	// aliased, and unknown annotations are removed.
	if _, ok := p.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Errorf("last-applied-configuration annotation not dropped")
	}
	if p.Annotations["serviceaccount"] != "default" {
		t.Errorf("service-account.name not aliased: %+v", p.Annotations)
	}
	if _, ok := p.Annotations["some/internal-annotation"]; ok {
		t.Errorf("unknown annotation not dropped: %+v", p.Annotations)
	}
}

func TestSanitizeDropsEmptyContainers(t *testing.T) {
	s := &k8s.ClusterSnapshot{
		Pods: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "p"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx:1.25"},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app", Image: "nginx:1.25", ImageID: "docker://sha256:xxx", ContainerID: "containerd://abc"},
					},
				},
			},
		},
	}

	Sanitize(s)

	p := s.Pods[0]
	if len(p.Spec.Containers) != 1 {
		t.Fatalf("containers lost: %+v", p.Spec.Containers)
	}
	if p.Spec.Containers[0].Image != "nginx:1.25" {
		t.Errorf("image not preserved: %+v", p.Spec.Containers[0])
	}
	cs := p.Status.ContainerStatuses[0]
	if cs.ImageID != "" || cs.ContainerID != "" {
		t.Errorf("runtime ids not stripped: imageID=%q containerID=%q", cs.ImageID, cs.ContainerID)
	}
}

func TestSanitizeNormalizesEmptyEventType(t *testing.T) {
	s := &k8s.ClusterSnapshot{
		Events: []corev1.Event{
			{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		},
	}
	Sanitize(s)
	if s.Events[0].Type != "Normal" {
		t.Errorf("empty event type not normalized: %q", s.Events[0].Type)
	}
}

func TestDurationToHuman(t *testing.T) {
	now := time.Now()
	cases := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{2 * time.Hour, "120m"}, // kubectl's HumanDuration formats 2h as 120m
		{4 * time.Hour, "4h"},
		{2 * 24 * time.Hour, "2d"}, // exactly 48h renders as 2d
		{3 * 24 * time.Hour, "3d"},
	}
	for _, c := range cases {
		got := DurationToHuman(metav1.NewTime(now.Add(-c.age)), now)
		if got != c.want {
			t.Errorf("DurationToHuman(%s) = %q, want %q", c.age, got, c.want)
		}
	}
	if got := DurationToHuman(metav1.Time{}, now); got != "unknown" {
		t.Errorf("zero time = %q, want %q", got, "unknown")
	}
}
