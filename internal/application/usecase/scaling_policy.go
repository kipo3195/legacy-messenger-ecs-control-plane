package usecase

import (
	"fmt"
	"legacy-messenger-control-plane/internal/domain"
	"sync"
	"time"
)

// ScalingPolicy는 스케일링을 지금 실행할 수 있는지 판단한다.
//
// 현재 구현은 연속 판단 횟수와 마지막 실행 시간을 메모리에 저장한다.
// 따라서 프로세스가 재시작되면 해당 상태는 초기화된다.
type ScalingPolicy struct {
	minDesiredCount int
	maxDesiredCount int

	maxScaleOutStep int
	maxScaleInStep  int

	scaleOutConsecutiveCount int
	scaleInConsecutiveCount  int

	scaleOutCooldown time.Duration
	scaleInCooldown  time.Duration

	minReportCoverage float64

	mu sync.Mutex

	// 서비스별 정책 상태
	scaleOutStreak map[string]int
	scaleInStreak  map[string]int

	lastScaleOutAt map[string]time.Time
	lastScaleInAt  map[string]time.Time
}

func NewScalingPolicy() *ScalingPolicy {
	return &ScalingPolicy{
		minDesiredCount: 1,
		maxDesiredCount: 10,

		maxScaleOutStep: 2,
		maxScaleInStep:  1,

		scaleOutConsecutiveCount: 3,
		scaleInConsecutiveCount:  5,

		scaleOutCooldown: 30 * time.Second,
		scaleInCooldown:  1 * time.Minute,

		minReportCoverage: 0.8,

		scaleOutStreak: make(map[string]int),
		scaleInStreak:  make(map[string]int),

		lastScaleOutAt: make(map[string]time.Time),
		lastScaleInAt:  make(map[string]time.Time),
	}
}

// Evaluate는 demand 판단 결과에 정책을 적용한다.
//
// 반환값:
//   - SessionAutoScalingResult: 정책 적용 후 결과
//   - bool: 실제 스케일링 실행 승인 여부
func (p *ScalingPolicy) Evaluate(
	result domain.SessionAutoScalingResult,
	ecsState domain.ECSServiceControlState,
	reportCoverage float64,
	now time.Time,
) (domain.SessionAutoScalingResult, bool) {

	p.mu.Lock()
	defer p.mu.Unlock()

	// 유지 판단이면 ECS 수렴 여부와 무관하게 실행할 것이 없음
	if result.Action == domain.ScalingActionKeep {
		p.resetStreak(result.ServiceName)
		result.Executed = false
		result.Reason = "scaling is not required"

		return result, false
	}

	// 수렴 여부 파악
	if !isECSConverged(ecsState) {
		result.Action = domain.ScaleActionSkip
		result.Executed = false
		result.Reason = fmt.Sprintf(
			"ecs service is not converged: desired=%d running=%d pending=%d",
			ecsState.DesiredCount,
			ecsState.RunningCount,
			ecsState.PendingCount,
		)
		return result, false
	}

	switch result.Action {
	case domain.ScalingActionScaleOut:
		return p.evaluateScaleOut(
			result,
			ecsState,
			now,
		)

	case domain.ScalingActionScaleIn:
		return p.evaluateScaleIn(
			result,
			ecsState,
			reportCoverage,
			now,
		)

	default:
		p.resetStreak(result.ServiceName)

		result.Executed = false
		result.Reason = fmt.Sprintf(
			"unsupported scaling action: %s",
			result.Action,
		)

		return result, false
	}
}

func (p *ScalingPolicy) evaluateScaleOut(
	result domain.SessionAutoScalingResult,
	ecsState domain.ECSServiceControlState,
	now time.Time,
) (domain.SessionAutoScalingResult, bool) {

	serviceName := result.ServiceName

	// 반대 방향의 연속 판단 횟수 초기화
	p.scaleInStreak[serviceName] = 0
	p.scaleOutStreak[serviceName]++

	if result.CurrentDesiredCount >= p.maxDesiredCount {
		result.Reason = "maximum desired count has been reached"
		return result, false
	}

	// 이전 Scale-out으로 Task가 아직 생성 중이면 추가 확장하지 않는다.
	if ecsState.PendingCount > 0 {
		result.Reason = fmt.Sprintf(
			"scale-out deferred because pending tasks exist: pendingCount=%d",
			ecsState.PendingCount,
		)
		return result, false
	}

	lastScaleOutAt := p.lastScaleOutAt[serviceName]

	if !lastScaleOutAt.IsZero() &&
		now.Sub(lastScaleOutAt) < p.scaleOutCooldown {

		remaining := p.scaleOutCooldown - now.Sub(lastScaleOutAt)

		result.Reason = fmt.Sprintf(
			"scale-out cooldown is active: remaining=%s",
			remaining.Round(time.Second),
		)
		return result, false
	}

	currentStreak := p.scaleOutStreak[serviceName]

	if currentStreak < p.scaleOutConsecutiveCount {
		result.Reason = fmt.Sprintf(
			"scale-out condition is not persistent enough: current=%d, required=%d",
			currentStreak,
			p.scaleOutConsecutiveCount,
		)
		return result, false
	}

	targetDesiredCount := result.RecommendedDesiredCount

	// 한 번에 증가할 수 있는 Task 수 제한
	maxTargetDesiredCount :=
		result.CurrentDesiredCount + p.maxScaleOutStep

	if targetDesiredCount > maxTargetDesiredCount {
		targetDesiredCount = maxTargetDesiredCount
	}

	if targetDesiredCount > p.maxDesiredCount {
		targetDesiredCount = p.maxDesiredCount
	}

	if targetDesiredCount <= result.CurrentDesiredCount {
		result.Reason = "scale-out target does not exceed current desired count"
		return result, false
	}

	result.RecommendedDesiredCount = targetDesiredCount
	result.Reason = fmt.Sprintf(
		"scale-out policy approved: consecutiveCount=%d, targetDesiredCount=%d",
		currentStreak,
		targetDesiredCount,
	)

	// 이번 판단이 승인되었으므로 연속 횟수 초기화
	p.scaleOutStreak[serviceName] = 0

	return result, true
}

func isECSConverged(
	state domain.ECSServiceControlState) bool {
	// desired running pending
	//.  3.      3.      0.  -> OK
	//.  3.      2.      1   -> Scale out
	//.  2.      3.      0.  -> Scale in
	//.  3.      2.      0.  -> 비정상
	return state.PendingCount == 0 &&
		state.RunningCount == state.DesiredCount
}

func (p *ScalingPolicy) evaluateScaleIn(
	result domain.SessionAutoScalingResult,
	ecsState domain.ECSServiceControlState,
	reportCoverage float64,
	now time.Time,
) (domain.SessionAutoScalingResult, bool) {

	serviceName := result.ServiceName

	// 반대 방향의 연속 판단 횟수 초기화
	p.scaleOutStreak[serviceName] = 0

	if result.CurrentDesiredCount <= p.minDesiredCount {
		p.scaleInStreak[serviceName] = 0
		result.Reason = "minimum desired count has been reached"

		return result, false
	}

	// pending 상태가 있으면 판단 횟수 초기화, pending으로 인한 종료
	if ecsState.PendingCount > 0 {
		p.scaleInStreak[serviceName] = 0

		result.Reason = fmt.Sprintf(
			"scale-in deferred because pending tasks exist: pendingCount=%d",
			ecsState.PendingCount,
		)
		return result, false
	}

	// Scale-in은 세션 보고가 충분히 신뢰될 때만 판단한다.
	if reportCoverage < p.minReportCoverage {
		p.scaleInStreak[serviceName] = 0

		result.Reason = fmt.Sprintf(
			"scale-in deferred because report coverage is insufficient: current=%.2f, required=%.2f",
			reportCoverage,
			p.minReportCoverage,
		)
		return result, false
	}

	// Scale-out 직후에는 세션 재분배 시간을 확보한다.
	lastScaleOutAt := p.lastScaleOutAt[serviceName]

	// 마지막 scale out 시간을 기준으로 계산
	if !lastScaleOutAt.IsZero() &&
		// 현재 시각 - 마지막 Scale-out시간이 설정된 Cooldown보다 짧은 경우
		now.Sub(lastScaleOutAt) < p.scaleInCooldown {

		// Scale-out 승인 거절
		p.scaleInStreak[serviceName] = 0

		remaining := p.scaleInCooldown - now.Sub(lastScaleOutAt)

		result.Reason = fmt.Sprintf(
			"scale-in blocked during scale-out stabilization: remaining=%s",
			remaining.Round(time.Second),
		)
		return result, false
	}

	lastScaleInAt := p.lastScaleInAt[serviceName]

	if !lastScaleInAt.IsZero() &&
		now.Sub(lastScaleInAt) < p.scaleInCooldown {

		p.scaleInStreak[serviceName] = 0

		remaining := p.scaleInCooldown - now.Sub(lastScaleInAt)

		result.Reason = fmt.Sprintf(
			"scale-in cooldown is active: remaining=%s",
			remaining.Round(time.Second),
		)
		return result, false
	}

	p.scaleInStreak[serviceName]++

	currentStreak := p.scaleInStreak[serviceName]

	if currentStreak < p.scaleInConsecutiveCount {
		result.Reason = fmt.Sprintf(
			"scale-in condition is not persistent enough: current=%d, required=%d",
			currentStreak,
			p.scaleInConsecutiveCount,
		)
		return result, false
	}

	// Scale-in은 한 번에 하나 또는 설정된 step만큼만 진행한다.
	targetDesiredCount :=
		result.CurrentDesiredCount - p.maxScaleInStep

	// 정책 step이 실제 추천 수보다 아래까지 내려가지 않도록 제한
	if targetDesiredCount < result.RecommendedDesiredCount {
		targetDesiredCount = result.RecommendedDesiredCount
	}

	if targetDesiredCount < p.minDesiredCount {
		targetDesiredCount = p.minDesiredCount
	}

	if targetDesiredCount >= result.CurrentDesiredCount {
		result.Reason = "scale-in target is not below current desired count"
		return result, false
	}

	result.RecommendedDesiredCount = targetDesiredCount
	result.Reason = fmt.Sprintf(
		"scale-in policy approved: consecutiveCount=%d, targetDesiredCount=%d",
		currentStreak,
		targetDesiredCount,
	)

	p.scaleInStreak[serviceName] = 0

	return result, true
}

// RecordExecution은 실제 실행에 성공한 시점을 기록한다.
// Evaluate 승인 시점이 아니라 ECS 변경 또는 drain 시작 성공 후 호출한다.
func (p *ScalingPolicy) RecordExecution(
	serviceName string,
	action string,
	executedAt time.Time,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch action {
	case string(domain.ScalingActionScaleOut):
		p.lastScaleOutAt[serviceName] = executedAt

	case string(domain.ScalingActionScaleIn):
		p.lastScaleInAt[serviceName] = executedAt
	}
}

func (p *ScalingPolicy) resetStreak(
	serviceName string,
) {
	p.scaleOutStreak[serviceName] = 0
	p.scaleInStreak[serviceName] = 0
}
