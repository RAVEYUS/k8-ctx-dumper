// Package formatter renders a sanitized ClusterSnapshot into a token-optimized
// representation: Markdown tables for human/LLM consumption, or compact JSON
// for programmatic use.
package formatter

import "k8s-ctx-dumper/pkg/k8s"

// Formatter is the output contract for every rendering backend. Implementations
// must be safe for concurrent use (they hold no mutable state).
type Formatter interface {
	// Format renders the snapshot. The snapshot is assumed to be already
	// sanitized; formatters must not mutate it.
	Format(s *k8s.ClusterSnapshot) (string, error)
}

// New returns the formatter registered under the given name. Supported names:
// "markdown" and "json". An unknown name returns a descriptive error.
func New(name string) (Formatter, error) {
	switch name {
	case "markdown":
		return MarkdownFormatter{}, nil
	case "json":
		return JSONFormatter{}, nil
	default:
		return nil, ErrUnknownFormat
	}
}
