# Cron Jobs

Plain Go structs with a `Handle() error` method, registered in a single schedule file. Jobs run in-process as goroutines — no external queue, no separate daemon, no runtime dependency on Pickle.

## Writing a job

Jobs live in `app/jobs/`. A job is any exported struct with a `Handle() error` method.

```go
// app/jobs/expire_sessions.go
package jobs

import (
    "fmt"
    "log"
    "time"

    "myapp/app/models"
)

type ExpireSessionsJob struct{}

func (j ExpireSessionsJob) Handle() error {
    deleted, err := models.QuerySession().
        WhereExpiredBefore(time.Now()).
        Delete()
    if err != nil {
        return fmt.Errorf("expiring sessions: %w", err)
    }
    log.Printf("expired %d sessions", deleted)
    return nil
}
```

Jobs access the database through the generated `models` package, same as controllers. No special injection or job context.

Optionally, override the display name:

```go
func (j ExpireSessionsJob) Name() string {
    return "expire-sessions"
}
```

The default display name is the struct type name (e.g. `ExpireSessionsJob`).

## The schedule file

All scheduled jobs are registered in `schedule/jobs.go`. One file, every piece of scheduled work visible at a glance — same philosophy as `routes/web.go` for HTTP endpoints.

```go
// schedule/jobs.go
package schedule

import (
    "time"

    "myapp/app/jobs"
)

var Schedule = jobs.Cron(func(s *jobs.Scheduler) {
    s.Job("0 * * * *",   jobs.ExpireSessionsJob{})       // every hour
    s.Job("*/5 * * * *", jobs.SyncExchangeRatesJob{})    // every 5 minutes
    s.Job("0 0 * * *",   jobs.SendDailyDigestJob{})      // midnight daily
    s.Job("0 2 * * 0",   jobs.PruneAuditLogsJob{}).
        MaxRetries(3).
        Timeout(5 * time.Minute)                          // weekly, with options
})
```

Standard 5-field cron syntax: `minute hour day-of-month month day-of-week`.

## Options

Options chain on the return value of `s.Job()`:

```go
s.Job("0 * * * *", jobs.RetryableJob{}).
    MaxRetries(3).                  // retry up to 3 times on failure (default: 0)
    RetryDelay(30 * time.Second).   // wait between retries (default: 0)
    Timeout(2 * time.Minute).       // kill if Handle() takes longer (default: no timeout)
    SkipIfRunning()                 // don't start if previous run is still going (default)
```

| Option | Default | Description |
|--------|---------|-------------|
| `MaxRetries(n)` | 0 | Number of retry attempts after a failure |
| `RetryDelay(d)` | 0 | Duration to wait between retries |
| `Timeout(d)` | none | Maximum execution time; job is cancelled if exceeded |
| `SkipIfRunning()` | on | Skip this tick if the previous run hasn't finished |
| `AllowOverlap()` | off | Permit concurrent runs of the same job |

`SkipIfRunning` is the default behavior. Call `AllowOverlap()` to opt out. Overlap prevention uses an in-memory mutex per job entry — sufficient for single-binary deployments.

## Scaffolding

```bash
$ pickle make:job ExpireSessions
  created app/jobs/expire_sessions.go
```

Generates:

```go
package jobs

type ExpireSessionsJob struct{}

func (j ExpireSessionsJob) Handle() error {
    // TODO: implement job logic
    return nil
}
```

Then register it in `schedule/jobs.go`:

```go
s.Job("0 * * * *", jobs.ExpireSessionsJob{})
```

## How it runs

The scheduler starts as a background goroutine when the server starts and shuts down cleanly on SIGINT/SIGTERM. No separate process, no separate binary. Deployment is still a single static binary.

```go
// Generated in commands/pickle_gen.go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()

go schedule.Schedule.Start(ctx)

// ... start HTTP server
```

`Start()` blocks until the context is cancelled. Each registered job fires on its cron schedule in its own goroutine.

## Standalone worker

If you need to run jobs without the HTTP server — a Kubernetes CronJob, a scheduled Lambda, a dedicated worker — wire the scheduler in a separate binary:

```go
// cmd/worker/main.go
package main

import (
    "context"
    "os"
    "os/signal"

    "myapp/schedule"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    schedule.Schedule.Start(ctx)
}
```

Pickle doesn't scaffold this because most projects don't need it.

## Failed job logging

Failed jobs log to stdout via `log.Printf`. The format includes the job type, attempt number, and error:

```
[pickle] job ExpireSessionsJob failed (attempt 1/3): context deadline exceeded
[pickle] job ExpireSessionsJob failed (attempt 2/3): connection refused
[pickle] job ExpireSessionsJob failed (attempt 3/3): connection refused
```

Skipped overlapping runs are also logged:

```
[pickle] job ExpireSessionsJob skipped: previous run still in progress
```

No database table. No email alerts. If you need alerting, wrap the job:

```go
type AlertOnFailure struct {
    Inner jobs.Job
}

func (j AlertOnFailure) Handle() error {
    if err := j.Inner.Handle(); err != nil {
        notifyPagerDuty(err)
        return err
    }
    return nil
}
```

That's just Go.

## CLI commands

| Command | Description |
|---------|-------------|
| `pickle make:job Name` | Scaffold a new job in `app/jobs/` |
| `pickle schedule` | List all registered jobs with their next run times |
| `pickle run:job JobName` | Run a job immediately, outside its schedule |

## Directory layout

```
You write:                          Pickle generates:
app/jobs/                           app/jobs/
  expire_sessions.go                  pickle_gen.go  <- Scheduler, Cron, Job types
  sync_exchange_rates.go
schedule/
  jobs.go
```

`schedule/jobs.go` is user-written and never touched by the generator. `app/jobs/pickle_gen.go` is generated and overwritten on every run.
