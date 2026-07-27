package fake

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"legacy-messenger-control-plane/internal/domain"
	"legacy-messenger-control-plane/internal/ports"

	"github.com/google/uuid"
)

// 내부 메모리에 각 서비스 별 desired count까지 관리 할 수 있도록 함
type UpdateDesiredCountCall struct {
	ServiceName  string
	DesiredCount int32
}

type ECSClient struct {
	mu sync.RWMutex

	serviceStates map[string]domain.ECSServiceControlState // desired, running, pending
	redeployCount map[string]int

	tasks map[string]domain.TaskStatus

	UpdateDesiredCountCalls []UpdateDesiredCountCall
	UpdateDesiredCountErr   error

	// Task 하나가 PENDING에서 RUNNING으로 전환되는 시간
	taskStartDelay time.Duration

	taskRunningNotifier ports.TaskRunningNotifier
}

var _ ports.ECSPort = (*ECSClient)(nil)

func NewECSClient(
	initialStates map[string]domain.ECSServiceControlState,
	taskRunningNotifier ports.TaskRunningNotifier,
) *ECSClient {
	// map 복사
	states := make(map[string]domain.ECSServiceControlState, len(initialStates))

	for serviceName, state := range initialStates {
		states[serviceName] = state
	}

	return &ECSClient{
		serviceStates:       states,
		redeployCount:       make(map[string]int),
		tasks:               make(map[string]domain.TaskStatus),
		taskStartDelay:      15 * time.Second,
		taskRunningNotifier: taskRunningNotifier,
	}
}

func (c *ECSClient) GetServiceControlState(ctx context.Context, clusterName string, serviceName string) (domain.ECSServiceControlState, error) {
	if err := ctx.Err(); err != nil {
		return domain.ECSServiceControlState{}, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	state, exists := c.serviceStates[serviceName]
	if !exists {
		return domain.ECSServiceControlState{},
			fmt.Errorf("fake ECS service not found: %s", serviceName)
	}

	return state, nil
}

func (c *ECSClient) DescribeService(ctx context.Context, clusterName string, ecsServiceName string) (*domain.ServiceStatus, error) {
	return nil, nil
}
func (c *ECSClient) DescribeTasks(ctx context.Context, clusterName string, ecsServiceName string, desiredStatus string) ([]domain.TaskStatus, error) {
	return nil, nil
}

func (c *ECSClient) GetServiceTargetGroups(ctx context.Context, clusterName string, ecsServiceName string) ([]domain.ServiceTargetGroup, error) {
	return nil, nil
}

func (c *ECSClient) GetServiceTargetGroupArn(ctx context.Context, clusterName string, ecsServiceName string) (string, error) {
	return "", nil
}

func (c *ECSClient) UpdateServiceDesiredCount(
	ctx context.Context,
	clusterName string,
	serviceName string,
	desiredCount int,
) (domain.ECSServiceControlState, error) {

	// 1. 기존 desiredCount보다 큰 요청인지 검증
	// 2. DesiredCount 즉시 증가
	// 3. 부족한 Task 수만큼 PendingCount 증가

	if err := ctx.Err(); err != nil {
		return domain.ECSServiceControlState{}, err
	}

	if desiredCount < 0 {
		return domain.ECSServiceControlState{},
			fmt.Errorf("desired count must not be negative: %d", desiredCount)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.serviceStates[serviceName]
	if !exists {
		return domain.ECSServiceControlState{},
			fmt.Errorf("fake ECS service not found: %s", serviceName)
	}

	if desiredCount <= int(state.DesiredCount) {

		return domain.ECSServiceControlState{},
			fmt.Errorf(
				"fake ECS scale-out requires desired count increase: service=%s current=%d requested=%d",
				serviceName,
				state.DesiredCount,
				desiredCount,
			)
	}

	currentTaskCount := int(
		state.RunningCount + state.PendingCount,
	)

	missingTaskCount := desiredCount - currentTaskCount

	state.DesiredCount = int32(desiredCount)

	if missingTaskCount > 0 {
		state.PendingCount += int32(missingTaskCount)
	}

	c.serviceStates[serviceName] = state
	result := state

	// missingTaskCount 목표 Task 수에서 이미 실행 중이거나 시작 중인 Task 수를 뺀 값
	// desired 5, running 3, pending 1
	// missingTaskCount = 5 - (3 + 1) = 1
	//

	taskIDs := make([]string, 0, missingTaskCount)

	for i := 0; i < missingTaskCount; i++ {
		taskID := createTaskID(serviceName)

		now := time.Now()

		c.tasks[taskID] = domain.TaskStatus{
			TaskID:        taskID,
			LastStatus:    "PENDING",
			DesiredStatus: "RUNNING",
			CreatedAt:     &now,
		}
		log.Printf("[UpdateServiceDesiredCount] new task ID : %s\n", taskID)
		taskIDs = append(taskIDs, taskID)
	}

	for order, taskID := range taskIDs {
		go c.transitionPendingTaskToRunning(
			serviceName,
			taskID,
			order,
		)
	}

	return result, nil
}

func (c *ECSClient) transitionPendingTaskToRunning(
	serviceName string,
	taskID string,
	order int,
) {
	delay := c.taskStartDelay +
		time.Duration(order)*500*time.Millisecond

	timer := time.NewTimer(delay)
	defer timer.Stop()

	<-timer.C

	c.mu.Lock()

	state, exists := c.serviceStates[serviceName]
	if !exists {
		c.mu.Unlock()
		return
	}

	task, exists := c.tasks[taskID]
	if !exists {
		c.mu.Unlock()
		return
	}

	if task.LastStatus != "PENDING" {
		c.mu.Unlock()
		return
	}

	if state.PendingCount <= 0 ||
		state.RunningCount >= state.DesiredCount {
		c.mu.Unlock()
		return
	}

	now := time.Now()

	task.LastStatus = "RUNNING"
	task.StartedAt = &now

	state.PendingCount--
	state.RunningCount++

	c.tasks[taskID] = task
	c.serviceStates[serviceName] = state

	// 외부 호출용 복사본
	runningTask := task

	c.mu.Unlock()

	c.notifyTaskRunning(
		serviceName,
		runningTask,
	)
}

func createTaskID(serviceName string) string {
	serviceName = strings.TrimSpace(serviceName)

	return fmt.Sprintf("%s-%s", serviceName, uuid.NewString())
}

func (c *ECSClient) notifyTaskRunning(
	serviceName string,
	task domain.TaskStatus,
) {
	if c.taskRunningNotifier == nil {
		return
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		3*time.Second,
	)
	defer cancel()

	err := c.taskRunningNotifier.NotifyTaskRunning(
		ctx,
		domain.TaskRunningEvent{
			ServiceName: serviceName,
			TaskID:      task.TaskID,
			StartedAt:   task.StartedAt,
		},
	)
	if err != nil {
		log.Printf(
			"failed to register running task to session provider: service=%s taskID=%s error=%v",
			serviceName,
			task.TaskID,
			err,
		)
	}
}

func (c *ECSClient) ForceNewDeployment(ctx context.Context, clusterName string, ecsServiceName string) (domain.ServiceRedeployResult, error) {
	return domain.ServiceRedeployResult{}, nil
}

func (c *ECSClient) GetRunningTaskIDs(ctx context.Context, clusterName string, ecsServiceName string) ([]string, error) {
	return nil, nil
}

func (c *ECSClient) UpdateTaskProtection(ctx context.Context, clusterName string, protectedTaskIDs []string, flag bool) error {
	return nil
}

func (c *ECSClient) DescribeTask(ctx context.Context, clusterName string, taskID string) (domain.ECSTask, error) {
	return domain.ECSTask{}, nil
}
