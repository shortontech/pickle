package squeeze

import (
	"strings"
	"testing"
)

func TestParseLiveRLSStatus(t *testing.T) {
	input := `users                                    drift fingerprint=abc
  - RLS disabled
  - runtime role has BYPASSRLS
  - unexpected permissive policy legacy_all
messages                                 in-sync fingerprint=abc
`
	got, err := ParseLiveRLSStatus(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected observations: %#v", got)
	}
	if got[0].Enabled || !got[0].Forced || !got[0].RuntimeBypass || !got[0].ManualPermissive || !got[0].Drift {
		t.Fatalf("unexpected drift observation: %#v", got[0])
	}
	if !got[1].Enabled || !got[1].Forced || got[1].Drift {
		t.Fatalf("unexpected in-sync observation: %#v", got[1])
	}
	if !strings.Contains(got[0].Detail, "legacy_all") {
		t.Fatalf("missing detail: %#v", got[0])
	}
}

func TestRunOptionsLiveRLSEvidenceReachesRules(t *testing.T) {
	ctx := &AnalysisContext{LiveRLS: []LiveRLSObservation{{Table: "users", Enabled: false}}}
	findings := ruleRLSNotEnabled(ctx)
	if len(findings) != 1 || findings[0].Rule != "rls_not_enabled" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}
