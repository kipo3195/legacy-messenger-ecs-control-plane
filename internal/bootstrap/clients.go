package bootstrap

import (
	"context"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/adapters/aws"
	"legacy-messenger-control-plane/internal/adapters/fake"
	"legacy-messenger-control-plane/internal/adapters/http/client"
	"legacy-messenger-control-plane/internal/adapters/redis"
	"legacy-messenger-control-plane/internal/adapters/sessionprovider"
	"legacy-messenger-control-plane/internal/domain"
	"legacy-messenger-control-plane/internal/ports"
	"net/http"
	"time"
)

type Clients struct {
	ECS         ports.ECSPort
	CloudWatch  ports.CloudWatchPort
	ELB         ports.ELBPort
	TaskSession ports.TaskSessionPort
	TaskDrain   ports.TaskDrainPort

	closeRedis func() error
}

func NewClients(ctx context.Context, cfg *configs.Config) (*Clients, error) {

	var ecsClient ports.ECSPort

	httpClient := &http.Client{
		Timeout: 3 * time.Second,
	}

	providerClient := sessionprovider.NewClient(
		cfg.SessionProvider.URL,
		httpClient,
	)

	initialStates := make(map[string]domain.ECSServiceControlState)
	initialStates["ws-service"] = domain.ECSServiceControlState{
		DesiredCount: 0,
		RunningCount: 0,
		PendingCount: 0,
	}

	if cfg.Mock {
		ecsClient = fake.NewECSClient(
			initialStates,
			providerClient,
		)
	} else {
		client, err := aws.NewECSClient(ctx, cfg)
		if err != nil {
			return nil, err
		}
		ecsClient = client
	}

	cloudWatchClient, err := aws.NewCloudWatchClient(ctx, cfg.AWS.Region)
	if err != nil {
		return nil, err
	}

	elbClient, err := aws.NewELBV2Client(ctx, cfg.AWS.Region)
	if err != nil {
		return nil, err
	}

	// 로컬 연결은 redis 연결을 위해 ssh 사용, AWS 환경에서는 동일 VPC의 private ip로 접근
	//sshClient, err := ssh.NewSSHClient(cfg.SSH)
	taskSessionClient, err := redis.NewRedisClient(ctx, cfg.Redis, nil)
	if err != nil {
		return nil, err
	}

	taskEndPointResolver := aws.NewECSTaskEndpointResolver(ecsClient, cfg.ECS, 33002)
	taskDrain := client.NewTaskDrainClient(taskEndPointResolver)

	return &Clients{
		ECS:         ecsClient,
		CloudWatch:  cloudWatchClient,
		ELB:         elbClient,
		TaskSession: taskSessionClient,
		closeRedis:  taskSessionClient.Close,
		TaskDrain:   taskDrain,
	}, nil
}

// 외부에서 서비스 종료시 호출
func (c *Clients) Close() error {
	if c.closeRedis != nil {
		return c.closeRedis()
	}

	return nil
}
