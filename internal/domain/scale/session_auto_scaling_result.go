package scale

import (
	"legacy-messenger-control-plane/internal/domain/serviceevaluation"
	"legacy-messenger-control-plane/internal/domain/sessionreport"
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

	SessionReport []sessionreport.SessionReportResult `json:"sessionReport"`

	ECSState interface{} `json:"ecsState,omitempty"`
}
