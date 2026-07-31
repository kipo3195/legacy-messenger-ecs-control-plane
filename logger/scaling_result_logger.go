package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ScalingResultLogger struct {
	baseDir  string
	location *time.Location
	mu       sync.Mutex
}

func NewScalingResultLogger(
	baseDir string,
	location *time.Location,
) *ScalingResultLogger {
	if baseDir == "" {
		baseDir = "log"
	}

	if location == nil {
		location = time.Local
	}

	return &ScalingResultLogger{
		baseDir:  baseDir,
		location: location,
	}
}

type ScalingMonitoringResult struct {
	ServiceName             string
	TotalSessionCount       int
	RunningTaskCount        int
	CurrentDesiredCount     int
	RecommendedDesiredCount int
	Action                  any
	Reason                  string
	SessionReport           map[string]int
}

func (l *ScalingResultLogger) Write(
	result ScalingMonitoringResult,
) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().In(l.location)
	date := now.Format("2006-01-02")

	// log/2026-07-24
	dateDirectory := filepath.Join(l.baseDir, date)

	if err := os.MkdirAll(dateDirectory, 0755); err != nil {
		return fmt.Errorf(
			"failed to create scaling log directory: path=%s: %w",
			dateDirectory,
			err,
		)
	}

	// log/2026-07-24/2026-07-24_scaling_monitoring_result.txt
	filePath := filepath.Join(
		dateDirectory,
		fmt.Sprintf("%s_scaling_monitoring_result.txt", date),
	)

	file, err := os.OpenFile(
		filePath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to open scaling log file: path=%s: %w",
			filePath,
			err,
		)
	}
	defer file.Close()

	content := fmt.Sprintf(
		`################################## Scaling Monitoring Result ##################################
[Scaling Evaluation]
  RecordedAt               : %s
  ServiceName              : %s
  TotalSessionCount        : %d
  RunningTaskCount         : %d
  CurrentDesiredCount      : %d
  RecommendedDesiredCount  : %d
  Action                   : %v
  Reason                   : %s
`,
		now.Format("2006-01-02 15:04:05.000 MST"),
		result.ServiceName,
		result.TotalSessionCount,
		result.RunningTaskCount,
		result.CurrentDesiredCount,
		result.RecommendedDesiredCount,
		result.Action,
		result.Reason,
	)

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf(
			"failed to write scaling monitoring result: path=%s: %w",
			filePath,
			err,
		)
	}

	for k, v := range result.SessionReport {
		content := fmt.Sprintf(
			`
[Scaling Evaluation]
  TaskID               	 : %s
  SessionCount             : %d
`,
			k,
			v,
		)

		if _, err := file.WriteString(content); err != nil {
			return fmt.Errorf(
				"failed to write scaling monitoring result: path=%s: %w",
				filePath,
				err,
			)
		}
	}

	content = fmt.Sprintln(`###############################################################################################
	`)
	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf(
			"failed to write scaling monitoring result: path=%s: %w",
			filePath,
			err,
		)
	}

	return nil
}
