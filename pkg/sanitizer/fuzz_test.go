package sanitizer

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-ctx-dumper/pkg/k8s"
)

// FuzzSanitize ensures the sanitizer never panics on arbitrary object
// metadata, annotations, or status payloads.
func FuzzSanitize(f *testing.F) {
	f.Add("name", "key", "value", "pod", "deployment", "service", "event")
	f.Add("", "", "", "", "", "", "")
	f.Add("a|b\nc", "k|v\n", "x=1&y=2", "p", "d", "s", "e")
	f.Fuzz(func(t *testing.T, name, key, val, kind, ns, node, phase string) {
		ann := map[string]string{}
		if key != "" {
			ann[key] = val
		}
		s := &k8s.ClusterSnapshot{
			Pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Annotations: ann},
					Spec:       corev1.PodSpec{NodeName: node},
					Status:     corev1.PodStatus{Phase: corev1.PodPhase(phase)},
				},
			},
			Events: []corev1.Event{
				{ObjectMeta: metav1.ObjectMeta{Name: name}, Type: kind, Reason: key, Message: val},
			},
		}
		Sanitize(s) // must not panic
	})
}
