package formatter

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s-ctx-dumper/pkg/k8s"
)

// FuzzMarkdownFormat ensures the Markdown formatter never panics on
// arbitrary snapshot contents (names, images, events, annotations).
func FuzzMarkdownFormat(f *testing.F) {
	f.Add("name", "img:tag", "pod", "default", "Warning", "BackOff", "a|b\nc")
	f.Add("", "", "", "", "", "", "")
	f.Add("x|y", "reg/img:tag", "kind", "ns", "Normal", "Pulled", "line\nwith|pipes")
	f.Fuzz(func(t *testing.T, name, image, kind, ns, etype, reason, msg string) {
		s := &k8s.ClusterSnapshot{
			Context:   "ctx",
			Namespace: ns,
			HasPods:   true,
			Pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: name, Image: image}}},
				},
			},
			Events: []corev1.Event{
				{Type: etype, Reason: reason, Message: msg, InvolvedObject: corev1.ObjectReference{Kind: kind, Name: name}},
			},
		}
		_, _ = (MarkdownFormatter{}).Format(s) // must not panic
	})
}
