package usecase

import (
	"context"
	"fmt"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/domain"
	"legacy-messenger-control-plane/internal/ports"
	"log"
	"time"
)

type scaleInUsecase struct {
	taskSessionPort ports.TaskSessionPort
	ecsPort         ports.ECSPort
	taskDrainPort   ports.TaskDrainPort
	ecsCfg          *configs.ECSConfig
	autoScaleCfg    *configs.AutoScaleConfig
	scalingPolicy   *ScalingPolicy
	coordinator     *ScaleInCoordinator
	targetTaskID    string
}

type ScaleInUsecase interface {
	Process(ctx context.Context) error
}

func NewScaleInUsecase(
	taskSessionPort ports.TaskSessionPort,
	ecsPort ports.ECSPort,
	taskDrainPort ports.TaskDrainPort,
	autoScaleCfg *configs.AutoScaleConfig,
	ecsCfg *configs.ECSConfig,
	scalingPolicy *ScalingPolicy,
	coordinator *ScaleInCoordinator,
) ScaleInUsecase {

	return &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		ecsPort:         ecsPort,
		taskDrainPort:   taskDrainPort,
		autoScaleCfg:    autoScaleCfg,
		ecsCfg:          ecsCfg,
		scalingPolicy:   scalingPolicy,
		coordinator:     coordinator,
		targetTaskID:    "",
	}
}

// SessionAutoScalingScheduler에서 Scale in 정책을 승인 (Coordinator에 ScaleInJob 등록)
// 하면 일정 주기마다 Process -> 활성 Job 조회 -> 현재 상태에 맞는 다음 단계 수행
func (u *scaleInUsecase) Process(ctx context.Context) error {
	jobs := u.coordinator.GetActiveJobs()

	for _, job := range jobs {
		if err := u.processJob(ctx, job); err != nil {
			markErr := u.coordinator.MarkFailed(
				job.ServiceName,
				err,
			)
			if markErr != nil {
				fmt.Printf(
					"[scale-in] failed to mark job as failed: serviceName=%s processError=%v markError=%v\n",
					job.ServiceName,
					err,
					markErr,
				)
				continue
			}

			fmt.Printf(
				"[scale-in] job failed: serviceName=%s status=%s error=%v\n",
				job.ServiceName,
				job.Status,
				err,
			)
		}
	}

	return nil
}

func (u *scaleInUsecase) processJob(
	ctx context.Context,
	job domain.ScaleInJob,
) error {

	// 진행중인 작업에 따라 다르게 처리함.
	switch job.Status {
	case domain.ScaleInStatusRequested:
		log.Println("[processJob] 1111")
		return u.startDrain(ctx, job)

	case domain.ScaleInStatusDraining:
		log.Println("[processJob] 2222")
		return u.checkDrain(ctx, job)

	case domain.ScaleInStatusApplied:
		log.Println("[processJob] 3333")
		return u.checkCompletion(ctx, job)

	default:
		return nil
	}
}

// REQUESTED 상태
// 메인 Auto Scaling Scheduler가 Scale-in 작업을 등록한 직후
func (u *scaleInUsecase) startDrain(
	ctx context.Context,
	job domain.ScaleInJob,
) error {
	targetTask, err := u.selectScaleInTarget( // 가장 적은 수의 session을 갖는 task를 선출
		ctx,
		job.ServiceName,
	)
	if err != nil {
		return err
	}
	// running task 조회 -> scale in 대상 제외 처리 (proection)
	runningTaskIDs, err := u.ecsPort.GetRunningTaskIDs(
		ctx,
		u.ecsCfg.ClusterName,
		job.ECSServiceName,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to get running tasks: %w",
			err,
		)
	}

	// 종료 대상 외 Task를 생존 예정 Task로 선정
	protectedTaskIDs := make([]string, 0)

	for _, taskID := range runningTaskIDs {
		if taskID == targetTask.TaskID {
			continue
		}

		protectedTaskIDs = append(
			protectedTaskIDs,
			taskID,
		)
	}

	// 생존 예정 Task protection 설정
	if len(protectedTaskIDs) > 0 {
		if err := u.ecsPort.UpdateTaskProtection(
			ctx,
			u.ecsCfg.ClusterName,
			protectedTaskIDs,
			true,
		); err != nil {
			return fmt.Errorf(
				"failed to protect survivor tasks: %w",
				err,
			)
		}
	}

	// 5. 대상 Task에 실제 Drain 요청 (이건 redis에 호출하게 아니지 않나?)
	if err := u.taskDrainPort.RequestDrain(
		ctx,
		job.ServiceName,
		targetTask.TaskID,
	); err != nil {

		// Drain 요청 실패 시 protection 원복
		if len(protectedTaskIDs) > 0 {
			_ = u.ecsPort.UpdateTaskProtection(
				context.Background(),
				u.ecsCfg.ClusterName,
				protectedTaskIDs,
				false,
			)
		}

		return fmt.Errorf(
			"failed to request task drain: taskID=%s: %w",
			targetTask.TaskID,
			err,
		)
	}

	// 6.메모리에서 현재 drain 대상 task 정보 관리 - 완료 후 protection 해제용
	u.targetTaskID = targetTask.TaskID

	// 7. 상태 변경을 위해 sessionCount 0으로 초기화 (세션 보고는 하지 않지만, 보고를 하지 않는다고해서 redis의 count가 0이 되는것은 아니므로)
	// 그리고 drain 요청을 받은 서비스는 request 수신 즉시 report 보고 하지 않아야함.
	err = u.taskSessionPort.DeleteTaskSessionState(ctx, job.ServiceName, u.targetTaskID)
	if err != nil {
		return fmt.Errorf(
			"failed to request task remove session state. taskID=%s: %w",
			targetTask.TaskID,
			err,
		)
	}

	// 8. 상태 및 작업 정보 저장
	return u.coordinator.MarkDraining(
		job.ServiceName,
		targetTask.TaskID,
		protectedTaskIDs,
	)
}

func (u *scaleInUsecase) selectScaleInTarget(
	ctx context.Context,
	serviceName string,
) (domain.TaskInfo, error) {
	reports, err := u.taskSessionPort.GetTaskSessionReport( // redis에 있는 report를 직접 조회 해서 가정 적은 session의 task를 찾음
		ctx,
		serviceName,
	)
	if err != nil {
		return domain.TaskInfo{}, fmt.Errorf(
			"failed to get task session reports: %w",
			err,
		)
	}

	expiredReports, _, err :=
		u.taskSessionPort.GetInvalidReportTask(
			ctx,
			serviceName,
			u.autoScaleCfg,
		)
	if err != nil {
		return domain.TaskInfo{}, fmt.Errorf(
			"failed to get invalid task reports: %w",
			err,
		)
	}

	var target domain.TaskInfo
	found := false

	for taskID, report := range reports {
		// 만료되거나 중지 대상으로 분류된 Task 제외
		if _, expired := expiredReports[taskID]; expired {
			continue
		}

		if !found || report.SessionCount < target.SessionCount {
			target = domain.TaskInfo{
				TaskID:       taskID,
				SessionCount: report.SessionCount,
			}
			found = true
		}
	}

	if !found {
		return domain.TaskInfo{}, fmt.Errorf(
			"no eligible scale-in target found: serviceName=%s",
			serviceName,
		)
	}

	return target, nil
}

// DRAINING
// Drain 요청을 보낸 상태야.
// 매 Scheduler 실행마다 대상 Task의 세션 수를 확인 (0이 될때까지)
func (u *scaleInUsecase) checkDrain(
	ctx context.Context,
	job domain.ScaleInJob,
) error {
	report, err := u.taskSessionPort.GetTaskSessionReportByTask( // drain 중에 현재 task에 연결된 session의 수 점검, 단 TargetTaskID는 Requested시점의 최소 session task임
		ctx,
		job.ServiceName,
		job.TargetTaskID,
	)
	if err != nil {
		return err
	}

	if report.SessionCount > 0 {
		return u.coordinator.ResetZeroSessionStreak(
			job.ServiceName,
		)
	}

	// zeroSessionStreak 증가 (판단 정책)
	streak, err := u.coordinator.IncreaseZeroSessionStreak(
		job.ServiceName,
	)
	if err != nil {
		return err
	}

	log.Println("streak 점검 : ", streak)
	if streak < 3 {
		return nil
	}

	// 0이 연속 3회, desiredCount 감소, 상태 APPLIED
	_, err = u.ecsPort.UpdateServiceDesiredCount(
		ctx,
		u.ecsCfg.ClusterName,
		job.ECSServiceName,
		job.TargetDesiredCount,
	)
	if err != nil {
		return err
	}
	// taskID LastStatus를 STOPPED로 변경 -> checkCompletion에서 상태를 확인함.

	u.scalingPolicy.RecordExecution(
		job.ServiceName,
		string(domain.ScalingActionScaleIn),
		time.Now(),
	)

	return u.coordinator.MarkApplied(job.ServiceName)
}

// APPLIED
// 이미 ECS의 desiredCount 감소 요청까지 성공한 상태
func (u *scaleInUsecase) checkCompletion(
	ctx context.Context,
	job domain.ScaleInJob,
) error {

	// ECS 서비스 수렴 여부 확인
	ecsState, err := u.ecsPort.GetServiceControlState(
		ctx,
		u.ecsCfg.ClusterName,
		job.ECSServiceName,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to get ECS service state: %w",
			err,
		)
	}

	// desiredCount == runningCount인가
	// pendingCount == 0인가
	// 정상적으로 scale in이 되었는가
	if ecsState.PendingCount > 0 ||
		ecsState.RunningCount != ecsState.DesiredCount {
		return nil
	}

	// 현재 drain 진행중인 task 정보 조회 - requested 시점에 저장함
	targetTaskID := job.TargetTaskID

	// 대상 Task STOPPED 확인
	ecsTask, err := u.ecsPort.DescribeTask(ctx, u.ecsCfg.ClusterName, targetTaskID)
	if err != nil {
		return fmt.Errorf(
			"failed to get ECS DescribeTask : %w",
			err,
		)
	}

	if ecsTask.LastStatus != "STOPPED" {
		return fmt.Errorf(
			"%s status is invalid ... status : %w",
			targetTaskID,
			err,
		)
	}

	// 종료 대상 외 Task를 생존 예정 Task로 선정
	protectedTaskIDs := make([]string, 0)

	// running task 조회 -> scale in 대상 제외 처리 (proection)
	runningTaskIDs, err := u.ecsPort.GetRunningTaskIDs(
		ctx,
		u.ecsCfg.ClusterName,
		job.ECSServiceName,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to get running tasks: %w",
			err,
		)
	}

	for _, taskID := range runningTaskIDs {
		if taskID == targetTaskID {
			continue
		}

		protectedTaskIDs = append(
			protectedTaskIDs,
			taskID,
		)
	}

	// Task protection 해제
	if err := u.ecsPort.UpdateTaskProtection(
		ctx,
		u.ecsCfg.ClusterName,
		protectedTaskIDs,
		false,
	); err != nil {
		return fmt.Errorf(
			"failed to protect survivor tasks: %w",
			err,
		)
	}

	// scail in 완료 후 taskID 초기화
	u.targetTaskID = ""

	log.Println("draining 완료 이후 여기 호출 하나")

	// Scale-in 작업을 종료 상태로 바꾸기 위해 호출
	return u.coordinator.Complete(job.ServiceName)
}

func (c *ScaleInCoordinator) MarkFailed(
	serviceName string,
	cause error,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, exists := c.jobs[serviceName]
	if !exists {
		return fmt.Errorf(
			"scale-in job not found: serviceName=%s",
			serviceName,
		)
	}

	job.Status = domain.ScaleInStatusFailed
	job.UpdatedAt = time.Now()

	if cause != nil {
		job.LastError = cause.Error()
	}

	return nil
}

func (c *ScaleInCoordinator) Complete(
	serviceName string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, exists := c.jobs[serviceName]
	if !exists {
		return fmt.Errorf(
			"scale-in job not found: serviceName=%s",
			serviceName,
		)
	}

	if job.Status != domain.ScaleInStatusApplied {
		return fmt.Errorf(
			"scale-in cannot be completed: serviceName=%s status=%s",
			serviceName,
			job.Status,
		)
	}

	job.Status = domain.ScaleInStatusCompleted
	job.UpdatedAt = time.Now()

	return nil
}
