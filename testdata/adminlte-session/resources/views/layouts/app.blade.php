<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ $page->title }}</title>
    <link rel="stylesheet" href="{{ asset('css/adminlte-compat.css') }}">
</head>
<body class="layout-fixed sidebar-expand-lg bg-body-tertiary">
<div class="app-wrapper">
    <nav class="app-header navbar navbar-expand bg-body">
        <span class="navbar-brand">Pickle AdminLTE</span>
    </nav>
    @include('partials.sidebar')
    <main class="app-main">
        <header class="app-content-header"><h1>{{ $page->heading }}</h1></header>
        <section class="app-content">@yield('content')</section>
    </main>
</div>
</body>
</html>
