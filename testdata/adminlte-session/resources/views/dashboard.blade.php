<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ $page->title }}</title>
</head>
<body class="layout-fixed sidebar-expand-lg bg-body-tertiary">
<div class="app-wrapper">
    <nav class="app-header navbar navbar-expand bg-body">
        <span class="navbar-brand">Pickle AdminLTE</span>
    </nav>
    <aside class="app-sidebar bg-body-secondary shadow">
        <div class="sidebar-brand">Warehouse</div>
        <nav class="sidebar-wrapper"><a href="/">Dashboard</a></nav>
    </aside>
    <main class="app-main">
        <header class="app-content-header"><h1>{{ $page->heading }}</h1></header>
        <section class="app-content">
            @if ($authenticated)
                <p>Signed in as {{ $user->name }}</p>
                <div class="row">
                    @foreach ($metrics as $metric)
                        <article class="small-box text-bg-primary">
                            <h2>{{ $metric->value }}</h2>
                            <p>{{ $metric->label }}</p>
                        </article>
                    @endforeach
                </div>
            @else
                <p>Your session has expired.</p>
            @endif
        </section>
    </main>
</div>
</body>
</html>
