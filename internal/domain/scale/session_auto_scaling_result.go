package scale

import (
	"legacy-messenger-control-plane/internal/domain/serviceevaluation"
)

type SessionAutoScalingResult struct {
	ServiceName string `json:"serviceName"`

	TotalSessionCount       int `json:"totalSessionCount"`
	RunningTaskCount        int `json:"runningTaskCount"`
	CurrentDesiredCount     int `json:"currentDesiredCount"`
	RecommendedDesiredCount int `json:"recommendedDesiredCount"`

	Action   serviceevaluation.ScalingAction `json:"action"`
	Executed bool                            `json:"executed"`
	Reason   string                          `json:"reason"`

	SessionReport map[string]int `json:"sessionReport"`

	ECSState interface{} `json:"ecsState,omitempty"`
}
