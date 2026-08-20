package ports

import (
	"context"
	"legacy-messenger-control-plane/internal/domain"
)

type ECSPort interface {
	DescribeService(ctx context.Context, clusterName string, ecsServiceName string) (*domain.ServiceStatus, error)
	DescribeTask(ctx context.Context, clusterName string, taskID string) (domain.ECSTask, error)
	DescribeTasks(ctx context.Context, clusterName string, ecsServiceName string, desiredStatus string) ([]domain.TaskStatus, error)
	GetServiceTargetGroups(ctx context.Context, clusterName string, ecsServiceName string) ([]domain.ServiceTargetGroup, error)
	GetServiceTargetGroupArn(ctx context.Context, clusterName string, ecsServiceName string) (string, error)
	GetServiceControlState(ctx context.Context, clusterName string, ecsServiceName string) (domain.ECSServiceControlState, error)
	UpdateServiceDesiredCount(ctx context.Context, clusterName string, ecsServiceName string, desiredCount int) (domain.ECSServiceControlState, error)
	ForceNewDeployment(ctx context.Context, clusterName string, ecsServiceName string) (domain.ServiceRedeployResult, error)
	GetRunningTaskIDs(ctx context.Context, clusterName string, ecsServiceName string) ([]string, error)
	UpdateTaskProtection(ctx context.Context, clusterName string, protectedTaskIDs []string, flag bool) error
	GetContainerInstanceEC2ID(ctx context.Context, clusterName string, containerInstanceARN string) (string, error)
}
