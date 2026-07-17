# Blade views (experimental)

Pickle can compile a safe, typed subset of Laravel-shaped `*.blade.php` files
into ordinary Go. PHP is never invoked, embedded, or available as a fallback.
This feature is under active development in spec 081.

## Named routes

Static named routes can be used for links, form actions, and current-route
navigation state:

```blade
<a href="{{ route('dashboard') }}">Dashboard</a>

@routeIs('dashboard')
    <span class="active">Dashboard</span>
@endrouteIs
```

Route names are static and compiled into calls against the request's router.
Dynamic route names and arbitrary PHP are never evaluated. Parameterized Blade
route generation is reserved for a later typed-expression slice; Go code can
use `ctx.RouteURL(name, pickle.RouteParams{...})` today.

## Source files

Place authored templates under `resources/views/` and run `pickle generate`.
Pickle currently emits `app/http/views_gen.go`. The generated renderer and its
named data contract live in the application's HTTP package:

```go
data := pickle.DashboardData{Authenticated: true}
data.Page.Title = "Warehouse"
data.User.Name = "Avery"
return pickle.Dashboard(ctx, data)
```

The contract is inferred from use. A path used by `@if` becomes `bool`; a
collection used by `@foreach` becomes a typed slice; escaped output becomes
`string`. Incompatible uses fail generation.

## Implemented language slice

The first implementation supports literal HTML, Blade comments, escaped paths
such as `{{ $user->name }}`, `@if`/`@else`/`@endif`, and
`@foreach ($items as $item)`/`@endforeach`. Static assets use the compiler
intrinsic `{{ asset('css/app.css') }}`.

Static composition uses Laravel-shaped names:

```blade
@extends('layouts.app')
@section('content')
    @include('partials.summary')
@endsection
```

Layouts place sections with `@yield('content')`. Names are resolved at
generation time; missing dependencies, duplicate sections, missing yields,
content outside sections, and include/layout cycles are generation errors.

Expressions are paths, not PHP expressions. Arbitrary calls, raw output,
unknown directives, `<?php`, `<?=`, `@php`, and `@endphp` fail generation with
a source location.

`{{ ... }}` is HTML-escaped. Generated renderers use a package-private response
body marker, so ordinary controller strings cannot opt out of JSON serialization
or mark request data as trusted HTML.

Files under `resources/assets/` are emitted into the Go binary under SHA-256
content-addressed URLs. The generated `PickleAsset` handler serves only manifest
entries and adds a strong ETag, immutable caching, an explicit content type, and
`X-Content-Type-Options: nosniff`. Register it through a controller route using
the single-segment `/assets/:asset` pattern.

## AdminLTE and session fixture

`testdata/adminlte-session` is the active integration fixture. It contains an
AdminLTE-shaped responsive dashboard, selects `AUTH_DRIVER=session`, creates and
destroys sessions through Pickle's session driver, and protects the dashboard
with auth middleware.

Its current stylesheet is a small Pickle-owned class-compatibility fixture, not
the upstream AdminLTE distribution. Pinned upstream assets and provenance,
layout/components, scaffold selection flags, and the rest of spec 081 remain to
be implemented.
