package scheduler

import (
	"context"
	"legacy-messenger-control-plane/internal/application/usecase"
	"legacy-messenger-control-plane/logger"
	"log"
	"time"
)

// handler와 동일하게 외부 -> 내부 usecase 진입점

type SessionScalingScheduler struct {
	usecase      usecase.SessionAutoScalingUsecase
	serviceName  string
	interval     time.Duration
	resultLogger *logger.ScalingResultLogger
}

func NewSessionScalingScheduler(
	usecase usecase.SessionAutoScalingUsecase,
	serviceName string,
	interval time.Duration,
	resultLogger *logger.ScalingResultLogger,
) *SessionScalingScheduler {
	return &SessionScalingScheduler{
		usecase:      usecase,
		serviceName:  serviceName,
		interval:     interval,
		resultLogger: resultLogger,
	}
}

func (s *SessionScalingScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 서비스 시작 직후 한 번 실행하고 싶을 때
	s.execute(ctx)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			s.execute(ctx)
		}
	}
}

func (s *SessionScalingScheduler) execute(ctx context.Context) {
	result, err := s.usecase.EvaluateAndScale(ctx, s.serviceName)
	if err != nil {
		log.Printf(
			"f: service=%s error=%v",
			s.serviceName,
			err,
		)
	}

	// fmt.Println("########################## Scaling Monitoring Result ##########################")
	// log.Println("")
	// fmt.Println(" ServiceName : ", result.ServiceName)
	// fmt.Println(" TotalSessionCount : ", result.TotalSessionCount)
	// fmt.Println(" RunningTaskCount : ", result.RunningTaskCount)
	// fmt.Println(" CurrentDesiredCount : ", result.CurrentDesiredCount)
	// fmt.Println(" RecommendedDesiredCount : ", result.RecommendedDesiredCount)
	// fmt.Println(" Action : ", result.Action)
	// fmt.Println(" Reason : ", result.Reason)
	// fmt.Println("##############################################################################")

	if s.resultLogger == nil {
		log.Println("scaling result logger is not configured")
		return
	}

	err = s.resultLogger.Write(
		logger.ScalingMonitoringResult{
			ServiceName:             result.ServiceName,
			TotalSessionCount:       result.TotalSessionCount,
			RunningTaskCount:        result.RunningTaskCount,
			CurrentDesiredCount:     result.CurrentDesiredCount,
			RecommendedDesiredCount: result.RecommendedDesiredCount,
			Action:                  result.Action,
			Reason:                  result.Reason,
		},
	)

	if err != nil {
		log.Printf(
			"failed to write scaling monitoring result: service=%s error=%v",
			s.serviceName,
			err,
		)
	}

}
