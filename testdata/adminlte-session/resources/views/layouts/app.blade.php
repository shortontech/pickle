<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="color-scheme" content="light dark">
    <title>{{ $page->title }}</title>
    <link rel="stylesheet" href="{{ asset('vendor/adminlte/adminlte.min.css') }}">
    <link rel="stylesheet" href="{{ asset('css/pickle-adminlte.css') }}">
</head>
<body class="layout-fixed sidebar-expand-lg bg-body-tertiary">
<div class="app-wrapper">
    <nav class="app-header navbar navbar-expand bg-body">
        <div class="container-fluid">
            <ul class="navbar-nav">
                <li class="nav-item">
                    <a class="nav-link" data-lte-toggle="sidebar" href="#" role="button" aria-label="Toggle navigation">
                        <svg width="20" height="20" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M2.5 12a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5m0-4a.5.5 0 0 1 .5-.5h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5"/></svg>
                    </a>
                </li>
                <li class="nav-item d-none d-md-block"><a href="{{ route('dashboard') }}" class="nav-link">Dashboard</a></li>
            </ul>
            <ul class="navbar-nav ms-auto">
                <li class="nav-item"><span class="nav-link demo-user">{{ $user->name }}</span></li>
            </ul>
        </div>
    </nav>
    @include('partials.sidebar')
    <main class="app-main">
        <div class="app-content-header">
            <div class="container-fluid">
                <div class="row">
                    <div class="col-sm-6"><h1 class="mb-0">{{ $page->heading }}</h1></div>
                    <div class="col-sm-6"><ol class="breadcrumb float-sm-end"><li class="breadcrumb-item"><a href="{{ route('dashboard') }}">Home</a></li><li class="breadcrumb-item active">Dashboard</li></ol></div>
                </div>
            </div>
        </div>
        <div class="app-content"><div class="container-fluid">@yield('content')</div></div>
    </main>
    <footer class="app-footer"><strong>Pickle AdminLTE test application</strong><span class="float-end d-none d-sm-inline">Compiled entirely into Go</span></footer>
</div>
<script src="{{ asset('vendor/adminlte/adminlte.min.js') }}"></script>
</body>
</html>
