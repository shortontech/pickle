package generator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shortontech/pickle/pkg/tickle"
)

// DocsJSON returns the raw JSON string of embedded documentation.
func DocsJSON() string {
	return embedDOCS
}

// ParseDocs parses the embedded documentation JSON.
func ParseDocs() ([]tickle.DocFile, error) {
	var docs []tickle.DocFile
	if err := json.Unmarshal([]byte(embedDOCS), &docs); err != nil {
		return nil, fmt.Errorf("parsing embedded docs: %w", err)
	}
	return docs, nil
}

// FormatDocsMarkdown formats docs as readable markdown, optionally filtering by type name.
func FormatDocsMarkdown(typeName string) (string, error) {
	docs, err := ParseDocs()
	if err != nil {
		return "", err
	}

	if typeName != "" {
		for _, d := range docs {
			if strings.EqualFold(d.Name, typeName) {
				return d.Content, nil
			}
		}
		return "", fmt.Errorf("type %q not found in documentation", typeName)
	}

	// No filter — return all doc names with descriptions
	var b strings.Builder
	b.WriteString("# Pickle Framework Documentation\n\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "- **%s** — %s\n", d.Name, d.Description)
	}
	b.WriteString("\nPass a type name to see full documentation (e.g. Context, Router, QueryBuilder).\n")
	return b.String(), nil
}
