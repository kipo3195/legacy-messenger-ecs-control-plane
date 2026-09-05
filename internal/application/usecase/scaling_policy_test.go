package usecase

import (
	"legacy-messenger-control-plane/internal/domain"
	"testing"
	"time"
)

// 테스트 목표
// maintain 테스트

// effective capacity per task = 80
// 현재 desired = 2
// 전체 session = 81
// 필요 task 수 = ceil(81 / 80) = 2
// → desired 2 유지

func TestScalingPolicy_MaintainsWhenSessionsRequireTwoTasks(t *testing.T) {
	// Given - 테스트 상태 준비

	// policy 판단의 기준이되는 데이터들을 뽑아내기 위해서
	// calculateRequiredTaskCount와 evaluateScalingDemand를 조합해 ScalingPolicy.Evaluate에 들어갈 입력을 만들어주는 방식이 자연스러움.

	totalSessions := 81
	sessionPerTask := 100
	scaleOutUtilization := 0.8
	minTaskCount := 1
	maxTaskCount := 5

	requiredTaskCount := calculateRequiredTaskCount(totalSessions, sessionPerTask, scaleOutUtilization, minTaskCount, maxTaskCount)

	if requiredTaskCount != 2 {
		t.Fatalf("expected required task count 2, got %d", requiredTaskCount)
	}

	currentDesiredCount := 2
	currentRunningCount := 2

	demandResult := evaluateScalingDemand(
		"test-service",
		domain.TaskSessionInfo{TotalSessionCount: totalSessions},
		currentDesiredCount,
		currentRunningCount,
		requiredTaskCount,
	)

	policy := NewScalingPolicy()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// When - 정책 함수 호출

	reportCoverage := 1.0

	result, approved := policy.Evaluate(
		demandResult,
		domain.ECSServiceControlState{
			DesiredCount: 2,
			RunningCount: 2,
			PendingCount: 0,
		},
		reportCoverage,
		now,
	)

	// Then - 결과 확인
	if approved {
		t.Fatal("expected scaling not approved")
	}

	if result.Action != domain.ScalingActionKeep {
		t.Fatalf("expected KEEP, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 2 {
		t.Fatalf("expected recommended desired count 2, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// scale in 판단
// effective capacity per task = 80
// 현재 desired = 2
// 전체 session = 80
// 필요 task 수 = ceil(80 / 80) = 1
// → desired 1 판단
func TestScalingPolicy_RecognizesScaleInAtEffectiveCapacityBoundary(t *testing.T) {
	// Given - 테스트 상태 준비

	// policy 판단의 기준이되는 데이터들을 뽑아내기 위해서
	// calculateRequiredTaskCount와 evaluateScalingDemand를 조합해 ScalingPolicy.Evaluate에 들어갈 입력을 만들어주는 방식이 자연스러움.

	totalSessions := 80
	sessionPerTask := 100
	scaleOutUtilization := 0.8
	minTaskCount := 1
	maxTaskCount := 5

	requiredTaskCount := calculateRequiredTaskCount(totalSessions, sessionPerTask, scaleOutUtilization, minTaskCount, maxTaskCount)

	if requiredTaskCount != 1 {
		t.Fatalf("expected required task count 1, got %d", requiredTaskCount)
	}

	currentDesiredCount := 2
	currentRunningCount := 2

	demandResult := evaluateScalingDemand(
		"test-service",
		domain.TaskSessionInfo{TotalSessionCount: totalSessions},
		currentDesiredCount,
		currentRunningCount,
		requiredTaskCount,
	)

	policy := NewScalingPolicy()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// When - 정책 함수 호출

	reportCoverage := 1.0

	result, approved := policy.Evaluate(
		demandResult,
		domain.ECSServiceControlState{
			DesiredCount: 2,
			RunningCount: 2,
			PendingCount: 0,
		},
		reportCoverage,
		now,
	)

	// Then - 결과 확인
	if approved {
		t.Fatal("expected scaling not approved")
	}

	if result.Action != domain.ScalingActionScaleIn {
		t.Fatalf("expected SCALE_IN, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 1 {
		t.Fatalf("expected recommended desired count 1, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// 계산 결과가 max 초과일때

// 현재 desired: 8
// 한 번에 증가 가능한 step: 2
// step 기준 최대 target: 10
// policy 전체 max desired: 10
// 최종 recommended desired: 10

func TestScalingPolicy_DoesNotExceedMaxDesiredCount(t *testing.T) {
	// Given - policy에 들어온 scale-out 추천값이 maxDesiredCount를 초과하는 상황
	policy := NewScalingPolicy()

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             "test-service",
		CurrentDesiredCount:     8,
		RecommendedDesiredCount: 20,
		Action:                  domain.ScalingActionScaleOut,
		Executed:                false,
		Reason:                  "required task count exceeds current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 8,
		RunningCount: 8,
		PendingCount: 0,
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportCoverage := 1.0

	// When - scale-out은 기본적으로 3회 연속 조건을 만족해야 승인된다.
	var result domain.SessionAutoScalingResult
	var approved bool

	for i := 0; i < 3; i++ {
		result, approved = policy.Evaluate(
			demandResult,
			ecsState,
			reportCoverage,
			now,
		)
	}

	// Then - 추천값은 policy의 maxDesiredCount인 10으로 제한된다.
	if !approved {
		t.Fatal("expected scaling approved")
	}

	if result.Action != domain.ScalingActionScaleOut {
		t.Fatalf("expected SCALE_OUT, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 10 {
		t.Fatalf("expected recommended desired count 10, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// scale-in 추천값이 현재 desired보다 많이 낮을때

// 현재 desired: 3
// 한 번에 감소 가능한 step: 1
// step 기준 target: 2
// policy 전체 min desired: 1
// 최종 recommended desired: 2

func TestScalingPolicy_LimitsScaleInTargetByMaxScaleInStep(t *testing.T) {
	// Given - policy에 들어온 scale-in 추천값이 한 번에 감소 가능한 step보다 큰 상황
	policy := NewScalingPolicy()

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             "test-service",
		CurrentDesiredCount:     3,
		RecommendedDesiredCount: 1,
		Action:                  domain.ScalingActionScaleIn,
		Executed:                false,
		Reason:                  "required task count go below current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 3,
		RunningCount: 3,
		PendingCount: 0,
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportCoverage := 1.0

	// When - scale-in은 기본적으로 5회 연속 조건을 만족해야 승인된다.
	var result domain.SessionAutoScalingResult
	var approved bool

	for i := 0; i < 5; i++ {
		result, approved = policy.Evaluate(
			demandResult,
			ecsState,
			reportCoverage,
			now,
		)
	}

	// Then - 추천값은 policy의 maxScaleInStep에 따라 2로 제한된다.
	if !approved {
		t.Fatal("expected scaling approved")
	}

	if result.Action != domain.ScalingActionScaleIn {
		t.Fatalf("expected SCALE_IN, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 2 {
		t.Fatalf("expected recommended desired count 2, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// 현재 desired가 minDesiredCount이면 1 미만으로 scale-in하지 않는다.

// 현재 desired: 1
// policy 전체 min desired: 1
// 최종 recommended desired: 0
// → scale-in 승인 안함

func TestScalingPolicy_DoesNotApproveScaleInBelowMinDesiredCount(t *testing.T) {
	// Given - 현재 desired가 이미 policy의 minDesiredCount인 상황
	policy := NewScalingPolicy()

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             "test-service",
		CurrentDesiredCount:     1,
		RecommendedDesiredCount: 0,
		Action:                  domain.ScalingActionScaleIn,
		Executed:                false,
		Reason:                  "required task count is below current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 1,
		RunningCount: 1,
		PendingCount: 0,
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportCoverage := 1.0

	// When - scale-in 최소 desiredCount 제한을 평가한다.
	result, approved := policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	// Then - 현재 desired가 minDesiredCount이므로 scale-in은 승인되지 않는다.
	if approved {
		t.Fatal("expected scaling not approved")
	}

	// Action은 원래 판단된 스케일링 방향을 나타내고, approved는 정책이 실제 실행을 허용했는지를 나타내므로 1 -> 0이 실패하더라도 KEEP 아님
	if result.Action != domain.ScalingActionScaleIn {
		t.Fatalf("expected SCALE_IN, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 0 {
		t.Fatalf("expected recommended desired count 0, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// scale-out cooldown 중이면 scale-out을 승인하지 않는다.

// 직전 scale-out 실행: 10초 전
// scale-out cooldown: 30초
// 현재 action: SCALE_OUT
// → action은 유지되지만 scale-out 승인 안함

func TestScalingPolicy_DoesNotApproveScaleOutDuringCooldown(t *testing.T) {
	// Given - 같은 서비스에서 직전 scale-out 실행 시간이 cooldown 안에 있는 상황
	policy := NewScalingPolicy()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	serviceName := "test-service"

	policy.RecordExecution(
		serviceName,
		string(domain.ScalingActionScaleOut),
		now.Add(-10*time.Second),
	)

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             serviceName,
		CurrentDesiredCount:     2,
		RecommendedDesiredCount: 4,
		Action:                  domain.ScalingActionScaleOut,
		Executed:                false,
		Reason:                  "required task count exceeds current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 2,
		RunningCount: 2,
		PendingCount: 0,
	}

	reportCoverage := 1.0

	// When - scale-out cooldown 제한을 평가한다.
	result, approved := policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	// Then - cooldown 중이므로 scale-out은 승인되지 않는다.
	if approved {
		t.Fatal("expected scaling not approved")
	}

	if result.Action != domain.ScalingActionScaleOut {
		t.Fatalf("expected SCALE_OUT, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 4 {
		t.Fatalf("expected recommended desired count 4, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// scale-out 조건이 연속 판단 횟수를 만족하기 전까지는 승인하지 않는다.

// scale-out consecutive count: 3
// 1번째 평가: 승인 안함
// 2번째 평가: 승인 안함
// 3번째 평가: 승인

func TestScalingPolicy_DoesNotApproveScaleOutBeforeConsecutiveCountIsMet(t *testing.T) {
	// Given - scale-out 조건이 반복해서 들어오는 상황
	policy := NewScalingPolicy()

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             "test-service",
		CurrentDesiredCount:     2,
		RecommendedDesiredCount: 4,
		Action:                  domain.ScalingActionScaleOut,
		Executed:                false,
		Reason:                  "required task count exceeds current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 2,
		RunningCount: 2,
		PendingCount: 0,
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportCoverage := 1.0

	// When & Then - 1번째 평가는 승인되지 않는다.
	result, approved := policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	if approved {
		t.Fatal("expected first scale-out evaluation not approved")
	}

	if result.Action != domain.ScalingActionScaleOut {
		t.Fatalf("expected SCALE_OUT, got %s", result.Action)
	}

	// When & Then - 2번째 평가도 아직 승인되지 않는다.
	result, approved = policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	if approved {
		t.Fatal("expected second scale-out evaluation not approved")
	}

	if result.Action != domain.ScalingActionScaleOut {
		t.Fatalf("expected SCALE_OUT, got %s", result.Action)
	}

	// When & Then - 3번째 평가에서 연속 판단 조건을 만족해 승인된다.
	result, approved = policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	if !approved {
		t.Fatal("expected third scale-out evaluation approved")
	}

	if result.Action != domain.ScalingActionScaleOut {
		t.Fatalf("expected SCALE_OUT, got %s", result.Action)
	}

	if result.RecommendedDesiredCount != 4 {
		t.Fatalf("expected recommended desired count 4, got %d", result.RecommendedDesiredCount)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}

// 테스트 목표
// pending task가 있으면 ECS가 수렴 상태가 아니므로 scale-out 평가를 skip한다.

// 현재 action: SCALE_OUT
// ECS pending count: 1
// → action은 SCALE_ACTION_SKIP으로 변경되고 scale-out 승인 안함

func TestScalingPolicy_SkipsScaleOutWhenTaskIsPending(t *testing.T) {
	// Given - 이미 새로운 task가 pending 상태인 상황에서 추가 scale-out 요청이 들어온다.
	policy := NewScalingPolicy()

	demandResult := domain.SessionAutoScalingResult{
		ServiceName:             "test-service",
		CurrentDesiredCount:     2,
		RecommendedDesiredCount: 4,
		Action:                  domain.ScalingActionScaleOut,
		Executed:                false,
		Reason:                  "required task count exceeds current desired count",
	}

	ecsState := domain.ECSServiceControlState{
		DesiredCount: 2,
		RunningCount: 2,
		PendingCount: 1,
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	reportCoverage := 1.0

	// When
	result, approved := policy.Evaluate(
		demandResult,
		ecsState,
		reportCoverage,
		now,
	)

	// Then
	if approved {
		t.Fatal("expected scale-out not approved while task is pending")
	}

	if result.Action != domain.ScaleActionSkip {
		t.Fatalf("expected SCALE_ACTION_SKIP, got %s", result.Action)
	}

	if result.Executed {
		t.Fatal("expected not executed")
	}
}
