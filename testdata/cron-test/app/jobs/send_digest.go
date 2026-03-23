package jobs

import "log"

type SendDigestJob struct{}

func (j SendDigestJob) Handle() error {
	log.Println("sending daily digest emails")
	// In a real app, query users and send emails
	return nil
}
