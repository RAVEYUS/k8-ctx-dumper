package k8s

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestParseResources(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []ResourceKind
		wantErr bool
	}{
		{"default when empty", "", []ResourceKind{ResourcePods, ResourceServices, ResourceDeployments}, false},
		{"single", "pods", []ResourceKind{ResourcePods}, false},
		{"all four", "pods,services,deployments,events", []ResourceKind{ResourcePods, ResourceServices, ResourceDeployments, ResourceEvents}, false},
		{"whitespace and case", " Pods , Deployments ", []ResourceKind{ResourcePods, ResourceDeployments}, false},
		{"dedup", "pods,pods,events", []ResourceKind{ResourcePods, ResourceEvents}, false},
		{"unknown", "pods,secret", nil, true},
		{"only commas", ", ,", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseResources(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseResources(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	now := metav1.Now()

	objs := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.27"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx:1.27"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "ev-late", Namespace: "default"},
			Reason:     "Scheduled",
			Type:       corev1.EventTypeNormal,
			Count:      1,
		},
		&corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: "ev-early", Namespace: "default"},
			Reason:     "Pulled",
			Type:       corev1.EventTypeNormal,
			Count:      3,
		},
	}
	// Set explicit timestamps after construction so sort order is deterministic.
	objs[4].(*corev1.Event).LastTimestamp = metav1.NewTime(now.Add(1))
	objs[5].(*corev1.Event).LastTimestamp = metav1.NewTime(now.Add(0))

	cases := []struct {
		name      string
		client    *fake.Clientset
		resources []ResourceKind
		namespace string
		want      *ClusterSnapshot
	}{
		{
			name:      "all resource kinds in a namespace",
			client:    fake.NewSimpleClientset(objs...),
			resources: []ResourceKind{ResourcePods, ResourceServices, ResourceDeployments, ResourceEvents},
			namespace: "default",
			want: &ClusterSnapshot{
				Namespace:      "default",
				HasPods:        true,
				HasServices:    true,
				HasDeployments: true,
				// Pods are sorted by name; events by LastTimestamp.
				Pods:        []corev1.Pod{*objs[1].(*corev1.Pod), *objs[0].(*corev1.Pod)},
				Services:    []corev1.Service{*objs[2].(*corev1.Service)},
				Deployments: []appsv1.Deployment{*objs[3].(*appsv1.Deployment)},
				Events:      []corev1.Event{*objs[5].(*corev1.Event), *objs[4].(*corev1.Event)},
			},
		},
		{
			name:      "subset of resources",
			client:    fake.NewSimpleClientset(objs...),
			resources: []ResourceKind{ResourcePods, ResourceDeployments},
			namespace: "default",
			want: &ClusterSnapshot{
				Namespace:      "default",
				HasPods:        true,
				HasDeployments: true,
				Pods:           []corev1.Pod{*objs[1].(*corev1.Pod), *objs[0].(*corev1.Pod)},
				Deployments:    []appsv1.Deployment{*objs[3].(*appsv1.Deployment)},
			},
		},
		{
			name:      "all namespaces with no objects",
			client:    fake.NewSimpleClientset(),
			resources: []ResourceKind{ResourcePods, ResourceServices},
			namespace: "",
			want: &ClusterSnapshot{
				Namespace:   "",
				HasPods:     true,
				HasServices: true,
				Pods:        nil,
				Services:    nil,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := NewFetcher(c.client, nil, c.namespace, c.resources)
			got, err := f.Fetch(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Fetch() =\n%#v\nwant\n%#v", got, c.want)
			}
		})
	}
}

func TestFetchEmptyResources(t *testing.T) {
	client := fake.NewSimpleClientset()
	f := NewFetcher(client, nil, "default", []ResourceKind{})
	got, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &ClusterSnapshot{Namespace: "default"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Fetch() = %#v, want %#v", got, want)
	}
}

// TestFetchSurfacesAllErrors verifies that every failing resource type is
// reported, not just the first one encountered (regression test).
func TestFetchSurfacesAllErrors(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "default"}},
	)

	f := NewFetcher(client, nil, "default", []ResourceKind{ResourcePods, ResourceServices, ResourceDeployments})

	// Poison the fake so pod and deployment listing fails.
	client.PrependReactor("*", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom-pods")
	})
	client.PrependReactor("*", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom-deployments")
	})

	_, err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	joined := err.Error()
	for _, want := range []string{"listing pods: boom-pods", "listing deployments: boom-deployments"} {
		if !strings.Contains(joined, want) {
			t.Errorf("error %q does not contain %q", joined, want)
		}
	}
}

// TestFetchNamespaced verifies that a namespace-scoped fetch only returns
// objects from that namespace.
func TestFetchNamespaced(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "team-a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "team-b"}},
	)
	f := NewFetcher(client, nil, "team-a", []ResourceKind{ResourcePods})
	got, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Pods) != 1 || got.Pods[0].Namespace != "team-a" {
		t.Errorf("expected only the team-a pod, got %#v", got.Pods)
	}
}

// TestFetchEventsFallback verifies the core/v1 fallback path of fetchEvents:
// when the events.k8s.io/v1 client cannot be built (nil rest config), events
// are still returned from the core/v1 API.
func TestFetchEventsFallback(t *testing.T) {
	ev := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{Name: "ev", Namespace: "default"},
		Reason:     "Created",
		Type:       corev1.EventTypeNormal,
		Count:      1,
	}
	client := fake.NewSimpleClientset(ev)
	f := NewFetcher(client, nil, "default", []ResourceKind{ResourceEvents})
	got, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Name != "ev" {
		t.Errorf("expected 1 event from core/v1 fallback, got %#v", got.Events)
	}
}

func TestConvertEventsV1(t *testing.T) {
	in := []eventsv1.Event{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"},
			Regarding:  corev1.ObjectReference{Kind: "Pod", Name: "pod-a", Namespace: "default"},
			Type:       corev1.EventTypeWarning,
			Reason:     "BackOff",
			Note:       "Back-off restarting failed container",
			Series:     &eventsv1.EventSeries{Count: 5},
		},
		{
			ObjectMeta:      metav1.ObjectMeta{Name: "b", Namespace: "default"},
			Type:            corev1.EventTypeNormal,
			Reason:          "Scheduled",
			Note:            "Successfully assigned",
			DeprecatedCount: 2,
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
			Type:       corev1.EventTypeNormal,
			Reason:     "Created",
			Note:       "Created container",
		},
	}

	got := convertEventsV1(in)
	want := []corev1.Event{
		{
			ObjectMeta:     metav1.ObjectMeta{Name: "a", Namespace: "default"},
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "pod-a", Namespace: "default"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
			Count:          5,
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default"},
			Type:       corev1.EventTypeNormal,
			Reason:     "Scheduled",
			Message:    "Successfully assigned",
			Count:      2,
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
			Type:       corev1.EventTypeNormal,
			Reason:     "Created",
			Message:    "Created container",
			Count:      1,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("convertEventsV1() =\n%#v\nwant\n%#v", got, want)
	}
}

func int32Ptr(v int32) *int32 { return &v }
