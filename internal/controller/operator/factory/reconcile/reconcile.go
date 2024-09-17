package reconcile

import (
	"time"
)

var (
	podWaitReadyIntervalCheck = 50 * time.Millisecond
	appWaitReadyDeadline      = 5 * time.Second
	podWaitReadyTimeout       = 5 * time.Second
)

// InitFromConfig sets package configuration from config
func InitDeadlines(intervalCheck, appWaitDeadline, podReadyDeadline time.Duration) {
	podWaitReadyIntervalCheck = intervalCheck
	appWaitReadyDeadline = appWaitDeadline
	podWaitReadyTimeout = podReadyDeadline
}
