<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ $page->title }}</title>
    <link rel="stylesheet" href="{{ asset('css/adminlte-compat.css') }}">
</head>
<body class="login-page bg-body-secondary">
    <main aria-labelledby="page-heading">
        <h1 id="page-heading">{{ $page->heading }}</h1>
        @yield('content')
    </main>
</body>
</html>
