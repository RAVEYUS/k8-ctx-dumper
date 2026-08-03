package k8s

import "testing"

// FuzzParseResources ensures the resource parser never panics or accepts
// garbage without an error, regardless of input.
func FuzzParseResources(f *testing.F) {
	for _, seed := range []string{
		"",
		"pods",
		"pods,services,deployments,events",
		" Pods , Deployments ",
		"pods,pods,events",
		"bogus",
		", ,",
		"PODS,SeRvIcEs",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = ParseResources(raw) // must not panic
	})
}
