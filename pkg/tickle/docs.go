package tickle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocFile holds extracted documentation from a markdown file.
type DocFile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// ExtractDocs reads all .md files from docsDir. Each file's first line
// is expected to be a "# Title" header (the name), and the first non-empty
// line following it is the description. The full content is preserved.
func ExtractDocs(docsDir string) ([]DocFile, error) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", docsDir, err)
	}

	var docs []DocFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(docsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		content := string(data)
		lines := strings.Split(content, "\n")

		name := strings.TrimSuffix(entry.Name(), ".md")
		description := ""

		// First line should be "# Title"
		if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
			name = strings.TrimPrefix(lines[0], "# ")
		}

		// First non-empty line after the header is the description
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				description = trimmed
				break
			}
		}

		docs = append(docs, DocFile{
			Name:        name,
			Description: description,
			Content:     content,
		})
	}

	return docs, nil
}
