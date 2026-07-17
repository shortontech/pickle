@extends('layouts.guest')

@section('content')
<div class="login-box">
    <div class="login-logo"><a href="{{ route('auth.login') }}"><span class="fw-bold">Pickle</span> AdminLTE</a></div>
    <div class="card">
        <div class="card-body login-card-body">
            <p class="login-box-msg">Sign in to start the session-auth demo</p>
            @if($hasError)
            <div class="alert alert-danger" role="alert">{{ $error }}</div>
            @endif
            <form method="post" action="{{ route('auth.login.store') }}">
                @csrf
                <div class="input-group mb-3"><input class="form-control" type="email" name="email" value="{{ $email }}" aria-label="Email" autocomplete="username" required autofocus><div class="input-group-text">&#64;</div></div>
                <div class="input-group mb-3"><input class="form-control" type="password" name="password" aria-label="Password" autocomplete="current-password" minlength="8" required><div class="input-group-text">•••</div></div>
                <div class="d-grid gap-2"><button class="btn btn-primary" type="submit">Sign in</button></div>
            </form>
            <p class="mt-3 mb-0 text-body-secondary small">Demo credentials: admin&#64;example.test / password</p>
        </div>
    </div>
</div>
@endsection
