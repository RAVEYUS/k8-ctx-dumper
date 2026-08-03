package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"k8s-ctx-dumper/pkg/formatter"
	"k8s-ctx-dumper/pkg/k8s"
	"k8s-ctx-dumper/pkg/sanitizer"
)

// dump command flags.
var (
	resourcesFlag string
	formatFlag    string
	copyFlag      bool
	outputFlag    string
)

// timeout is the maximum time the whole fetch+format pipeline may take. It
// bounds the concurrent API calls so a hung cluster fails fast instead of
// blocking forever.
const timeout = 60 * time.Second

// dumpCmd performs the core workflow: build a client, fetch the requested
// resources concurrently, sanitize, format, then emit the result.
var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump cluster state (pods, services, deployments, events)",
	Long: `Fetches the requested Kubernetes resources, strips API noise, and renders
the result as token-optimized Markdown or compact JSON.

Examples:
  k8s-ctx-dumper dump
  k8s-ctx-dumper dump -n all --resources pods,deployments
  k8s-ctx-dumper dump --format json --copy
  k8s-ctx-dumper dump --kubeconfig ~/other/.kube/config --context prod`,
	Args: cobra.NoArgs,
	RunE: runDump,
}

// runDump is the command handler. Errors are returned (not logged) so the
// root command's error handling stays the single exit path.
func runDump(cmd *cobra.Command, args []string) error {
	resources, err := k8s.ParseResources(resourcesFlag)
	if err != nil {
		return err
	}

	// "all" is the user-facing alias for dumping every namespace; internally
	// an empty namespace string means NamespaceAll to the client-go listers.
	ns := namespace
	if ns == "all" {
		ns = ""
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	client, restCfg, err := k8s.NewClient(kubeconfig, kubeContext)
	if err != nil {
		return err
	}

	fetcher := k8s.NewFetcher(client, restCfg, ns, resources)
	snapshot, err := fetcher.Fetch(ctx)
	if err != nil {
		return err
	}

	// The snapshot carries the resolved context name (explicit flag, or the
	// kubeconfig current-context) so the output stays accurate when none was
	// pinned. Failure to resolve is not fatal: the dump still succeeds.
	snapshot.Context = resolvedCurrentContext()

	sanitizer.Sanitize(snapshot)

	f, err := formatter.New(formatFlag)
	if err != nil {
		return err
	}
	out, err := f.Format(snapshot)
	if err != nil {
		return err
	}

	if outputFlag != "" {
		if err := os.WriteFile(outputFlag, []byte(out), 0o644); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}
	}
	fmt.Fprint(cmd.OutOrStdout(), out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if copyFlag {
		if err := clipboard.WriteAll(out); err != nil {
			return fmt.Errorf("copying to clipboard: %w", err)
		}
	}
	return nil
}

// resolvedCurrentContext returns the context name the kubeconfig resolution
// would pick: the explicit --context flag when set, otherwise the kubeconfig's
// current-context. An empty string is returned when it cannot be determined.
func resolvedCurrentContext() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	raw, err := loadingRules.Load()
	if err != nil {
		return ""
	}
	if kubeContext != "" {
		return kubeContext
	}
	return raw.CurrentContext
}

func init() {
	dumpCmd.Flags().StringVarP(&resourcesFlag, "resources", "r", "pods,services,deployments",
		"comma-separated resources to dump (pods, services, deployments, events)")
	dumpCmd.Flags().StringVarP(&formatFlag, "format", "f", "markdown",
		"output format: markdown or json")
	dumpCmd.Flags().BoolVarP(&copyFlag, "copy", "c", false,
		"copy the output to the system clipboard")
	dumpCmd.Flags().StringVarP(&outputFlag, "output", "o", "",
		"write the output to this file as well as stdout")
	rootCmd.AddCommand(dumpCmd)
}
