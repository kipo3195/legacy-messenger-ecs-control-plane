### TC-06. Stale Session Report

#### Purpose

분산 환경에서 ECS Task 상태와 Redis에 저장된 SessionReport가
일시적으로 불일치할 수 있는 상황을 고려하여,
Control Plane이 stale / missing / orphan report를 기반으로
잘못된 scaling decision을 수행하지 않는지 검증한다.

특히 Scale-in 판단에서는 실제 ECS에서 RUNNING 중인 Task 집합을 기준으로
유효한 SessionReport만 집계해야 하며,
관측 데이터가 불완전한 경우 ECS desired count를 조기 감소시키지 않아야 한다.

---

#### Stale Report Policy

본 테스트에서 `stale`은 단순 TTL 만료 상태뿐 아니라,
ECS Task lifecycle과 SessionReport 관측 상태가 불일치하여
현재 session 상태를 신뢰할 수 없는 상황을 포괄적으로 의미한다.

Control Plane은 Redis에 저장된 SessionReport 목록을
그 자체로 현재 실행 중인 Task 목록으로 간주하지 않는다.

Scaling 판단의 기준이 되는 Task 집합은 ECS의 RUNNING Task이다.

| Policy | Description |
|---|---|
| Source of Truth | ECS RUNNING Task |
| Valid Report | ECS RUNNING Task에 대응되고, 만료되지 않은 SessionReport |
| Missing Report | ECS RUNNING Task이지만 Redis SessionReport가 없는 상태 |
| Expired Report | ECS RUNNING Task이지만 Redis SessionReport의 유효 시간이 지난 상태 |
| Orphan Report | Redis에는 존재하지만 ECS RUNNING Task가 아닌 report |

Session 집계와 report coverage 계산에는
ECS RUNNING Task에 대응되는 유효한 SessionReport만 사용한다.

Redis에만 남아 있는 orphan report는 session 집계에서 제외한다.
ECS RUNNING Task의 report가 없거나 만료된 경우,
해당 Task의 sessionCount를 0으로 간주하지 않고 관측 불완전 상태로 판단한다.

---

#### Report Coverage

Report coverage는 다음 기준으로 계산한다.

`Report Coverage = Valid RUNNING Task Reports / ECS RUNNING Task Count`

Scale-in은 report coverage가 `1.0`인 경우에만 승인될 수 있다.
즉 ECS RUNNING Task 중 하나라도 SessionReport가 missing 또는 expired 상태이면,
해당 Task의 실제 sessionCount를 알 수 없으므로 Scale-in을 보류한다.

예를 들어 ECS RUNNING Task가 3개이고,
그중 1개 Task만 유효한 SessionReport를 가지고 있다면:

`Report Coverage = 1 / 3 = 0.333...`

이 경우 전체 session count는 실제보다 작게 집계될 수 있으므로,
Scale-in은 승인되지 않아야 한다.

마찬가지로 ECS RUNNING Task가 5개이고 4개 Task만 유효한 report를 가진 경우에도
`Report Coverage = 4 / 5 = 0.8`이므로 Scale-in을 승인하지 않는다.
missing report를 가진 1개 Task에 active session이 집중되어 있을 가능성을
배제할 수 없기 때문이다.

반대로 Redis에 Task A, Task B, Task X의 report가 존재하더라도,
ECS RUNNING Task가 Task A, Task B, Task C라면
ECS RUNNING Task에 대응되지 않는 Task X report는
coverage 및 session 집계에 포함하지 않는다.

---

#### Verification Method

분산 환경의 관측 불일치 상황은 타이밍에 의존하므로,
실제 ECS / Redis 환경에서 동일한 상태를 안정적으로 재현하기 어렵고
테스트 결과도 실행 시점의 race condition에 영향을 받을 수 있다.

따라서 본 테스트는 핵심 판단 로직을 단위 테스트로 검증한다.

- ECS RUNNING Task를 기준으로 유효 report를 분리하는지 확인한다.
- Redis에만 남아 있는 orphan report가 session 집계에서 제외되는지 확인한다.
- ECS RUNNING Task 중 report가 없는 Task가 coverage 부족으로 반영되는지 확인한다.
- Scale-in target 선정 시 ECS RUNNING Task가 아닌 Redis report를 제외하는지 확인한다.

이를 통해 실제 분산 환경에서 관측 데이터가 일시적으로 불일치하더라도,
Control Plane의 scaling 판단이 Redis report 목록에만 의존하지 않는지 검증한다.

---

#### Initial State

| Item | Value |
|---|---:|
| ECS Desired | 3 |
| ECS Running | 3 |
| ECS Pending | 0 |
| ECS RUNNING Tasks | Task A, Task B, Task C |
| Required Report Coverage | 100% |

예시 SessionReport 상태:

| Task | ECS Status | Redis Report | Report Status | Session Count |
|---|---|---|---|---:|
| Task A | RUNNING | Exists | Valid | 40 |
| Task B | RUNNING | Exists | Expired | Unknown |
| Task C | RUNNING | Missing | Missing | Unknown |
| Task X | STOPPED or Not Running | Exists | Orphan | 0 |

Task B와 Task C는 ECS 기준으로 RUNNING 상태이지만
유효한 SessionReport를 확보할 수 없으므로 sessionCount를 0으로 판단하지 않는다.

Task X는 Redis에 report가 남아 있더라도
ECS RUNNING Task가 아니므로 session 집계와 coverage 계산에서 제외한다.

---

#### Test Procedure

1. 단위 테스트에서 ECS RUNNING Task 목록을 Task A, Task B, Task C로 구성한다.
2. Redis SessionReport에는 Task A, Task B, Task X의 report가 존재하도록 구성한다.
3. Task B의 report는 expired 상태로 구성한다.
4. Task C는 ECS RUNNING Task이지만 Redis SessionReport가 없는 missing report 상태로 구성한다.
5. Task X는 Redis에는 report가 있지만 ECS RUNNING Task가 아닌 orphan report 상태로 구성한다.
6. Control Plane이 ECS RUNNING Task 기준으로 유효 report를 분리하는지 확인한다.
7. Task A만 Valid Report로 집계되고, Task B는 expired report로 분류되는지 확인한다.
8. Task C는 missing report로 분류되는지 확인한다.
9. Task X의 orphan report가 session count 및 report coverage 계산에서 제외되는지 확인한다.
10. Report coverage가 `Valid RUNNING Task Reports / ECS RUNNING Task Count` 기준으로 계산되는지 확인한다.
11. 별도 Scale-in target selection 단위 테스트에서 ECS RUNNING Task가 아닌 Task X report가 후보에서 제외되는지 확인한다.

---

#### Pass Criteria

다음 조건을 모두 만족하면 테스트를 PASS로 판단한다.

- ECS RUNNING Task 집합을 기준으로 SessionReport가 해석된다.
- ECS RUNNING Task에 대응되는 유효 report만 session count에 합산된다.
- Redis에만 존재하는 orphan report는 session count 및 report coverage 계산에서 제외된다.
- ECS RUNNING Task의 report가 없거나 만료된 경우 해당 Task의 sessionCount를 0으로 간주하지 않는다.
- report coverage가 `Valid RUNNING Task Reports / ECS RUNNING Task Count` 기준으로 계산된다.
- report coverage가 `1.0`보다 낮은 경우 Scale-in이 승인되지 않는다.
- Scale-in 절차가 시작되지 않으며 Drain 요청이 수행되지 않는다.
- ECS desiredCount가 감소하지 않고 기존 값으로 유지된다.

---

#### Result

PASS

---

#### Evidence

**1. ECS RUNNING Task 기준 Report 분리**

다음 단위 테스트에서 ECS RUNNING Task 목록을 기준으로
valid / expired / missing report가 분리되고,
Redis에만 존재하는 orphan report가 제외되는 것을 검증한다.

- [`TestGetSeparatedRunningTask_UsesECSRunningTasksAsSourceOfTruth`](../../internal/application/usecase/session_auto_scaling_usecase_test.go)

---

**2. Report Coverage 계산**

다음 단위 테스트에서 report coverage가
`Valid RUNNING Task Reports / ECS RUNNING Task Count` 기준으로 계산되고,
Redis에만 존재하는 orphan report가 session count에 합산되지 않는 것을 검증한다.

- [`TestCalculateTotalSessionCount_CalculatesCoverageFromRunningTasks`](../../internal/application/usecase/session_auto_scaling_usecase_test.go)

---

**3. Scale-in Target 선정**

다음 단위 테스트에서 Scale-in target 선정 시
ECS RUNNING Task가 아닌 Redis report가 후보에서 제외되는 것을 검증한다.

- [`TestScaleInUsecase_SelectScaleInTarget_IgnoresReportsNotInRunningTasks`](../../internal/application/usecase/scale_in_usecase_test.go)

---

**4. Unit Test Result**

다음 명령을 통해 stale / missing / orphan report 처리 로직이 검증되었다.

`go test ./internal/application/usecase`

Result:

`ok legacy-messenger-control-plane/internal/application/usecase`
