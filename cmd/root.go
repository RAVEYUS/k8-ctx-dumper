package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd is the base command. It holds the persistent flags that apply to
// every subcommand (kubeconfig, namespace, context) and prints usage when
// invoked without a subcommand.
var rootCmd = &cobra.Command{
	Use:   "k8s-ctx-dumper",
	Short: "Dump Kubernetes cluster state as token-efficient Markdown or JSON",
	Long: `k8s-ctx-dumper queries a Kubernetes cluster via client-go, strips API
noise (managedFields, timestamps, low-value annotations) and formats the
result as token-optimized Markdown or JSON, suitable for LLM context windows.

Example:
  k8s-ctx-dumper dump --namespace myapp --format markdown --copy`,
	SilenceUsage:  true, // don't dump the full help on every runtime error
	SilenceErrors: true, // main() decides how to present the error
}

// Persistent flags shared by all subcommands.
var (
	kubeconfig string
	namespace  string
	// kubeContext is the --context flag value (the context package import is
	// needed elsewhere in this package, so the variable gets a distinct name).
	kubeContext string
)

// Execute runs the root command and returns the exit code that main() should
// use: 0 on success, 1 on any error (including unknown flags). Errors are
// printed to stderr here so main() stays a thin exit wrapper.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", "",
		"path to the kubeconfig file (default: $KUBECONFIG or ~/.kube/config)")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default",
		"namespace to dump; use \"all\" for all namespaces")
	rootCmd.PersistentFlags().StringVar(&kubeContext, "context", "",
		"kubeconfig context to use (default: current context)")
}
