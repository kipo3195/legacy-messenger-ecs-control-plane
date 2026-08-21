package domain

import (
	cp "legacy-messenger-control-plane/internal/domain/connectionpressure"
	"legacy-messenger-control-plane/internal/domain/ecs"
	sca "legacy-messenger-control-plane/internal/domain/scale"
	si "legacy-messenger-control-plane/internal/domain/scalein"
	svc "legacy-messenger-control-plane/internal/domain/service"
	sc "legacy-messenger-control-plane/internal/domain/servicecontrol"
	se "legacy-messenger-control-plane/internal/domain/serviceevaluation"
	ss "legacy-messenger-control-plane/internal/domain/servicescale"
	sr "legacy-messenger-control-plane/internal/domain/sessionreport"
	th "legacy-messenger-control-plane/internal/domain/targethealth"
	tk "legacy-messenger-control-plane/internal/domain/task"
)

// domain 디렉터리 하위를 참조할 수 있도록 구성
// 실제로 해당 도메인 구조체를 사용하는 곳도 'domain' 으로 접근 가능함.

// Connection pressure
type ConnectionPressure = cp.ConnectionPressure
type ConnectionPressureMetric = cp.ConnectionPressureMetric
type ScalingRecommendation = cp.ScalingRecommendation
type ConnectionPressureStatus = cp.ConnectionPressureStatus

const (
	ConnectionPressureStatusLow    = cp.ConnectionPressureStatusLow
	ConnectionPressureStatusNormal = cp.ConnectionPressureStatusNormal
	ConnectionPressureStatusHigh   = cp.ConnectionPressureStatusHigh
)

// Service observation
type ServiceList = svc.ServiceList
type DeploymentStatus = svc.DeploymentStatus
type ServiceStatus = svc.ServiceStatus
type ServiceEvent = svc.ServiceEvent
type ServiceTargetGroup = svc.ServiceTargetGroup

// Target health
type TargetGroupHealth = th.TargetGroupHealth
type TargetHealthEntry = th.TargetHealthEntry
type TargetHealthResponse = th.TargetHealthResponse
type TargetHealthOverallStatus = th.TargetHealthOverallStatus
type TargetHealthSummary = th.TargetHealthSummary

const (
	TargetHealthOverallHealthy       = th.TargetHealthOverallHealthy
	TargetHealthOverallDegraded      = th.TargetHealthOverallDegraded
	TargetHealthOverallTransitioning = th.TargetHealthOverallTransitioning
	TargetHealthOverallUnknown       = th.TargetHealthOverallUnknown
)

// Task observation
type TaskNetworkInfo = tk.TaskNetworkInfo
type NetworkBindingInfo = tk.NetworkBindingInfo
type NetworkInterfaceInfo = tk.NetworkInterfaceInfo
type ContainerStatus = tk.ContainerStatus
type TaskStatus = tk.TaskStatus
type TaskSessionReport = tk.TaskSessionReport

type TaskSessionReportResult = tk.TaskSessionReportResult
type TaskSessionValue = tk.TaskSessionValue
type TaskRunningEvent = tk.TaskRunningEvent

// service scale
type ServiceScaleResult = ss.ServiceScaleResult
type ECSServiceControlState = ss.ECSServiceControlState
type ServiceScaleCommand = ss.ServiceScaleCommand

// service control
type ServiceRedeployResult = sc.ServiceRedeployResult
type ServiceDeployment = sc.ServiceDeployment

// service evaluation
type ScalingCurrentStatus = se.ScalingCurrentStatus
type ScalingEvaluation = se.ScalingEvaluation
type ScalingPolicyStatus = se.ScalingPolicyStatus
type ScalingRecommendationStatus = se.ScalingRecommendationStatus

// scale
type SessionAutoScalingResult = sca.SessionAutoScalingResult
type SessionReport = sca.SessionReport
type TaskSessionInfo = sca.TaskSessionInfo
type TaskInfo = sca.TaskInfo

const (
	ScalingActionScaleOut       = se.ScalingActionScaleOut
	ScalingActionScaleIn        = se.ScalingActionScaleIn
	ScalingActionKeep           = se.ScalingActionKeep
	ScaleActionSkip             = se.ScaleActionSkip
	ScalingActionNotScalable    = se.ScalingActionNotScalable
	ScaleActionScaleInCandidate = se.ScalingActionScaleInCandidate
)

// scale in
type ScaleInJob = si.ScaleInJob
type ScaleInStatus = si.ScaleInStatus

const (
	ScaleInStatusRequested = si.ScaleInStatusRequested
	ScaleInStatusDraining  = si.ScaleInStatusDraining
	ScaleInStatusApplied   = si.ScaleInStatusApplied
	ScaleInStatusCompleted = si.ScaleInStatusCompleted
	ScaleInStatusFailed    = si.ScaleInStatusFailed
)

type ECSTask = ecs.ECSTask

// session report
type SessionReportResult = sr.SessionReportResult
