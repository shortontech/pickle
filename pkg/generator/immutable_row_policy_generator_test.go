package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestImmutableLogicalWritesUseTheirOwnRowPolicy(t *testing.T) {
	table := &schema.Table{Name: "messages", IsImmutable: true, HasSoftDelete: true, Columns: []*schema.Column{{Name: "id", Type: schema.UUID}, {Name: "version_id", Type: schema.UUID}, {Name: "deleted_at", Type: schema.Timestamp, IsNullable: true}}}
	var src bytes.Buffer
	generateImmutableMethods(&src, table, "MessageQuery", "Message")
	text := src.String()
	for _, want := range []string{`evaluateRowPolicyRecord("messages", "update_new"`, `checkExistingPolicy("update_old", model.ID)`, `checkExistingPolicy("delete", model.ID)`, `createWithPolicyOperation(model, "")`, `tx.WithPolicyContext(*q.policyContext)`} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated immutable query missing %q\n%s", want, text)
		}
	}
}
