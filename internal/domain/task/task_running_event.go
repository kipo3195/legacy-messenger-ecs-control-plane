package task

import "time"

type TaskRunningEvent struct {
	ServiceName string
	TaskID      string

	StartedAt *time.Time
}
