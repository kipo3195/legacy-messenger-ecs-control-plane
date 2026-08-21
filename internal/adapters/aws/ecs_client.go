package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/domain"
	"legacy-messenger-control-plane/internal/ports"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

var _ ports.ECSPort = (*ECSClient)(nil)

type ECSClient struct {
	client *ecs.Client
}

func NewECSClient(ctx context.Context, cfg *configs.Config) (*ECSClient, error) {
	if cfg.AWS.Region == "" {
		return nil, fmt.Errorf("aws region is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.AWS.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config: %w", err)
	}

	return &ECSClient{
		client: ecs.NewFromConfig(awsCfg),
	}, nil
}

func (c *ECSClient) DescribeService(ctx context.Context, clusterName string, ecsServiceName string) (*domain.ServiceStatus, error) {

	if clusterName == "" {
		return nil, fmt.Errorf("clusterName is required")
	}

	if ecsServiceName == "" {
		return nil, fmt.Errorf("ecsServiceName is required")
	}

	svc, err := c.describeECSService(ctx, clusterName, ecsServiceName)
	if err != nil {
		return nil, err
	}

	status := &domain.ServiceStatus{
		ServiceName:    ecsServiceName,
		ECSServiceName: ecsServiceName,
		ClusterName:    clusterName,
		Status:         ptrString(svc.Status),
		DesiredCount:   svc.DesiredCount,
		RunningCount:   svc.RunningCount,
		PendingCount:   svc.PendingCount,
		TaskDefinition: ptrString(svc.TaskDefinition),
		Deployments:    make([]domain.DeploymentStatus, 0),
		Events:         make([]domain.ServiceEvent, 0),
	}

	for _, d := range svc.Deployments {
		status.Deployments = append(status.Deployments, domain.DeploymentStatus{
			Status:       ptrString(d.Status),
			DesiredCount: d.DesiredCount,
			RunningCount: d.RunningCount,
			PendingCount: d.PendingCount,
			RolloutState: string(d.RolloutState),
		})
	}

	for _, e := range svc.Events {
		status.Events = append(status.Events, domain.ServiceEvent{
			Message:   ptrString(e.Message),
			CreatedAt: ptrTime(e.CreatedAt),
		})
	}

	return status, nil
}

func ptrString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func ptrTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}

// adpater에서 AWS라이브러리 참조 및 결과를 ->  []domain.TaskStatus 하지 않고 AWS라이브러리 호출 결과를 return하여 usecase에서 처리하게 되면
// usecase에서 외부 의존성 (AWS)를 알게 된다.
func (c *ECSClient) DescribeTasks(ctx context.Context, clusterName string, ecsServiceName string, desiredStatus string) ([]domain.TaskStatus, error) {

	if clusterName == "" {
		return nil, fmt.Errorf("clusterName is required")
	}

	if ecsServiceName == "" {
		return nil, fmt.Errorf("ecsServiceName is required")
	}

	status := toECSDesiredStatus(desiredStatus)

	// 특정 서비스의 Task를 조회하려면 먼저 ListTasks로 해당 서비스에 속한 Task ARN 목록을 가져오고,
	// 그 결과를 DescribeTasks에 넘겨야 해. AWS ListTasks는 serviceName으로 필터링할 수 있고, DescribeTasks는 tasks 배열을 필수로 받는다.

	listOut, err := c.client.ListTasks(ctx, &ecs.ListTasksInput{
		Cluster:       &clusterName,
		ServiceName:   &ecsServiceName,
		DesiredStatus: status,
	})

	if err != nil {
		return nil, fmt.Errorf("[ListServiceTasks] ListTasks error: %w", err)
	}

	if len(listOut.TaskArns) == 0 {
		return nil, nil
	}

	describeOut, err := c.client.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: &clusterName,
		Tasks:   listOut.TaskArns,
	})
	if err != nil {
		return nil, fmt.Errorf("[ListServiceTasks] DescribeTasks error: %w", err)
	}

	result := make([]domain.TaskStatus, 0, len(describeOut.Tasks))

	for _, task := range describeOut.Tasks {
		result = append(result, toTaskStatus(task))
	}

	return result, nil
}

func toTaskStatus(task types.Task) domain.TaskStatus {
	return domain.TaskStatus{
		TaskID:               extractID(aws.ToString(task.TaskArn)),
		TaskArn:              aws.ToString(task.TaskArn),
		TaskDefinition:       extractID(aws.ToString(task.TaskDefinitionArn)),
		LastStatus:           aws.ToString(task.LastStatus),
		DesiredStatus:        aws.ToString(task.DesiredStatus),
		HealthStatus:         string(task.HealthStatus),
		LaunchType:           string(task.LaunchType),
		AvailabilityZone:     aws.ToString(task.AvailabilityZone),
		ContainerInstanceArn: aws.ToString(task.ContainerInstanceArn),
		CapacityProviderName: aws.ToString(task.CapacityProviderName),

		CreatedAt:  task.CreatedAt,
		StartedAt:  task.StartedAt,
		StoppingAt: task.StoppingAt,
		StoppedAt:  task.StoppedAt,

		StopCode:      string(task.StopCode),
		StoppedReason: aws.ToString(task.StoppedReason),

		Containers: toContainerStatuses(task.Containers),
		Network:    toTaskNetworkInfo(task),
	}
}

func toContainerStatuses(containers []types.Container) []domain.ContainerStatus {
	result := make([]domain.ContainerStatus, 0, len(containers))

	for _, c := range containers {
		result = append(result, domain.ContainerStatus{
			Name:              aws.ToString(c.Name),
			LastStatus:        aws.ToString(c.LastStatus),
			HealthStatus:      string(c.HealthStatus),
			Image:             aws.ToString(c.Image),
			RuntimeID:         aws.ToString(c.RuntimeId),
			Reason:            aws.ToString(c.Reason),
			ExitCode:          c.ExitCode,
			NetworkBindings:   toNetworkBindings(c.NetworkBindings),
			NetworkInterfaces: toNetworkInterfaces(c.NetworkInterfaces),
		})
	}

	return result
}

func toNetworkBindings(bindings []types.NetworkBinding) []domain.NetworkBindingInfo {
	result := make([]domain.NetworkBindingInfo, 0, len(bindings))

	for _, b := range bindings {
		result = append(result, domain.NetworkBindingInfo{
			BindIP:        aws.ToString(b.BindIP),
			ContainerPort: aws.ToInt32(b.ContainerPort),
			HostPort:      aws.ToInt32(b.HostPort),
			Protocol:      string(b.Protocol),
		})
	}

	return result
}

func toNetworkInterfaces(interfaces []types.NetworkInterface) []domain.NetworkInterfaceInfo {
	result := make([]domain.NetworkInterfaceInfo, 0, len(interfaces))

	for _, ni := range interfaces {
		result = append(result, domain.NetworkInterfaceInfo{
			AttachmentID:       aws.ToString(ni.AttachmentId),
			PrivateIPv4Address: aws.ToString(ni.PrivateIpv4Address),
		})
	}

	return result
}

func toTaskNetworkInfo(task types.Task) domain.TaskNetworkInfo {
	var bindings []domain.NetworkBindingInfo
	var interfaces []domain.NetworkInterfaceInfo
	var privateIPv4 string

	for _, c := range task.Containers {
		bindings = append(bindings, toNetworkBindings(c.NetworkBindings)...)

		containerInterfaces := toNetworkInterfaces(c.NetworkInterfaces)
		interfaces = append(interfaces, containerInterfaces...)

		for _, ni := range containerInterfaces {
			if ni.PrivateIPv4Address != "" && privateIPv4 == "" {
				privateIPv4 = ni.PrivateIPv4Address
			}
		}
	}

	modeHint := "unknown"

	if len(bindings) > 0 {
		modeHint = "bridge"
	}

	if privateIPv4 != "" {
		modeHint = "awsvpc"
	}

	return domain.TaskNetworkInfo{
		ModeHint:    modeHint,
		Bindings:    bindings,
		Interfaces:  interfaces,
		PrivateIPv4: privateIPv4,
	}
}

func extractID(arn string) string {
	if arn == "" {
		return ""
	}

	parts := strings.Split(arn, "/")
	return parts[len(parts)-1]
}

func toECSDesiredStatus(status string) types.DesiredStatus {
	switch status {
	case "STOPPED":
		return types.DesiredStatusStopped
	case "PENDING":
		return types.DesiredStatusPending
	default:
		return types.DesiredStatusRunning
	}
}

func (c *ECSClient) GetServiceTargetGroups(ctx context.Context, clusterName string, ecsServiceName string) ([]domain.ServiceTargetGroup, error) {
	svc, err := c.describeECSService(ctx, clusterName, ecsServiceName)
	if err != nil {
		return nil, err
	}

	targetGroups := make([]domain.ServiceTargetGroup, 0)

	for _, lb := range svc.LoadBalancers {
		if lb.TargetGroupArn == nil || *lb.TargetGroupArn == "" {
			continue
		}

		targetGroups = append(targetGroups, domain.ServiceTargetGroup{
			TargetGroupArn: *lb.TargetGroupArn,
			ContainerName:  ptrString(lb.ContainerName),
			ContainerPort:  lb.ContainerPort,
		})
	}

	return targetGroups, nil
}

// 공통 함수
func (c *ECSClient) describeECSService(ctx context.Context, clusterName string, ecsServiceName string) (*types.Service, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("clusterName is required")
	}

	if ecsServiceName == "" {
		return nil, fmt.Errorf("ecsServiceName is required")
	}

	out, err := c.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  &clusterName,
		Services: []string{ecsServiceName},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ecs service %s: %w", ecsServiceName, err)
	}

	if len(out.Services) == 0 {
		return nil, fmt.Errorf("ecs service not found: %s", ecsServiceName)
	}

	return &out.Services[0], nil
}

func (c *ECSClient) GetServiceTargetGroupArn(ctx context.Context, clusterName string, ecsServiceName string) (string, error) {
	if clusterName == "" {
		return "", fmt.Errorf("clusterName is required")
	}

	if ecsServiceName == "" {
		return "", fmt.Errorf("ecsServiceName is required")
	}

	out, err := c.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterName),
		Services: []string{ecsServiceName},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe ecs service: %w", err)
	}

	if len(out.Services) == 0 {
		return "", fmt.Errorf("ecs service not found: %s", ecsServiceName)
	}

	service := out.Services[0]

	if len(service.LoadBalancers) == 0 {
		return "", fmt.Errorf("load balancer not configured for ecs service: %s", ecsServiceName)
	}

	targetGroupArn := service.LoadBalancers[0].TargetGroupArn
	if targetGroupArn == nil || *targetGroupArn == "" {
		return "", fmt.Errorf("targetGroupArn is empty for ecs service: %s", ecsServiceName)
	}

	return *targetGroupArn, nil
}

func (c *ECSClient) GetServiceControlState(ctx context.Context, clusterName string, ecsServiceName string) (domain.ECSServiceControlState, error) {

	out, err := c.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterName),
		Services: []string{ecsServiceName},
	})
	if err != nil {
		return domain.ECSServiceControlState{}, fmt.Errorf("describe ecs service failed: %w", err)
	}

	if len(out.Failures) > 0 {
		return domain.ECSServiceControlState{}, fmt.Errorf(
			"ecs service describe failure: arn=%s reason=%s",
			aws.ToString(out.Failures[0].Arn),
			aws.ToString(out.Failures[0].Reason),
		)
	}

	if len(out.Services) == 0 {
		return domain.ECSServiceControlState{}, fmt.Errorf("ecs service not found: %s", ecsServiceName)
	}

	return toECSServiceControlState(out.Services[0]), nil
}

func (c *ECSClient) UpdateServiceDesiredCount(
	ctx context.Context,
	clusterName string,
	ecsServiceName string,
	desiredCount int,
) (domain.ECSServiceControlState, error) {

	if desiredCount < 0 {
		return domain.ECSServiceControlState{},
			fmt.Errorf("[ecs] desired count must not be negative: %d", desiredCount)
	}

	out, err := c.client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String(clusterName),
		Service:      aws.String(ecsServiceName),
		DesiredCount: aws.Int32(int32(desiredCount)),
	})
	if err != nil {
		return domain.ECSServiceControlState{}, fmt.Errorf("update ecs service desiredCount failed: %w", err)
	}

	if out.Service == nil {
		return domain.ECSServiceControlState{}, fmt.Errorf("update ecs service returned empty service")
	}

	return toECSServiceControlState(*out.Service), nil
}

func toECSServiceControlState(s types.Service) domain.ECSServiceControlState {
	return domain.ECSServiceControlState{
		ECSServiceName: aws.ToString(s.ServiceName),
		DesiredCount:   s.DesiredCount,
		RunningCount:   s.RunningCount,
		PendingCount:   s.PendingCount,
		Status:         aws.ToString(s.Status),
	}
}

func (c *ECSClient) ForceNewDeployment(ctx context.Context, clusterName string, ecsServiceName string) (domain.ServiceRedeployResult, error) {
	out, err := c.client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:            aws.String(clusterName),
		Service:            aws.String(ecsServiceName),
		ForceNewDeployment: true,
	})
	if err != nil {
		return domain.ServiceRedeployResult{}, fmt.Errorf("failed to force new deployment: %w", err)
	}

	if out.Service == nil {
		return domain.ServiceRedeployResult{}, errors.New("update service returned nil service")
	}

	return mapServiceToRedeployResult(clusterName, *out.Service), nil
}

func mapServiceToRedeployResult(clusterName string, svc types.Service) domain.ServiceRedeployResult {
	deployments := make([]domain.ServiceDeployment, 0, len(svc.Deployments))

	for _, d := range svc.Deployments {
		deployments = append(deployments, domain.ServiceDeployment{
			ID:                 aws.ToString(d.Id),
			Status:             aws.ToString(d.Status),
			RolloutState:       string(d.RolloutState),
			RolloutStateReason: aws.ToString(d.RolloutStateReason),
			TaskDefinition:     aws.ToString(d.TaskDefinition),
			DesiredCount:       d.DesiredCount,
			RunningCount:       d.RunningCount,
			PendingCount:       d.PendingCount,
			CreatedAt:          d.CreatedAt,
			UpdatedAt:          d.UpdatedAt,
		})
	}

	return domain.ServiceRedeployResult{
		ECSServiceName: aws.ToString(svc.ServiceName),
		ClusterName:    clusterName,
		DesiredCount:   svc.DesiredCount,
		RunningCount:   svc.RunningCount,
		PendingCount:   svc.PendingCount,
		Deployments:    deployments,
	}
}

func (c *ECSClient) GetRunningTaskIDs(ctx context.Context, clusterName string, ecsServiceName string) ([]string, error) {
	return nil, nil
}

func (c *ECSClient) UpdateTaskProtection(ctx context.Context, clusterName string, protectedTaskIDs []string, flag bool) error {

	if len(protectedTaskIDs) == 0 {
		return fmt.Errorf(
			"failed to update ECS task protection: cluster=%s taskIDs=%v protectionEnabled=%t: %s",
			clusterName,
			protectedTaskIDs,
			flag,
			"protectedTaskIDs is 0",
		)
	}

	_, err := c.client.UpdateTaskProtection(
		ctx,
		&ecs.UpdateTaskProtectionInput{
			Cluster:           aws.String(clusterName),
			Tasks:             protectedTaskIDs,
			ProtectionEnabled: flag,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"failed to update ECS task protection: cluster=%s taskIDs=%v protectionEnabled=%t: %w",
			clusterName,
			protectedTaskIDs,
			flag,
			err,
		)
	}

	return nil
}

func (c *ECSClient) DescribeTask(
	ctx context.Context,
	clusterName string,
	taskID string,
) (domain.ECSTask, error) {

	output, err := c.client.DescribeTasks(
		ctx,
		&ecs.DescribeTasksInput{
			Cluster: aws.String(clusterName),
			Tasks: []string{
				taskID,
			},
		},
	)
	if err != nil {
		return domain.ECSTask{}, fmt.Errorf(
			"failed to describe ECS task: taskID=%s: %w",
			taskID,
			err,
		)
	}

	if len(output.Failures) > 0 {
		return domain.ECSTask{}, fmt.Errorf(
			"failed to describe ECS task: taskID=%s reason=%s",
			taskID,
			aws.ToString(output.Failures[0].Reason),
		)
	}

	if len(output.Tasks) == 0 {
		return domain.ECSTask{}, fmt.Errorf(
			"ECS task not found: taskID=%s",
			taskID,
		)
	}

	// 조회할 taskID에 해당하는 task를 매칭 하는 로직
	var task types.Task
	found := false

	for _, t := range output.Tasks {
		if extractTaskID(aws.ToString(t.TaskArn)) == taskID {
			task = t
			found = true
			break
		}
	}

	if !found {
		return domain.ECSTask{}, fmt.Errorf("task not found: taskID=%s", taskID)
	}

	result := domain.ECSTask{
		TaskARN:              aws.ToString(task.TaskArn),
		LastStatus:           aws.ToString(task.LastStatus),
		DesiredStatus:        aws.ToString(task.DesiredStatus),
		ContainerInstanceARN: aws.ToString(task.ContainerInstanceArn),
	}

	result.TaskID = extractTaskID(result.TaskARN)
	// bridge 모드라면 extractTaskPrivateIP(task)가 빈 문자열을 반환
	// result.PrivateIP = extractTaskPrivateIP(task)

	for _, container := range task.Containers {
		result.NetworkBindings = append(
			result.NetworkBindings,
			toNetworkBindings(container.NetworkBindings)...,
		)
	}

	return result, nil
}

func extractHostPort(
	task types.Task,
	containerPort int,
) (int, error) {

	for _, container := range task.Containers {
		for _, binding := range container.NetworkBindings {

			if int(aws.ToInt32(binding.ContainerPort)) == containerPort {
				return int(aws.ToInt32(binding.HostPort)), nil
			}
		}
	}

	return 0, fmt.Errorf(
		"host port not found: containerPort=%d",
		containerPort,
	)
}

func extractTaskID(taskARN string) string {
	index := strings.LastIndex(taskARN, "/")
	if index < 0 || index == len(taskARN)-1 {
		return taskARN
	}

	return taskARN[index+1:]
}

func extractTaskPrivateIP(task types.Task) string {
	for _, attachment := range task.Attachments {
		for _, detail := range attachment.Details {
			if aws.ToString(detail.Name) == "privateIPv4Address" {
				return aws.ToString(detail.Value)
			}
		}
	}

	return ""
}

func (c *ECSClient) GetContainerInstanceEC2ID(
	ctx context.Context,
	clusterName string,
	containerInstanceARN string,
) (string, error) {

	output, err := c.client.DescribeContainerInstances(
		ctx,
		&ecs.DescribeContainerInstancesInput{
			Cluster: aws.String(clusterName),
			ContainerInstances: []string{
				containerInstanceARN,
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to describe container instance: %w",
			err,
		)
	}

	if len(output.ContainerInstances) == 0 {
		return "", fmt.Errorf(
			"container instance not found: %s",
			containerInstanceARN,
		)
	}

	ec2InstanceID :=
		aws.ToString(output.ContainerInstances[0].Ec2InstanceId)

	if ec2InstanceID == "" {
		return "", fmt.Errorf(
			"EC2 instance ID is empty: %s",
			containerInstanceARN,
		)
	}

	return ec2InstanceID, nil
}
