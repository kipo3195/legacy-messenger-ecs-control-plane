package ports

import (
	"context"

	"legacy-messenger-control-plane/internal/domain"
)

type TaskLifecycleNotifier interface {
	NotifyTaskRunning(ctx context.Context, event domain.TaskRunningEvent) error
	NotifyTaskStopped(ctx context.Context, serviceName string, taskID string) error
}
