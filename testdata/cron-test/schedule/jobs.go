package schedule

import (
	"time"

	"cron-test/app/jobs"
)

var Schedule = jobs.Cron(func(s *jobs.Scheduler) {
	s.Job("0 * * * *", jobs.CleanupJob{})           // every hour
	s.Job("0 0 * * *", jobs.SendDigestJob{})         // midnight daily
	s.Job("*/5 * * * *", jobs.CleanupJob{}).
		MaxRetries(3).
		Timeout(2 * time.Minute).
		AllowOverlap()
})
