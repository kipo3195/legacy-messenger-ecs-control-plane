package usecase

import (
	"context"
	"fmt"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/domain"
	"legacy-messenger-control-plane/internal/ports"
	"log"
	"math"
	"sort"
	"time"
)

type sessionAutoScalingUsecase struct {
	taskSessionPort    ports.TaskSessionPort
	ecsPort            ports.ECSPort
	registry           *configs.ServiceRegistry
	autoScale          *configs.AutoScaleConfig
	ecsCfg             *configs.ECSConfig
	scalingPolicy      *ScalingPolicy
	scaleInCoordinator *ScaleInCoordinator
}

type SessionAutoScalingUsecase interface {
	EvaluateAndScale(ctx context.Context, serviceName string) (domain.SessionAutoScalingResult, error)
}

func NewSessionAutoScalingUsecase(
	taskSessionPort ports.TaskSessionPort,
	ecsPort ports.ECSPort,
	registry *configs.ServiceRegistry,
	ecsCfg *configs.ECSConfig,
	autoScale *configs.AutoScaleConfig,
	scalingPolicy *ScalingPolicy,
	scaleInCoordinator *ScaleInCoordinator,
) SessionAutoScalingUsecase {
	return &sessionAutoScalingUsecase{
		taskSessionPort:    taskSessionPort,
		ecsPort:            ecsPort,
		registry:           registry,
		ecsCfg:             ecsCfg,
		autoScale:          autoScale,
		scalingPolicy:      scalingPolicy,
		scaleInCoordinator: scaleInCoordinator,
	}
}

// 실제 객체를 생성하지 않고 구현체가 인터페이스 계약을 만족하는지 컴파일러에게 확인시키는 명시적 인터페이스 구현 검증문
// https://chatgpt.com/share/6a583671-bcc4-83ee-9722-5f21a67d7b99
var _ SessionAutoScalingUsecase = (*sessionAutoScalingUsecase)(nil)

func (u *sessionAutoScalingUsecase) EvaluateAndScale(ctx context.Context, serviceName string) (domain.SessionAutoScalingResult, error) {
	// [핵심 정책]
	// 1. expires가 지난 보고는 세션 합산에서 제외한다.
	// 2. 보고가 만료됐다고 해서 해당 Task의 sessionCount가 0이거나, ECS Task가 종료됐다고 판단하지 않는다.
	// 3. RUNNING Task의 보고가 만료됐거나 누락됐다면 전체 세션 집계는 실제보다 작을 수 있다.
	// 4. 보고가 불완전한 상태에서는 Scale-in을 수행하지 않는다.
	// 5. 유효한 보고만으로도 Scale-out 조건을 충족하면 Scale-out은 수행할 수 있다.
	// 6. 서비스별로 동시에 하나의 Scale-in Drain 작업만 진행한다.

	// 1. Redis report와 ECS Task 비교
	// yaml 파일의 서비스 정의 조회
	serviceDef, err := u.registry.Find(serviceName)
	if err != nil {
		return domain.SessionAutoScalingResult{}, fmt.Errorf("service not found: %s", serviceName)
	}

	// 2. 현재 ECS의 상태 조회
	ecsState, err := u.ecsPort.GetServiceControlState(
		ctx,
		u.ecsCfg.ClusterName,
		serviceDef.ECSServiceName,
	)
	//fmt.Printf("[ecs state] desiredCount:%d runningCount : %d, pendingCount : %d\n", ecsState.DesiredCount, ecsState.RunningCount, ecsState.PendingCount)

	if err != nil {
		return domain.SessionAutoScalingResult{},
			fmt.Errorf("failed to get ECS service control state: %w", err)
	}

	// 3. 전체 report 조회
	reported, err := u.taskSessionPort.GetTaskSessionReport(ctx, serviceName)
	if err != nil {
		return domain.SessionAutoScalingResult{}, fmt.Errorf("failed to get task session reports: %w", err)
	}

	// 4. 만료, 중지된 task조회
	expiredReport, stopCandidates, err := u.taskSessionPort.GetInvalidReportTask(ctx, serviceName, u.autoScale)
	if err != nil {
		return domain.SessionAutoScalingResult{}, fmt.Errorf("get task session report expired error")
	}
	// 5. report 결과를 순회하면서 유효와 만료 task 분리 (expiredReport는 중지된 task를 포함)
	_, normalTask := getSeperatedTask(expiredReport, reported)

	// 6. 정상 보고 report 커버리지 계산
	// 전체 세션의 수, task당 평균 세션 수, 세션이 적은 task의 순서로 정의된 slice, 정상적으로 보고하는 task의 비율
	taskSessionInfo := calculateTotalSessionCount(reported, normalTask, int(ecsState.RunningCount))

	//f("[session count] total : %d, avg : %d, reportCoverage : %0.1f\n", taskSessionInfo.TotalSessionCount, taskSessionInfo.AvgSessionCount, taskSessionInfo.ReportCoverage)

	// 7. 현재 시점에 요구되는 task수 계산
	requiredTaskCount := calculateRequiredTaskCount(
		taskSessionInfo.TotalSessionCount,
		u.autoScale.SessionPerTask,      // Task당 적절한 session 수
		u.autoScale.ScaleOutUtilization, // Task가 적절한 session 수 대비 몇 %가 되었을때 scaling을 진행할 것인지에 대한 비율
		u.autoScale.MinTaskCount,
		u.autoScale.MaxTaskCount,
	)
	//fmt.Printf("[required task count] count : %d\n", requiredTaskCount)

	// 8. 현재 desiredCount와 필요한 Task 수를 비교
	demendResult := evaluateScalingDemand(
		serviceName,
		taskSessionInfo,
		int(ecsState.DesiredCount),
		int(ecsState.RunningCount),
		requiredTaskCount,
	)
	//fmt.Printf("[scale demend] action : %s\n", demendResult.Action)

	var result domain.SessionAutoScalingResult

	// 9. 정책 평가 + 수렴 여부 (직전의 요청이 처리되었는지)
	policyDecision, scalingApproved := u.scalingPolicy.Evaluate(
		demendResult,
		ecsState,
		taskSessionInfo.ReportCoverage,
		time.Now(),
	)

	//10. scaling 실행
	if !scalingApproved {
		result = policyDecision
	} else {
		result, err = u.applyScalingDecision(
			ctx,
			serviceDef.ECSServiceName,
			policyDecision,
		)

		if err != nil {
			return domain.SessionAutoScalingResult{}, fmt.Errorf(
				"failed to apply scaling decision: serviceName=%s: %w",
				serviceName,
				err,
			)
		}

		// ECS 변경이 실제로 실행된 경우에만 실행 시각 기록 - CoolDown 체크용
		if result.Executed {
			executedAt := time.Now()

			u.scalingPolicy.RecordExecution(
				serviceName,
				string(result.Action),
				executedAt,
			)

			// fmt.Printf(
			// 	"[scaling executed] serviceName=%s action=%s currentDesiredCount=%d recommendedDesiredCount=%d executedAt=%s\n",
			// 	serviceName,
			// 	result.Action,
			// 	result.CurrentDesiredCount,
			// 	result.RecommendedDesiredCount,
			// 	executedAt.Format(time.RFC3339),
			// )
		}
	}

	// 11. 3.에서 구한 stop candidate redis 점검 후 삭제 (추후 ECS에서 task 정보도 조회 하면 좋을 듯)
	u.stopExpiredTasks(ctx, serviceName, stopCandidates)

	sessionReportMap := make(map[string]int, len(normalTask))
	// 12. 정상적인 session report 데이터를 로깅
	for _, v := range normalTask {
		sessionCount := reported[v].SessionCount
		sessionReportMap[v] = sessionCount
	}

	result.SessionReport = sessionReportMap

	ecsState, err = u.ecsPort.GetServiceControlState(
		ctx,
		u.ecsCfg.ClusterName,
		serviceDef.ECSServiceName,
	)
	return result, nil
}

func calculateTotalSessionCount(
	reported map[string]domain.SessionReport,
	normalTask []string,
	runningTask int,
) domain.TaskSessionInfo {

	totalCount := 0

	sessionCountPerTask := make([]domain.TaskInfo, 0, len(normalTask))
	// normalTask를 기준으로 totalCount 구하기
	for _, taskID := range normalTask {

		report, exists := reported[taskID]
		if !exists {
			continue
		}

		totalCount += report.SessionCount

		sessionCountPerTask = append(sessionCountPerTask, domain.TaskInfo{
			TaskID:       taskID,
			SessionCount: report.SessionCount,
		})
	}

	var reportCoverage float64
	if len(normalTask) != 0 {
		// 정상적으로 보고를 보내는 task / 정상적으로 돌고있는 task
		reportCoverage = float64(len(normalTask)) / float64(runningTask)
	}

	// SessionCount가 적은 순서로 오름차순 정렬
	sort.Slice(sessionCountPerTask, func(i, j int) bool {
		return sessionCountPerTask[i].SessionCount <
			sessionCountPerTask[j].SessionCount
	})

	avgSessionCount := 0
	if len(sessionCountPerTask) > 0 {
		avgSessionCount = totalCount / len(sessionCountPerTask)
	}

	return domain.TaskSessionInfo{
		TotalSessionCount:   totalCount,
		AvgSessionCount:     avgSessionCount,
		SessionCountPerTask: sessionCountPerTask, // scale in 용
		ReportCoverage:      reportCoverage,
	}
}

func calculateRequiredTaskCount(
	totalSessionCount int, // task 전체에 대한 session의 수
	sessionPerTask int, // task당 추구하는 session의 수
	scaleOutUtilization float64, // 대응해야하는 비율
	minTaskCount int, // 최소 task 수
	maxTaskCount int, // 최대 task 수
) int {
	if sessionPerTask <= 0 {
		return minTaskCount
	}

	// Task당 세션 수 * scale 대응 비율
	effectiveCapacity :=
		float64(sessionPerTask) * scaleOutUtilization

	if effectiveCapacity <= 0 {
		return minTaskCount
	}

	// 요구되는 task의 수
	// 전체 연결 sessionCount / task당 scale out 대비 가능한 효과적인 session 수
	// ex) total : 3000, effective : 1200 -> 3
	// ex) total : 1000, effective : 1200 -> 1
	requiredCount := int(math.Ceil(
		float64(totalSessionCount) / effectiveCapacity,
	))

	if requiredCount < minTaskCount {
		return minTaskCount
	}

	if requiredCount > maxTaskCount {
		return maxTaskCount
	}

	return requiredCount
}

// 세션 수치 비교
func evaluateScalingDemand(
	serviceName string,
	taskSessionInfo domain.TaskSessionInfo,
	currentDesiredCount int,
	currentRunningCount int,
	requiredTaskCount int,
) domain.SessionAutoScalingResult {

	totalSessionCount := taskSessionInfo.TotalSessionCount

	switch {
	case requiredTaskCount > currentDesiredCount:
		return domain.SessionAutoScalingResult{
			ServiceName:             serviceName,
			TotalSessionCount:       totalSessionCount,
			CurrentDesiredCount:     currentDesiredCount,
			RunningTaskCount:        currentRunningCount,
			RecommendedDesiredCount: requiredTaskCount,
			Action:                  domain.ScalingActionScaleOut,
			Executed:                false,
			Reason:                  "required task count exceeds current desired count",
		}

	case requiredTaskCount < currentDesiredCount:
		return domain.SessionAutoScalingResult{
			ServiceName:             serviceName,
			TotalSessionCount:       totalSessionCount,
			CurrentDesiredCount:     currentDesiredCount,
			RunningTaskCount:        currentRunningCount,
			RecommendedDesiredCount: requiredTaskCount,
			Action:                  domain.ScalingActionScaleIn,
			Executed:                false,
			Reason:                  "required task count is below current desired count",
		}

	default:
		return domain.SessionAutoScalingResult{
			ServiceName:             serviceName,
			TotalSessionCount:       totalSessionCount,
			CurrentDesiredCount:     currentDesiredCount,
			RunningTaskCount:        currentRunningCount,
			RecommendedDesiredCount: requiredTaskCount,
			Action:                  domain.ScalingActionKeep,
			Executed:                false,
			Reason:                  "current desired count is appropriate",
		}
	}
}

func (u *sessionAutoScalingUsecase) applyScalingDecision(
	ctx context.Context,
	ecsServiceName string,
	result domain.SessionAutoScalingResult,
) (domain.SessionAutoScalingResult, error) {
	log.Println("[applyScalingDecision] result.Action :", result.Action)
	switch result.Action {

	case domain.ScalingActionScaleOut:
		updatedState, err := u.ecsPort.UpdateServiceDesiredCount(
			ctx,
			u.ecsCfg.ClusterName,
			ecsServiceName,
			result.RecommendedDesiredCount,
		)
		if err != nil {
			return domain.SessionAutoScalingResult{}, fmt.Errorf(
				"failed to update ECS desired count: serviceName=%s currentDesiredCount=%d recommendedDesiredCount=%d: %w",
				result.ServiceName,
				result.CurrentDesiredCount,
				result.RecommendedDesiredCount,
				err,
			)
		}

		result.Executed = true
		result.ECSState = updatedState
		result.Reason = fmt.Sprintf(
			"scale-out executed successfully: desiredCount=%d -> %d",
			result.CurrentDesiredCount,
			result.RecommendedDesiredCount,
		)

	case domain.ScalingActionScaleIn:

		// 상태에 따라서 drain 요청과 scale in (실제 ECS에 떠있는 서비스 중단 처리를 구분해서 실행해야되나?)

		return u.requestScaleIn(ecsServiceName, result)

	case domain.ScalingActionKeep:
		result.Executed = false

	case domain.ScalingActionNotScalable:
		result.Executed = false

	default:
		return domain.SessionAutoScalingResult{}, fmt.Errorf(
			"unsupported scaling action: %s",
			result.Action,
		)
	}

	return result, nil
}

func (u *sessionAutoScalingUsecase) stopExpiredTasks(
	ctx context.Context,
	serviceName string,
	stopCandidates []string,
) error {
	now := time.Now()

	for _, taskID := range stopCandidates {
		shouldStop, err := u.taskSessionPort.ShouldStopTask(
			ctx,
			serviceName,
			taskID,
			now,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to recheck stop candidate: taskID=%s: %w",
				taskID,
				err,
			)
		}

		// 후보 조회 이후 새로운 세션 보고가 들어온 경우
		if !shouldStop {
			continue
		}

		// if err := u.ecsPort.StopTask(
		// 	ctx,
		// 	taskID,
		// 	"session report timeout",
		// ); err != nil {
		// 	return fmt.Errorf(
		// 		"failed to stop task: taskID=%s: %w",
		// 		taskID,
		// 		err,
		// 	)
		// }
		if err := u.taskSessionPort.DeleteTaskSessionState(
			ctx,
			serviceName,
			taskID,
		); err != nil {
			return fmt.Errorf(
				"failed to delete task session state: taskID=%s: %w",
				taskID,
				err,
			)
		}
	}

	return nil
}

func getSeperatedTask(expiredReport map[string]string, reported map[string]domain.SessionReport) ([]string, []string) {
	expiredTask := make([]string, 0)
	normalTask := make([]string, 0)
	for k := range reported {
		taskID := k
		_, exists := expiredReport[taskID]
		if exists {
			expiredTask = append(expiredTask, taskID)
			continue
		}
		// expired report task check
		normalTask = append(normalTask, taskID)
	}

	return expiredTask, normalTask
}

type ScalingPolicyConfig struct {
	MinDesiredCount int
	MaxDesiredCount int

	ScaleOutConsecutiveCount int
	ScaleInConsecutiveCount  int

	ScaleOutCooldown time.Duration
	ScaleInCooldown  time.Duration

	MaxScaleOutStep int
	MaxScaleInStep  int

	MinReportCoverage float64
}

type ScalingEvaluation struct {
	Demand domain.SessionAutoScalingResult

	ECSState       domain.ECSServiceControlState
	ReportCoverage float64
	EvaluatedAt    time.Time
}

func (u *sessionAutoScalingUsecase) requestScaleIn(
	ecsServiceName string,
	result domain.SessionAutoScalingResult,
) (domain.SessionAutoScalingResult, error) {

	targetDesiredCount := result.CurrentDesiredCount - 1

	err := u.scaleInCoordinator.Request(
		domain.ScaleInJob{
			ServiceName:         result.ServiceName,
			ECSServiceName:      ecsServiceName,
			CurrentDesiredCount: result.CurrentDesiredCount,
			TargetDesiredCount:  targetDesiredCount,
		},
	)
	if err != nil {
		return result, fmt.Errorf(
			"failed to request scale-in: serviceName=%s: %w",
			result.ServiceName,
			err,
		)
	}

	result.Executed = false
	result.RecommendedDesiredCount = targetDesiredCount
	result.Reason = fmt.Sprintf(
		"scale-in job requested: desiredCount=%d -> %d",
		result.CurrentDesiredCount,
		targetDesiredCount,
	)

	return result, nil
}
