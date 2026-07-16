package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateRowPolicyRegistryIncludesManagedDDL(t *testing.T) {
	src, err := GenerateRowPolicyRegistry("policies", []PostgresRowPolicyPlan{{Table: "private.messages", Enable: true, Force: true, Policies: []GeneratedPostgresRowPolicy{{Name: "pickle_messages_select_123", Table: "private.messages", Command: schema.RLSSelect, Using: "TRUE"}}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{"GeneratedRowPolicyDDL", `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, `DROP POLICY IF EXISTS`, `CREATE POLICY`, `GeneratedRowPolicyDesired`, `private.messages`} {
		if !strings.Contains(text, want) {
			t.Errorf("generated source missing %q:\n%s", want, text)
		}
	}
}
