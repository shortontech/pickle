package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCompilesBladeViewContract(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, contents string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("go.mod", "module example.test/views\n\ngo 1.24\n")
	mustWrite("resources/views/layouts/app.blade.php", `<html><body>@include('partials.nav')@yield('content')</body></html>`)
	mustWrite("resources/views/partials/nav.blade.php", `<nav>{{ $page->title }}</nav>`)
	mustWrite("resources/views/dashboard.blade.php", `@extends('layouts.app')@section('content')@if ($authenticated)<p>{{ $user->name }}</p>@endif@endsection`)
	for _, dir := range []string{"app/http", "app/http/requests", "app/models"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project, err := DetectProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Generate(project, filepath.Join("..")); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(root, "app/http/views_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type DashboardData struct", "Authenticated bool", "html.EscapeString(data.Page.Title)", "renderedViewResponse"} {
		if !strings.Contains(string(generated), want) {
			t.Errorf("views_gen.go missing %q", want)
		}
	}
}
