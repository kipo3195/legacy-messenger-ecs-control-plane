package usecase

import (
	"context"
	"errors"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/domain"
	"strings"
	"testing"
	"time"
)

// 테스트 목표
// Scale-in 대상 선정 시 가장 session 수가 적은 Task를 선택한다.

// task-a session = 10
// task-b session = 3
// task-c session = 7
// 만료된 report 없음
// → task-b 선택

func TestScaleInUsecase_SelectScaleInTarget_SelectsLowestSessionTask(t *testing.T) {
	// Given - 테스트 상태 준비
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		reports: map[string]domain.SessionReport{
			"task-a": {SessionCount: 10},
			"task-b": {SessionCount: 3},
			"task-c": {SessionCount: 7},
		},
		expiredReports: map[string]string{},
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	target, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - session 수가 가장 적은 task-b가 선택된다.
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if target.TaskID != "task-b" {
		t.Fatalf("expected target task-b, got %s", target.TaskID)
	}

	if target.SessionCount != 3 {
		t.Fatalf("expected session count 3, got %d", target.SessionCount)
	}
}

// 테스트 목표
// Scale-in 대상 선정 시 만료된 report를 가진 Task는 제외한다.

// task-a session = 1
// task-b session = 5
// task-a는 만료된 report
// → task-b 선택

func TestScaleInUsecase_SelectScaleInTarget_IgnoresExpiredReports(t *testing.T) {
	// Given - session 수는 task-a가 더 적지만 만료된 report로 분류된 상황
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		reports: map[string]domain.SessionReport{
			"task-a": {SessionCount: 1},
			"task-b": {SessionCount: 5},
		},
		expiredReports: map[string]string{
			"task-a": "expired",
		},
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	target, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - 만료되지 않은 task-b가 선택된다.
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if target.TaskID != "task-b" {
		t.Fatalf("expected target task-b, got %s", target.TaskID)
	}

	if target.SessionCount != 5 {
		t.Fatalf("expected session count 5, got %d", target.SessionCount)
	}
}

// 테스트 목표
// 모든 Task report가 만료된 경우 Scale-in 대상을 선택하지 않는다.

// task-a session = 1
// task-b session = 5
// task-a, task-b 모두 만료된 report
// → 선택 가능한 대상 없음

func TestScaleInUsecase_SelectScaleInTarget_ReturnsErrorWhenAllReportsExpired(t *testing.T) {
	// Given - report는 존재하지만 모두 만료된 상황
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		reports: map[string]domain.SessionReport{
			"task-a": {SessionCount: 1},
			"task-b": {SessionCount: 5},
		},
		expiredReports: map[string]string{
			"task-a": "expired",
			"task-b": "expired",
		},
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	_, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - 유효한 대상이 없으므로 에러를 반환한다.
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "no eligible scale-in target found") {
		t.Fatalf("expected no eligible target error, got %v", err)
	}
}

// 테스트 목표
// Task session report 자체가 없는 경우 Scale-in 대상을 선택하지 않는다.

// reports: empty
// expired: empty
// → 선택 가능한 대상 없음

func TestScaleInUsecase_SelectScaleInTarget_ReturnsErrorWhenNoReportsExist(t *testing.T) {
	// Given - 아직 어떤 Task도 session report를 보내지 않은 상황
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		reports:        map[string]domain.SessionReport{},
		expiredReports: map[string]string{},
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	_, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - 유효한 대상이 없으므로 에러를 반환한다.
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "no eligible scale-in target found") {
		t.Fatalf("expected no eligible target error, got %v", err)
	}
}

// 테스트 목표
// Task session report 조회에 실패하면 해당 에러를 전파한다.

// GetTaskSessionReport error 발생
// → failed to get task session reports 에러 반환

func TestScaleInUsecase_SelectScaleInTarget_PropagatesReportLoadError(t *testing.T) {
	// Given - 전체 session report 조회가 실패하는 상황
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		getReportsErr: errors.New("redis unavailable"),
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	_, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - report 조회 실패 에러를 감싼 에러를 반환한다.
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "failed to get task session reports") {
		t.Fatalf("expected report load error, got %v", err)
	}
}

// 테스트 목표
// 만료된 report 조회에 실패하면 해당 에러를 전파한다.

// GetInvalidReportTask error 발생
// → failed to get invalid task reports 에러 반환

func TestScaleInUsecase_SelectScaleInTarget_PropagatesInvalidReportLoadError(t *testing.T) {
	// Given - 전체 report 조회는 성공하지만 만료 report 조회가 실패하는 상황
	taskSessionPort := &scaleInTargetSelectionTaskSessionPort{
		reports: map[string]domain.SessionReport{
			"task-a": {SessionCount: 1},
		},
		getInvalidReportsErr: errors.New("redis unavailable"),
	}

	usecase := &scaleInUsecase{
		taskSessionPort: taskSessionPort,
		autoScaleCfg:    &configs.AutoScaleConfig{},
	}

	// When - Scale-in 대상 Task를 선정한다.
	_, err := usecase.selectScaleInTarget(
		context.Background(),
		"test-service",
	)

	// Then - 만료 report 조회 실패 에러를 감싼 에러를 반환한다.
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "failed to get invalid task reports") {
		t.Fatalf("expected invalid report load error, got %v", err)
	}
}

type scaleInTargetSelectionTaskSessionPort struct {
	reports              map[string]domain.SessionReport
	expiredReports       map[string]string
	getReportsErr        error
	getInvalidReportsErr error
}

func (p *scaleInTargetSelectionTaskSessionPort) SaveTaskSessionReport(
	ctx context.Context,
	report domain.TaskSessionReport,
) error {
	return nil
}

func (p *scaleInTargetSelectionTaskSessionPort) GetTaskSessionReport(
	ctx context.Context,
	serviceName string,
) (map[string]domain.SessionReport, error) {
	if p.getReportsErr != nil {
		return nil, p.getReportsErr
	}

	return p.reports, nil
}

func (p *scaleInTargetSelectionTaskSessionPort) GetInvalidReportTask(
	ctx context.Context,
	serviceName string,
	cfg *configs.AutoScaleConfig,
) (map[string]string, []string, error) {
	if p.getInvalidReportsErr != nil {
		return nil, nil, p.getInvalidReportsErr
	}

	return p.expiredReports, nil, nil
}

func (p *scaleInTargetSelectionTaskSessionPort) ShouldStopTask(
	ctx context.Context,
	serviceName string,
	taskID string,
	now time.Time,
) (bool, error) {
	return false, nil
}

func (p *scaleInTargetSelectionTaskSessionPort) DeleteTaskSessionState(
	ctx context.Context,
	serviceName string,
	taskID string,
) error {
	return nil
}

func (p *scaleInTargetSelectionTaskSessionPort) GetTaskSessionReportByTask(
	ctx context.Context,
	serviceName string,
	taskID string,
) (domain.SessionReport, error) {
	return domain.SessionReport{}, nil
}
