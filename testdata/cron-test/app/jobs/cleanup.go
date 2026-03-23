package jobs

import "log"

type CleanupJob struct{}

func (j CleanupJob) Handle() error {
	log.Println("running cleanup job: removing expired records")
	// In a real app, you'd do something like:
	//   _, err := models.QuerySession().WhereExpiredBefore(time.Now()).Delete()
	return nil
}
