package taskdrain

import (
	"context"
	"fmt"
	"legacy-messenger-control-plane/internal/ports"
	"net/http"
	"time"
)

var _ ports.TaskDrainPort = (*TaskDrainClient)(nil)

type TaskDrainClient struct {
	httpClient *http.Client
	resolver   ports.TaskEndpointResolver
}

func NewTaskDrainClient(
	resolver ports.TaskEndpointResolver,
) *TaskDrainClient {
	return &TaskDrainClient{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		resolver: resolver,
	}
}

type TaskDrainRequest struct {
	TaskID string
}

func (c *TaskDrainClient) RequestDrain(
	ctx context.Context,
	serviceName string,
	taskID string,
) error {
	// 가장 적은 session Count를 갖는 task에 drain 요청
	endpoint, err := c.resolver.ResolveTaskEndpoint(
		ctx,
		serviceName,
		taskID,
	)
	if err != nil {
		fmt.Printf("[RequestDrain] resolve endpoint error! TaskID: %s", taskID)
		return err
	}

	fmt.Printf("[RequestDrain] resolve endpoint : %s", endpoint)

	return nil
}

func (c *TaskDrainClient) CancelDrain(
	ctx context.Context,
	serviceName string,
	taskID string,
) error {

	return nil
}
