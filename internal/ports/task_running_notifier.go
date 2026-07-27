package ports

import (
	"context"

	"legacy-messenger-control-plane/internal/domain"
)

type TaskRunningNotifier interface {
	NotifyTaskRunning(
		ctx context.Context,
		event domain.TaskRunningEvent,
	) error
}
