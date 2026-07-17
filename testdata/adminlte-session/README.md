# AdminLTE + session-auth test application

This is the active integration fixture for spec 081. It vendors the published
AdminLTE 4.0.0 CSS and JavaScript, uses AdminLTE's real v4 layout structure, and
protects its typed Blade dashboard with Pickle's session auth driver.

The login form submits real credentials to `POST /login`. The application
loads the user from PostgreSQL, verifies the bcrypt password hash, and only
then creates the signed server-side session. Use `admin@example.test` and
`password` for the migration-installed demo administrator.

The upstream assets are pinned, content-addressed, embedded into the Go binary,
and recorded with their npm integrity, SHA-256 hashes, MIT license, and release
provenance under `third_party/adminlte/`. The fixture requires no Node.js or
network access to generate, build, or run.

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
