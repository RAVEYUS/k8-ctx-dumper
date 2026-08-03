// Package k8s wraps the Kubernetes client-go SDK: it builds a clientset from
// a kubeconfig or in-cluster configuration, then concurrently fetches the
// requested resource lists into a ClusterSnapshot for downstream processing.
package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// clientConfig holds the settings needed to construct a Kubernetes client.
// It is assembled from persistent CLI flags in the cmd package.
type clientConfig struct {
	// KubeconfigPath is an explicit path to a kubeconfig file. When empty,
	// the standard KUBECONFIG env var / ~/.kube/config resolution applies.
	KubeconfigPath string

	// ContextName optionally pins a specific context from the kubeconfig.
	// When empty, the current context of the kubeconfig is used.
	ContextName string
}

// NewClient builds a *kubernetes.Clientset for the given configuration.
//
// Resolution order:
//  1. Out-of-cluster: kubeconfig path (flag) -> KUBECONFIG env -> ~/.kube/config.
//  2. In-cluster fallback: when no kubeconfig can be loaded, rest.InClusterConfig()
//     is attempted, so the binary also works inside a Pod with a service account.
//
// A *rest.Config is also returned so callers can derive extra clients (e.g. the
// events API group) from the exact same configuration, including its warning
// handling and auth settings.
func NewClient(kubeconfigPath, contextName string) (*kubernetes.Clientset, *rest.Config, error) {
	cfg := &clientConfig{
		KubeconfigPath: kubeconfigPath,
		ContextName:    contextName,
	}

	restCfg, err := outOfClusterConfig(cfg)
	if err != nil {
		// No usable kubeconfig: fall back to in-cluster configuration so the
		// tool keeps working when run from inside a cluster (e.g. a Job).
		restCfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, fmt.Errorf(
				"no kubeconfig found (%w) and in-cluster config unavailable (%w); "+
					"set --kubeconfig or the KUBECONFIG environment variable", outOfClusterErr(err), err)
		}
	}

	// Surface non-fatal API warnings (deprecations etc.) as log output instead
	// of dropping them silently.
	restCfg.WarningHandler = rest.NewWarningWriter(os.Stderr, rest.WarningWriterOptions{Deduplicate: true})

	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("building kubernetes client: %w", err)
	}
	return client, restCfg, nil
}

// outOfClusterConfig loads a *rest.Config from a kubeconfig. It implements the
// standard kubectl precedence: explicit flag path, then KUBECONFIG, then the
// default ~/.kube/config location.
func outOfClusterConfig(cfg *clientConfig) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	if cfg.KubeconfigPath != "" {
		loadingRules.ExplicitPath = cfg.KubeconfigPath
	} else if env := os.Getenv("KUBECONFIG"); env != "" {
		// NewDefaultClientConfigLoadingRules already honours KUBECONFIG; this
		// branch exists so an explicit flag still wins over the environment.
		loadingRules.ExplicitPath = env
	} else if home := homedir.HomeDir(); home != "" {
		// Set explicitly (rather than relying on the default) so the "no
		// kubeconfig found" error message can name the file we looked for.
		loadingRules.ExplicitPath = filepath.Join(home, ".kube", "config")
	}

	// The default overrides keep --context working the way kubectl users expect.
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.ContextName != "" {
		overrides.CurrentContext = cfg.ContextName
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// outOfClusterErr unwraps the error into a stable message for the combined
// error string in NewClient.
func outOfClusterErr(err error) error {
	return fmt.Errorf("out-of-cluster config: %v", err)
}
