@extends('layouts.app')

@section('content')
<div class="row">
    <div class="col-lg-3 col-6">
        <div class="small-box text-bg-primary">
            <div class="inner"><h3>{{ $orders->value }}</h3><p>Open orders</p></div>
            <svg class="small-box-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M7 4V2h10v2h3a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2zm2 0h6V3H9zm-3 6h12V8H6zm0 4h8v-2H6zm0 4h10v-2H6z"/></svg>
            <a href="#" class="small-box-footer link-light">View orders <span aria-hidden="true">→</span></a>
        </div>
    </div>
    <div class="col-lg-3 col-6">
        <div class="small-box text-bg-success">
            <div class="inner"><h3>{{ $shipments->value }}</h3><p>Shipments today</p></div>
            <svg class="small-box-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M3 6h11v9h2.5l2-3H22v6h-2a3 3 0 0 1-6 0H9a3 3 0 0 1-6 0H1V8a2 2 0 0 1 2-2m14 8h3v-1h-2.5zM6 16a2 2 0 1 0 0 4 2 2 0 0 0 0-4m11 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4"/></svg>
            <a href="#" class="small-box-footer link-light">View shipments <span aria-hidden="true">→</span></a>
        </div>
    </div>
    <div class="col-lg-3 col-6">
        <div class="small-box text-bg-warning">
            <div class="inner"><h3>{{ $inventory->value }}</h3><p>Low-stock items</p></div>
            <svg class="small-box-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 2 1 21h22zm0 5 6.9 12H5.1zM11 10h2v5h-2zm0 7h2v2h-2z"/></svg>
            <a href="#" class="small-box-footer link-dark">Review inventory <span aria-hidden="true">→</span></a>
        </div>
    </div>
    <div class="col-lg-3 col-6">
        <div class="small-box text-bg-danger">
            <div class="inner"><h3>{{ $suppliers->value }}</h3><p>Supplier alerts</p></div>
            <svg class="small-box-icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M16 11c1.66 0 3-1.34 3-3s-1.34-3-3-3-3 1.34-3 3 1.34 3 3 3M8 11c1.66 0 3-1.34 3-3S9.66 5 8 5 5 6.34 5 8s1.34 3 3 3m0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5C15 14.17 10.33 13 8 13m8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5"/></svg>
            <a href="#" class="small-box-footer link-light">Review suppliers <span aria-hidden="true">→</span></a>
        </div>
    </div>
</div>

<div class="row">
    <div class="col-lg-8">
        <div class="card card-primary card-outline mb-4">
            <div class="card-header"><h3 class="card-title">Warehouse overview</h3></div>
            <div class="card-body"><p class="mb-0">This page is rendered from typed <code>*.blade.php</code>, compiled into Go, and served with embedded AdminLTE assets.</p></div>
        </div>
    </div>
    <div class="col-lg-4">
        <div class="card mb-4">
            <div class="card-header"><h3 class="card-title">Session</h3></div>
            <div class="card-body"><p>Signed in as <strong>{{ $user->name }}</strong></p>
                <form method="post" action="{{ route('logout') }}"><input type="hidden" name="_token" value="{{ $csrf_token }}"><button class="btn btn-outline-danger" type="submit">Sign out</button></form>
            </div>
        </div>
    </div>
</div>
@endsection
