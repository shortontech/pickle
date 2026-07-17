@extends('layouts.guest')

@section('content')
<div class="login-box">
    <div class="card">
        <div class="card-header"><strong>Pickle AdminLTE</strong></div>
        <div class="card-body">
            <p>This deterministic login is for the local session-auth demo.</p>
            <form method="post" action="/login">
                <button type="submit">Sign in as demo administrator</button>
            </form>
        </div>
    </div>
</div>
@endsection
