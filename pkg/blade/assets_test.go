package blade

import (
	"strings"
	"testing"
)

func TestCompileAssetsGoIsContentAddressed(t *testing.T) {
	src, manifest, err := CompileAssetsGo("pickle", []AssetSource{{Name: "css/app.css", Body: []byte("body{}")}})
	if err != nil {
		t.Fatal(err)
	}
	url := manifest["css/app.css"]
	if !strings.HasPrefix(url, "/assets/app.") || !strings.HasSuffix(url, ".css") {
		t.Fatalf("URL = %q", url)
	}
	for _, want := range []string{"max-age=31536000, immutable", "X-Content-Type-Options", "If-None-Match", "sha256-", "renderedAssetResponse"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated source missing %q", want)
		}
	}
}

func TestCompileAssetsGoRejectsTraversal(t *testing.T) {
	_, _, err := CompileAssetsGo("pickle", []AssetSource{{Name: "../secret", Body: []byte("no")}})
	if err == nil || !strings.Contains(err.Error(), "invalid asset path") {
		t.Fatalf("unexpected error: %v", err)
	}
}
