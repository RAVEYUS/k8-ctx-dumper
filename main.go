// Command k8s-ctx-dumper dumps Kubernetes cluster state as token-optimized
// Markdown or JSON for LLM context windows.
package main

import (
	"os"

	"k8s-ctx-dumper/cmd"
)

func main() {
	if code := cmd.Execute(); code != 0 {
		os.Exit(code)
	}
}
