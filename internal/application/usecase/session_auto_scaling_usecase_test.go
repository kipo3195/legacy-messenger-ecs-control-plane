package usecase

import (
	"legacy-messenger-control-plane/internal/domain"
	"testing"
)

func TestGetSeparatedRunningTask_UsesECSRunningTasksAsSourceOfTruth(t *testing.T) {
	// Given - RUNNING Task 중 valid, expired, missing report가 각각 있고 task-x는 Redis에만 남아 있는 상황
	runningTaskIDs := []string{"task-a", "task-b", "task-c"}
	reports := map[string]domain.SessionReport{
		"task-a": {SessionCount: 10},
		"task-b": {SessionCount: 5},
		"task-x": {SessionCount: 1},
	}
	expiredReports := map[string]string{
		"task-b": "expired",
	}

	// When - ECS RUNNING Task 기준으로 report를 분리한다.
	expiredTask, normalTask, missingTask := getSeparatedRunningTask(
		runningTaskIDs,
		expiredReports,
		reports,
	)

	// Then - Redis에만 있는 task-x는 제외되고 RUNNING Task만 분류된다.
	assertStringSliceEqual(t, expiredTask, []string{"task-b"})
	assertStringSliceEqual(t, normalTask, []string{"task-a"})
	assertStringSliceEqual(t, missingTask, []string{"task-c"})
}

func TestCalculateTotalSessionCount_CalculatesCoverageFromRunningTasks(t *testing.T) {
	// Given - ECS RUNNING Task는 3개지만 유효 report는 1개만 존재한다.
	reports := map[string]domain.SessionReport{
		"task-a": {SessionCount: 10},
		"task-x": {SessionCount: 100},
	}
	normalTask := []string{"task-a"}
	runningTaskCount := 3

	// When - 유효 RUNNING Task 기준으로 session 합산과 coverage를 계산한다.
	result := calculateTotalSessionCount(
		reports,
		normalTask,
		runningTaskCount,
	)

	// Then - Redis에만 있는 task-x는 합산되지 않고 coverage는 1/3로 계산된다.
	if result.TotalSessionCount != 10 {
		t.Fatalf("expected total session count 10, got %d", result.TotalSessionCount)
	}

	expectedCoverage := float64(1) / float64(3)
	if result.ReportCoverage != expectedCoverage {
		t.Fatalf("expected report coverage %f, got %f", expectedCoverage, result.ReportCoverage)
	}
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}
