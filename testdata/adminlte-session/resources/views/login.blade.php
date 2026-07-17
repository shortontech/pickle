@extends('layouts.guest')

@section('content')
<div class="login-box">
    <div class="login-logo"><a href="/"><span class="fw-bold">Pickle</span> AdminLTE</a></div>
    <div class="card">
        <div class="card-body login-card-body">
            <p class="login-box-msg">Sign in to start the session-auth demo</p>
            <form method="post" action="/login">
                <div class="input-group mb-3"><input class="form-control" type="email" value="admin&#64;example.test" aria-label="Email" disabled><div class="input-group-text">&#64;</div></div>
                <div class="input-group mb-3"><input class="form-control" type="password" value="password" aria-label="Password" disabled><div class="input-group-text">•••</div></div>
                <div class="d-grid gap-2"><button class="btn btn-primary" type="submit">Sign in as demo administrator</button></div>
            </form>
        </div>
    </div>
</div>
@endsection
