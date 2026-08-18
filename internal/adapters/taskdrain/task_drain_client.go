package taskdrain

import (
	"context"
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

	return nil
}

func (c *TaskDrainClient) CancelDrain(
	ctx context.Context,
	serviceName string,
	taskID string,
) error {

	return nil
}
