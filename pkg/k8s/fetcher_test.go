package k8s

import (
	"reflect"
	"testing"
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
