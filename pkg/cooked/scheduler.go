package cooked

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Job is the interface all Pickle jobs must satisfy.
type Job interface {
	Handle() error
}

// JobEntry is a registered job with its schedule and options.
type JobEntry struct {
	Schedule       string
	Job            Job
	maxRetries     int
	retryDelay     time.Duration
	timeout        time.Duration
	allowOverlap   bool
}

// Scheduler collects job registrations.
type Scheduler struct {
	entries []*JobEntry
}

// Cron creates a new Scheduler by invoking the given configuration function.
func Cron(fn func(s *Scheduler)) *Scheduler {
	s := &Scheduler{}
	fn(s)
	return s
}

// Job registers a cron job with the given schedule expression.
func (s *Scheduler) Job(schedule string, job Job) *JobEntry {
	e := &JobEntry{
		Schedule: schedule,
		Job:      job,
	}
	s.entries = append(s.entries, e)
	return e
}

// Entries returns the registered job entries. Used by CLI tools to inspect the schedule.
func (s *Scheduler) Entries() []*JobEntry {
	return s.entries
}

// MaxRetries sets the maximum number of retries on failure.
func (e *JobEntry) MaxRetries(n int) *JobEntry { e.maxRetries = n; return e }

// RetryDelay sets the delay between retries.
func (e *JobEntry) RetryDelay(d time.Duration) *JobEntry { e.retryDelay = d; return e }

// Timeout sets a maximum execution duration. Zero means no timeout.
func (e *JobEntry) Timeout(d time.Duration) *JobEntry { e.timeout = d; return e }

// SkipIfRunning prevents overlapping runs (default behavior; explicit for documentation).
func (e *JobEntry) SkipIfRunning() *JobEntry { e.allowOverlap = false; return e }

// AllowOverlap permits a job to start even if the previous run hasn't finished.
func (e *JobEntry) AllowOverlap() *JobEntry { e.allowOverlap = true; return e }

// Start begins the scheduler loop. It blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	c := cron.New()
	for _, entry := range s.entries {
		entry := entry
		var mu sync.Mutex
		running := false
		c.AddFunc(entry.Schedule, func() {
			if !entry.allowOverlap {
				mu.Lock()
				if running {
					mu.Unlock()
					log.Printf("[pickle] job %T skipped: previous run still in progress", entry.Job)
					return
				}
				running = true
				mu.Unlock()
				defer func() {
					mu.Lock()
					running = false
					mu.Unlock()
				}()
			}
			runJob(entry)
		})
	}
	c.Start()
	<-ctx.Done()
	c.Stop()
}

func runJob(entry *JobEntry) {
	attempts := entry.maxRetries + 1
	for i := 0; i < attempts; i++ {
		var err error
		if entry.timeout > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), entry.timeout)
			done := make(chan error, 1)
			go func() { done <- entry.Job.Handle() }()
			select {
			case err = <-done:
			case <-ctx.Done():
				err = fmt.Errorf("job timed out after %s", entry.timeout)
			}
			cancel()
		} else {
			err = entry.Job.Handle()
		}
		if err == nil {
			return
		}
		log.Printf("[pickle] job %T failed (attempt %d/%d): %v", entry.Job, i+1, attempts, err)
		if i < attempts-1 && entry.retryDelay > 0 {
			time.Sleep(entry.retryDelay)
		}
	}
}
