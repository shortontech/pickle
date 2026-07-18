package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateSeederModelGlueTransformsEncryptedAndSealedColumns(t *testing.T) {
	table := &schema.Table{Name: "users", Columns: []*schema.Column{
		{Name: "email", Type: schema.String, IsEncrypted: true},
		{Name: "token", Type: schema.Text, IsSealed: true},
	}}
	source, err := GenerateSeederModelGlue("models", []*schema.Table{table}, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{"func TransformSeedValues", `case "users":`, `delete(out, "email")`, `out["email_encrypted"]`, "encryptSIV", "encryptGCM", "EncryptionKeyNext"} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated transformer missing %q:\n%s", want, text)
		}
	}
	exported, err := GenerateSeederModelGlue("models", []*schema.Table{table}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exportedEncryptionKey", "encryptDeterministic", "encryptRandom"} {
		if !strings.Contains(string(exported), want) {
			t.Fatalf("exported transformer missing %q", want)
		}
	}
}
