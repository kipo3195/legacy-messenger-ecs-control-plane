package aws

import (
	"context"
	"fmt"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/ports"
)

var _ ports.TaskEndpointResolver = (*ECSTaskEndpointResolver)(nil)

type ECSTaskEndpointResolver struct {
	ecsPort ports.ECSPort
	ecsCfg  *configs.ECSConfig
	ec2Port ports.EC2Port

	managementPort int
}

func NewECSTaskEndpointResolver(
	ecsPort ports.ECSPort,
	ecsCfg *configs.ECSConfig,
	ec2Port ports.EC2Port,
	managementPort int,
) *ECSTaskEndpointResolver {
	return &ECSTaskEndpointResolver{
		ecsPort:        ecsPort,
		ecsCfg:         ecsCfg,
		ec2Port:        ec2Port,
		managementPort: managementPort,
	}
}

func (r *ECSTaskEndpointResolver) ResolveTaskEndpoint(ctx context.Context, serviceName string, taskID string) (string, error) {

	task, err := r.ecsPort.DescribeTask(
		ctx,
		r.ecsCfg.ClusterName,
		taskID,
	)

	if err != nil {
		return "", fmt.Errorf(
			"failed to describe task: taskID=%s: %w",
			taskID,
			err,
		)
	}

	// if task.PrivateIP == "" {
	// 	return "", fmt.Errorf(
	// 		"task private IP is empty: taskID=%s",
	// 		taskID,
	// 	)
	// }

	if task.ContainerInstanceARN == "" {
		return "", fmt.Errorf(
			"container instance ARN is empty: taskID=%s",
			taskID,
		)
	}

	var hostPort int32

	for _, binding := range task.NetworkBindings {
		if binding.ContainerPort == int32(r.managementPort) {
			hostPort = binding.HostPort
			break
		}
	}

	if hostPort == 0 {
		return "", fmt.Errorf(
			"management host port not found: taskID=%s containerPort=%d",
			taskID,
			r.managementPort,
		)
	}

	ec2InstanceID, err := r.ecsPort.GetContainerInstanceEC2ID(
		ctx,
		r.ecsCfg.ClusterName,
		task.ContainerInstanceARN,
	)

	if err != nil {
		return "", err
	}

	privateIP, err := r.ec2Port.GetPrivateIP(
		ctx,
		ec2InstanceID,
	)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"%s:%d",
		privateIP,
		hostPort,
	), nil
}
