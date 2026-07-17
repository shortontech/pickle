package blade

import (
	"strings"
	"testing"
)

func mustParseView(t *testing.T, name, source string) *Document {
	t.Helper()
	document, err := Parse(name, source)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestResolveLayoutSectionsAndInclude(t *testing.T) {
	documents := []*Document{
		mustParseView(t, "layouts/app.blade.php", `<html>@include('partials.nav')<main>@yield('content')</main></html>`),
		mustParseView(t, "partials/nav.blade.php", `<nav>{{ $organization->name }}</nav>`),
		mustParseView(t, "dashboard.blade.php", `@extends('layouts.app')@section('content')<h1>{{ $page->title }}</h1>@endsection`),
	}
	resolved, err := Resolve(documents)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Name != "dashboard.blade.php" {
		t.Fatalf("resolved = %#v", resolved)
	}
	src, err := CompileGo("pickle", documents)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type DashboardData", "data.Organization.Name", "data.Page.Title", "<nav>", "<main>"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated source missing %q:\n%s", want, src)
		}
	}
}

func TestResolveReportsMissingSection(t *testing.T) {
	documents := []*Document{
		mustParseView(t, "layout.blade.php", `@yield('content')`),
		mustParseView(t, "page.blade.php", `@extends('layout')`),
	}
	_, err := Resolve(documents)
	if err == nil || !strings.Contains(err.Error(), `missing section "content"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveReportsDependencyCycle(t *testing.T) {
	documents := []*Document{
		mustParseView(t, "a.blade.php", `@include('b')`),
		mustParseView(t, "b.blade.php", `@include('a')`),
	}
	_, err := Resolve(documents)
	if err == nil || !strings.Contains(err.Error(), "b.blade.php:1:1") || !strings.Contains(err.Error(), "a.blade.php -> b.blade.php -> a.blade.php") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveReportsMissingDependency(t *testing.T) {
	document := mustParseView(t, "page.blade.php", `@include('missing')`)
	_, err := Resolve([]*Document{document})
	if err == nil || !strings.Contains(err.Error(), "page.blade.php:1:1") || !strings.Contains(err.Error(), `view dependency "missing.blade.php" does not exist`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
