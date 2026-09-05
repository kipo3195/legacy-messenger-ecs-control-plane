package scalein

import "time"

type ScaleInJob struct {
	ServiceName    string
	ECSServiceName string

	Status ScaleInStatus

	TargetTaskID     string
	ProtectedTaskIDs []string

	CurrentDesiredCount int
	TargetDesiredCount  int

	ZeroSessionStreak int

	RequestedAt time.Time
	AppliedAt   time.Time
	UpdatedAt   time.Time

	LastError string
}
