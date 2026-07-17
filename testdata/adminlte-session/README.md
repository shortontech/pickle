# AdminLTE + session-auth test application

This is the active integration fixture for spec 081. It deliberately starts
small: an AdminLTE-shaped dashboard is authored as `*.blade.php`, compiled to a
typed Go renderer, and protected by Pickle's session auth driver.

The CSS in this first slice is a tiny Pickle-owned compatibility fixture using
AdminLTE's public class vocabulary. It is not the upstream AdminLTE
distribution. The pinned upstream assets, license/provenance manifest,
content-addressed embedding, and scaffold installer land in subsequent 081
slices.

## Run it

From this directory:

```bash
cp .env.example .env
docker compose up -d --wait
../../pickle generate
go run ./cmd/server migrate
go run ./cmd/server
```

Open <http://localhost:18081/login>, use the deterministic demo login, and the
browser will receive a database-backed Pickle session before redirecting to the
dashboard. `SESSION_SECURE_COOKIE=false` is intentionally limited to this local
HTTP fixture; deployed HTTPS applications should retain the secure default.

Stop and remove the demo database with:

```bash
docker compose down -v
```
