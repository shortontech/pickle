package blade

import (
	"strings"
	"testing"
)

func TestParseAndCompileTypedView(t *testing.T) {
	doc, err := Parse("dashboard.blade.php", `<h1>{{ $organization->name }}</h1>@if ($authenticated)<ul>@foreach ($movements as $movement)<li>{{ $movement->quantity }}</li>@endforeach</ul>@else<p>Sign in</p>@endif`)
	if err != nil {
		t.Fatal(err)
	}
	src, err := CompileGo("pickle", []*Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type DashboardData struct", "Authenticated bool", "Name string", "[]struct {", "Quantity string", "html.EscapeString"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated source missing %q:\n%s", want, src)
		}
	}
}

func TestParseRejectsPHPWithLocation(t *testing.T) {
	_, err := Parse("unsafe.blade.php", "hello\n@php echo('no') @endphp")
	if err == nil || !strings.Contains(err.Error(), "unsafe.blade.php:2:1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileRejectsIncompatibleShape(t *testing.T) {
	doc, err := Parse("bad.blade.php", `{{ $items }}@foreach ($items as $item){{ $item->name }}@endforeach`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileGo("pickle", []*Document{doc}); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("unexpected error: %v", err)
	}
}
