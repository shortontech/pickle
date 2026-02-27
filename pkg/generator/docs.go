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
func ParseDocs() ([]tickle.TypeDoc, error) {
	var docs []tickle.TypeDoc
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

	var b strings.Builder
	for _, td := range docs {
		if typeName != "" && !strings.EqualFold(td.Name, typeName) {
			continue
		}

		fmt.Fprintf(&b, "## %s\n", td.Name)
		if td.Doc != "" {
			fmt.Fprintf(&b, "%s\n", td.Doc)
		}
		b.WriteString("\n")

		for _, m := range td.Methods {
			fmt.Fprintf(&b, "### %s\n", m.Signature)
			if m.Doc != "" {
				fmt.Fprintf(&b, "%s\n", m.Doc)
			}
			b.WriteString("\n")
		}
	}

	result := b.String()
	if result == "" && typeName != "" {
		return "", fmt.Errorf("type %q not found in documentation", typeName)
	}
	return result, nil
}
