@extends('layouts.app')

@section('content')
    @if ($authenticated)
        <p>Signed in as {{ $user->name }}</p>
        <form method="post" action="/logout">
            <input type="hidden" name="_token" value="{{ $csrf_token }}">
            <button type="submit">Sign out</button>
        </form>
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
@endsection
