package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"legacy-messenger-control-plane/configs"
	"legacy-messenger-control-plane/internal/adapters/ssh"
	"legacy-messenger-control-plane/internal/domain"
	"net"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type redisClient struct {
	client *goredis.Client
}

func NewRedisClient(ctx context.Context, redisCfg *configs.RedisConfig, sshClient *ssh.SSHClient) (*redisClient, error) {

	address := net.JoinHostPort(
		redisCfg.Host,
		strconv.Itoa(redisCfg.Port),
	)

	options := &goredis.Options{
		Addr:     address,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
	}

	// SSH Client가 존재하면 SSH 서버를 경유해 Redis에 연결한다.
	if sshClient != nil {
		fmt.Println("sshClient used")
		options.Dialer = sshClient.DialContext
		// golang.org/x/crypto/ssh의 tcpChan은
		// SetReadDeadline / SetWriteDeadline을 지원하지 않는다.
		options.ReadTimeout = -2
		options.WriteTimeout = -2
	}

	client := goredis.NewClient(options)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

		connectionType := "direct"
		if sshClient != nil {
			connectionType = "ssh tunnel"
		}

		return nil, fmt.Errorf(
			"failed to connect to Redis via %s: %w",
			connectionType,
			err,
		)
	}

	return &redisClient{
		client: client,
	}, nil
}

func (c *redisClient) SaveTaskSessionReport(ctx context.Context, report domain.TaskSessionReport) error {

	fmt.Printf("[SaveTaskSessionReport] serviceName : %s, taskID : %s, sessionCount : %d \n", report.ServiceName, report.TaskID, report.SessionCount)

	// HSET용 key 생성 - 수집 데이터 관리
	// control-plane:session:{ws}:reports task-001 '{"sessionCount":120,"reportedAt":"2026-07-14T22:10:00+09:00"}'
	// 조회 : HGETALL control-plane:session:{ws}:expireKey
	reportKey := fmt.Sprintf(
		"control-plane:session:{%s}:reports",
		report.ServiceName,
	)

	// ZADD용 key 생성 - task별 만료시간 관리
	// control-plane:session:{ws}:expires 1784034630 task-001 (같은 Member를 다시 ZADD하면 중복 저장되지 않고 Score가 갱신)
	// Member : task-001, Score : 1784034630
	// ZRANGEBYSCORE control-plane:session:{ws}:expires -inf 1784034630 하게되면 만료된 Member를 찾을 수 있음.
	// 조회 : ZRANGEBYSCORE control-plane:session:{ws}:expires -inf (1784036365 -> ZADD로 저장된 score < 1784036365, 앞에 '('를 제외하면 <=
	// 조회 : ZRANGEBYSCORE control-plane:session:{ws}:expires (1784036365 +inf -> ZASS로 저장된 score > 1784036365, 앞에 '('를 제외하면 >=
	expireKey := fmt.Sprintf(
		"control-plane:session:{%s}:expires",
		report.ServiceName,
	)

	value, err := json.Marshal(domain.TaskSessionValue{
		SessionCount: report.SessionCount,
		ReportedAt:   report.ReportedAt,
	})

	if err != nil {
		return fmt.Errorf("failed to marshal task session report: %w", err)
	}

	// TxPipelined는 여러 명령을 MULTI/EXEC으로 감싸 한 번에 실행합니다.
	// 일반 Pipelined는 네트워크 왕복 횟수를 줄이는 배치 처리이고, TxPipelined는 여기에 Redis 트랜잭션을 추가해 명령들이 중간에 다른 클라이언트 명령과 섞이지 않도록 실행합니다
	_, err = c.client.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		pipe.HSet(
			ctx,
			reportKey,
			report.TaskID,
			value,
		)

		pipe.ZAdd(ctx, expireKey, goredis.Z{
			Score:  float64(report.ExpiresAt.Unix()),
			Member: report.TaskID,
		})

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to save task session report: %w", err)
	}

	return nil
}

func (c *redisClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}

	return c.client.Close()
}

func (c *redisClient) GetTaskSessionReport(ctx context.Context, serviceName string) (map[string]domain.SessionReport, error) {

	reportKey := fmt.Sprintf(
		"control-plane:session:{%s}:reports",
		serviceName,
	)

	var getAllCmd *goredis.MapStringStringCmd

	_, err := c.client.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		getAllCmd = pipe.HGetAll(ctx, reportKey)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get task session reports: %w",
			err,
		)
	}

	result, err := getAllCmd.Result()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to read task session report result: %w",
			err,
		)
	}

	// log.Printf(
	// 	"[redis] task session reports: serviceName=%s key=%s\n",
	// 	serviceName,
	// 	reportKey,
	// )
	// report 출력
	// for _, v := range result {
	// 	log.Printf("[redis] report : %s\n", v)
	// }

	sessionReportMap := make(map[string]domain.SessionReport, 0)

	for taskID, value := range result {
		var report domain.SessionReport

		if err := json.Unmarshal([]byte(value), &report); err != nil {
			return nil, fmt.Errorf(
				"failed to unmarshal session report: taskID=%s: %w",
				taskID,
				err,
			)
		}
		sessionReportMap[taskID] = report
	}

	return sessionReportMap, nil
}

func (c *redisClient) GetInvalidReportTask(ctx context.Context, serviceName string, cfg *configs.AutoScaleConfig) (map[string]string, []string, error) {

	expiredKey := fmt.Sprintf(
		"control-plane:session:{%s}:expires",
		serviceName,
	)

	// 현재 시간보다 30초 이상 지난 score까지만 조회
	now := time.Now()
	expiredThreshold := now.Add(-time.Duration(cfg.ExpiresPeriod) * time.Second).Unix()

	results, err := c.client.ZRangeByScoreWithScores(
		ctx,
		expiredKey,
		&goredis.ZRangeBy{
			Min: "-inf",
			Max: strconv.FormatInt(expiredThreshold, 10),
		},
	).Result()
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to get expired report tasks: serviceName=%s: %w",
			serviceName,
			err,
		)
	}

	expiredTaskMap := make(map[string]string, len(results))

	for _, result := range results {
		taskID, ok := result.Member.(string)
		if !ok {
			return nil, nil, fmt.Errorf(
				"invalid expired task member type: serviceName=%s member=%v",
				serviceName,
				result.Member,
			)
		}

		expiredTaskMap[taskID] = strconv.FormatInt(
			int64(result.Score),
			10,
		)
	}
	if len(expiredTaskMap) > 0 {
		fmt.Printf("[expiredTaskMap] %v\n", expiredTaskMap)
	}

	// 중지된 task 구하기
	stopCandidate := now.Add(-time.Duration(cfg.StopCandidatePeriod) * time.Second).Unix()

	stopResults, err := c.client.ZRangeByScoreWithScores(
		ctx,
		expiredKey,
		&goredis.ZRangeBy{
			Min: "-inf",
			Max: strconv.FormatInt(stopCandidate, 10),
		},
	).Result()
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to get expired report tasks: serviceName=%s: %w",
			serviceName,
			err,
		)
	}

	stopTask := make([]string, len(stopResults))

	for _, result := range stopResults {
		taskID, ok := result.Member.(string)
		if !ok {
			return nil, nil, fmt.Errorf(
				"invalid stop task member type: serviceName=%s member=%v",
				serviceName,
				result.Member,
			)
		}
		stopTask = append(stopTask, taskID)
	}
	if len(stopTask) > 0 {
		fmt.Printf("[stopTask] %v\n", stopTask)
	}

	return expiredTaskMap, stopTask, nil
}

func (c *redisClient) ShouldStopTask(
	ctx context.Context,
	serviceName string,
	taskID string,
	now time.Time,
) (bool, error) {
	key := fmt.Sprintf(
		"control-plane:session:{%s}:expires",
		serviceName,
	)

	score, err := c.client.ZScore(ctx, key, taskID).Result()
	if errors.Is(err, goredis.Nil) {
		// 이미 삭제되었거나 아직 등록되지 않은 Task
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"failed to get task expiration score: %w",
			err,
		)
	}

	const stopGracePeriod = 30 * time.Second

	stopCutoff := now.
		Add(-stopGracePeriod).
		Unix()

	return int64(score) <= stopCutoff, nil
}

func (c *redisClient) DeleteTaskSessionState(
	ctx context.Context,
	serviceName string,
	taskID string,
) error {
	reportKey := fmt.Sprintf(
		"control-plane:session:{%s}:reports",
		serviceName,
	)

	expireKey := fmt.Sprintf(
		"control-plane:session:{%s}:expires",
		serviceName,
	)

	// expiredCountKey := fmt.Sprintf(
	// 	"control-plane:session:{%s}:expired-counts",
	// 	serviceName,
	// )

	_, err := c.client.TxPipelined(
		ctx,
		func(pipe goredis.Pipeliner) error {
			pipe.HDel(ctx, reportKey, taskID)
			pipe.ZRem(ctx, expireKey, taskID)
			//	pipe.HDel(ctx, expiredCountKey, taskID)

			return nil
		},
	)
	if err != nil {
		return fmt.Errorf(
			"failed to delete task session state: %w",
			err,
		)
	}

	return nil
}

func (c *redisClient) GetTaskSessionReportByTask(ctx context.Context, serviceName string, taskID string) (domain.SessionReport, error) {

	reportKey := fmt.Sprintf(
		"control-plane:session:{%s}:reports",
		serviceName,
	)

	var getAllCmd *goredis.MapStringStringCmd

	_, err := c.client.TxPipelined(ctx, func(pipe goredis.Pipeliner) error {
		getAllCmd = pipe.HGetAll(ctx, reportKey)
		return nil
	})
	if err != nil {
		return domain.SessionReport{}, fmt.Errorf(
			"failed to get task session reports: %w",
			err,
		)
	}

	result, err := getAllCmd.Result()
	if err != nil {
		return domain.SessionReport{}, fmt.Errorf(
			"failed to read task session report result: %w",
			err,
		)
	}

	for k, value := range result {
		var report domain.SessionReport

		if err := json.Unmarshal([]byte(value), &report); err != nil {
			return domain.SessionReport{}, fmt.Errorf(
				"failed to unmarshal session report: taskID=%s: %w",
				taskID,
				err,
			)
		}
		if k == taskID {
			return domain.SessionReport{
				SessionCount: report.SessionCount,
			}, nil
		}
	}
	return domain.SessionReport{}, nil
}
