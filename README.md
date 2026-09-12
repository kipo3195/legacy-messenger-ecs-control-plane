# Legacy Messenger ECS Control Plane

Java 기반 레거시 WebSocket Messenger를 AWS ECS 환경에서 운영하기 위해 구현한 **Go 기반 Control Plane POC**입니다.

[Legacy Messenger ECS Ops POC](https://github.com/kipo3195/legacy-messenger-ecs-ops-poc)를 통해 Messenger를 컨테이너 이미지로 전환하고 ECS Service / Task 단위로 실행할 수 있게 되었지만, 운영 기능은 여전히 AWS Console, AWS CLI, CloudWatch, Shell Script에 분산되어 있었습니다. 이 프로젝트는 분산된 ECS 운영 기능을 하나의 REST API 계층으로 통합하고, 각 WebSocket Task가 직접 보고하는 **실제 사용자 Session 수**를 기준으로 Scale-out과 Drain 기반 Scale-in을 자동화했습니다.

특히 WebSocket 서비스의 장기 연결 특성을 고려하여 CPU / Memory 중심의 일반적인 Auto Scaling 대신, **실제 Session 부하와 ECS Task lifecycle을 함께 고려하는 Scaling 구조**를 설계했습니다. 실제 AWS 환경에서 정상 Scale-out / Scale-in 및 Drain 실패 보호 흐름을 검증했고, 경계값과 stale report 처리는 단위 테스트와 연계 증적으로 검증했습니다.

---

## Highlights

* **Task별 실제 WebSocket Session 기반 Scaling**
* **Redis 기반 Session Report lifecycle 관리**
* **Scale-out / Scale-in 비대칭 실행 구조**
* **Drain + ECS Task Protection 기반 안전한 Scale-in**
* **Consecutive Count / Cooldown / Pending Guard**
* **Stale / Missing / Orphan Session Report 방어**
* **Scale-in APPLIED timeout 및 Drain completion 검증**
* **AWS ECS Adapter / Stateful Fake ECS Adapter 분리**
* **AWS 통합 검증 + Unit Test 기반 TC-01 ~ TC-06 검증**

---

## Tech Stack

| Category | Technology |
| --- | --- |
| Language | Go |
| Cloud | AWS ECS EC2, Elastic Load Balancing |
| State Store | Redis |
| Container | Docker |
| Architecture | Port / Adapter, Application Usecase |
| Protocol | REST API |
| Test Environment | AWS ECS, Stateful Fake ECS, Unit Test |

---

# 1. Problem

기존 Java 기반 Messenger는 서버별 Java process를 직접 실행하고, Shell Script와 운영 명령으로 서비스를 기동 / 종료 / 재배포하는 방식으로 운영되었습니다.

선행 프로젝트인 [Legacy Messenger ECS Ops POC](https://github.com/kipo3195/legacy-messenger-ecs-ops-poc)에서는 Messenger를 컨테이너 이미지로 전환하고 AWS ECS EC2 환경에서 Service / Task 단위로 배포할 수 있는 기반을 마련했습니다. 하지만 실행 환경을 ECS로 옮긴 것만으로 운영 자동화가 완성되지는 않았습니다.

ECS 전환 이후에도 운영자는 다음 작업을 각각 다른 도구에서 수행해야 했습니다.

* ECS Service의 `desiredCount`, `runningCount`, `pendingCount` 확인
* 실행 중인 Task 목록과 Task별 상태 확인
* Load Balancer Target Group의 Health 상태 확인
* `desiredCount` 변경을 통한 서비스 기동 / 종료 / 수평 확장
* `forceNewDeployment`를 이용한 Task 재배포
* CloudWatch 지표를 이용한 연결 부하 확인

이 구조에서는 운영 기능이 AWS Console, AWS CLI, CloudWatch, Shell Script에 흩어지고, 관리자 화면이나 외부 자동화 시스템이 동일한 운영 기능을 일관되게 재사용하기 어렵습니다. 또한 서비스별 최소 / 최대 Task 수, 확장 가능 여부, Scale-in 안전 조건 같은 운영 정책이 명령 실행자나 개별 Script에 의존할 수 있습니다.

따라서 ECS, ELB, CloudWatch에 분산된 운영 정보를 서비스 단위로 조합하고, 관측과 제어 기능을 REST API로 표준화하는 Control Plane이 필요했습니다.

| 문제 | 기존 방식 | 해결 방향 |
| --- | --- | --- |
| ECS 운영 기능 분산 | AWS Console / CLI / Shell Script | Control Plane REST API |
| 서비스 상태 관측 분산 | ECS / ELB / CloudWatch 개별 조회 | 서비스 단위 상태 통합 |
| 서비스 제어 | 관리자가 직접 `desiredCount` 변경 | 정책 기반 Control Plane 제어 |
| WebSocket 부하 판단 | CPU / Memory / ALB Connection | Task별 실제 Session Report |
| Scale-out | 수동 Task 증가 | Session 기반 자동 Scale-out |
| Scale-in | `desiredCount` 즉시 감소 | Drain 기반 단계적 Scale-in |
| 관측 데이터 불일치 | Redis Report 기준 오판 가능 | ECS RUNNING Task를 Source of Truth로 사용 |
| 반복 검증 | 실제 AWS 상태 전이 필요 | Stateful Fake ECS와 단위 테스트 |

## 1.1 WebSocket 서비스의 Scaling 문제

일반적인 HTTP 요청은 짧은 시간 안에 처리된 뒤 연결이 종료되지만, WebSocket 연결은 사용자가 로그인한 동안 장시간 유지됩니다.

업무용 Messenger는 출근 시간대처럼 짧은 시간 안에 로그인과 신규 연결이 집중될 수 있습니다. 이때 CPU 사용률이 낮더라도 하나의 Task가 이미 많은 장기 연결을 유지하고 있을 수 있고, 동일한 CPU 사용률을 보이는 Task라도 실제 Session 수는 서로 다를 수 있습니다.

따라서 다음 지표만으로는 실제 WebSocket 부하를 정확하게 판단하기 어렵습니다.

```text
CPU Usage
Memory Usage
ALB ActiveConnectionCount
```

Scaling 판단에는 실제 서비스 상태를 나타내는 다음 정보가 필요했습니다.

```text
전체 사용자 Session 수
Task별 실제 Session 수
현재 RUNNING Task 수
Task별 Session 편중
PENDING Task 존재 여부
Session Report의 최신성
```

이를 위해 각 WebSocket Task가 자신이 관리 중인 **실제 사용자 Session 수를 Control Plane에 직접 보고하는 구조**를 선택했습니다.

---

# 2. Architecture

![Legacy Messenger Control Plane Architecture](./docs/images/control-plane-architecture.png)

전체 구조에서 실제 사용자 연결과 메시지를 처리하는 **Data Plane**과 ECS의 상태를 관측하고 Scaling을 수행하는 **Control Plane**을 분리했습니다.

```text
Messenger Clients
        │
        ▼
       ALB
        │
        ▼
WebSocket ECS Tasks
        │
        │ Session Report
        ▼
   Control Plane
        │
        ├── Redis Session State
        ├── Scaling Scheduler
        ├── Scaling Policy
        └── Scale-in Coordinator
        │
   ┌────┴──────────────┐
   ▼                   ▼
Scale-out          Scale-in
                       │
                 Select Target
                       │
                  Protection
                       │
                     Drain
                       │
                Session = 0
                       │
   └───────────┬───────┘
               ▼
             ECSPort
          ┌────┴────┐
          ▼         ▼
       AWS ECS   Fake ECS
```

## 2.1 Data Plane

WebSocket Task는 실제 사용자 트래픽을 처리합니다.

* WebSocket 연결 관리
* 사용자 Session 관리
* 메시지 처리
* Task별 Session 수 보고
* Drain 요청 처리

## 2.2 Control Plane

Control Plane은 사용자 트래픽 경로에 직접 참여하지 않고 운영 상태를 관측하고 제어합니다.

* ECS Service / Task 상태 관측
* Target Group Health 관측
* Task별 Session Report 수집
* Redis 기반 Session 상태 관리
* Scaling Policy 평가
* 자동 Scale-out
* Scale-in 대상 선정
* ECS Task Protection
* Drain orchestration
* ECS `desiredCount` 변경
* Task lifecycle 수렴 확인

이를 통해 **사용자 트래픽 처리와 운영 제어의 책임을 분리**했습니다.

---

# 3. Core Design Decisions

## 3.1 CloudWatch Connection 대신 실제 Session을 Scaling Metric으로 사용

초기 POC에서는 ALB의 `ActiveConnectionCount`를 이용해 Task당 평균 연결 부하를 계산했습니다.

```text
Average Connection per Task
=
ActiveConnectionCount / RunningTaskCount
```

애플리케이션을 수정하지 않고 빠르게 Scaling 판단 흐름을 검증할 수 있다는 장점이 있었지만 다음 문제가 있었습니다.

* Network Connection과 인증된 사용자 Session이 동일하지 않음
* Task별 Connection 분포를 확인할 수 없음
* 특정 Task의 Session 편중을 알 수 없음
* Scale-in 대상 Task를 선택할 근거가 부족함
* CloudWatch 집계 주기로 인해 최신 상태 반영에 한계가 있음

따라서 최종 Scaling Metric을 다음과 같이 변경했습니다.

```text
WebSocket Task
      │
      │ Session Report
      ▼
Control Plane
      │
      ▼
Redis
 ├── Task별 최신 Session Count
 └── Report Expiration
```

CloudWatch Connection Metric은 **운영 관측용 지표**로 유지하고, 자동 Scaling의 기준은 **Task가 직접 보고한 실제 Session 수**로 분리했습니다.

## 3.2 Scale-out과 Scale-in을 동일하게 처리하지 않음

Scale-out과 Scale-in은 기존 사용자에게 미치는 영향이 다릅니다.

Scale-out은 새로운 Task를 추가하는 작업이므로 기존 사용자 연결에 직접적인 영향을 주지 않습니다.

```text
Session 증가
    ↓
Scaling Policy
    ↓
desiredCount 증가
    ↓
PENDING
    ↓
RUNNING
```

따라서 정책 조건을 만족하면 바로 실행합니다.

반면 Scale-in은 실행 중인 WebSocket Task를 제거하므로 해당 Task가 관리하는 기존 Connection에 영향을 줄 수 있습니다. 그래서 단순히 `desiredCount`를 즉시 감소시키지 않습니다.

```text
Session 감소
    ↓
Scale-in 조건 충족
    ↓
Target Task 선정
    ↓
Survivor Task Protection
    ↓
Target Task Drain
    ↓
Session = 0 확인
    ↓
desiredCount 감소
    ↓
APPLIED
    ↓
Target Task STOPPED 확인
```

Scale-in은 별도의 **Scale-in Job / Coordinator**에서 단계적으로 처리합니다.

## 3.3 Scale-in은 Fail-safe 방향으로 처리

Scale-in 과정에서는 잘못된 축소보다 기존 Capacity를 유지하는 것을 우선합니다.

핵심 원칙은 다음과 같습니다.

> 관측 상태가 불완전하거나 Drain 완료를 신뢰할 수 없는 경우 Scale-in을 진행하지 않는다.

예를 들어 다음 상황에서는 `desiredCount`를 조기 감소시키지 않습니다.

```text
Active Session 존재
        ↓
Scale-in 보류

Drain API 실패
        ↓
Scale-in 중단

RUNNING Task의 Session Report 누락
        ↓
Scale-in 판단 보류

Session Report 상태 불일치
        ↓
Scale-in 판단 보류
```

즉 장애 상황에서도 **서비스 축소보다 현재 Capacity 유지가 우선**입니다.

## 3.4 ECS RUNNING Task를 Session 판단의 Source of Truth로 사용

Redis에 Session Report가 존재한다고 해서 해당 Task가 현재 ECS에서 실행 중이라고 보장할 수는 없습니다.

분산 환경에서는 다음과 같은 상태가 발생할 수 있습니다.

```text
ECS RUNNING + Report 없음
ECS RUNNING + Report 만료
ECS STOPPED + Redis Report 존재
```

따라서 Scaling 판단의 기준이 되는 Task 집합은 Redis가 아니라 **ECS RUNNING Task**로 정의했습니다.

| 상태 | 처리 |
| --- | --- |
| RUNNING + Valid Report | Scaling 집계에 사용 |
| RUNNING + Missing Report | 불완전한 관측 상태 |
| RUNNING + Expired Report | 불완전한 관측 상태 |
| Not RUNNING + Redis Report | Orphan Report로 제외 |

특히 Scale-in은 관측 데이터가 완전하지 않은 경우 실행하지 않습니다.

## 3.5 Application과 AWS Infrastructure를 Port로 분리

Application Usecase가 AWS SDK나 Redis 구현에 직접 의존하지 않도록 외부 기능을 Port Interface로 분리했습니다.

```text
HTTP Handler
      │
      ▼
Application Usecase
      │
      ▼
Port Interface
      │
      ├── AWS ECS Adapter
      ├── Fake ECS Adapter
      ├── Redis Adapter
      └── Network Adapter
```

이를 통해 Scaling Policy와 Scale-in orchestration을 변경하지 않고 실행 환경만 교체할 수 있도록 했습니다.

## 3.6 단순 Mock 대신 Stateful Fake ECS 구현

실제 AWS에서 Scaling 경계조건과 Task lifecycle을 반복 테스트하면 다음 문제가 있습니다.

* Task 시작 / 종료 대기 시간
* 반복 테스트에 따른 비용
* 원하는 `PENDING` 상태 재현 어려움
* 특정 Scale-in 대상 구성 어려움
* 비정상 상태 재현 어려움

이를 해결하기 위해 실제 `ECSPort`와 동일한 Interface를 구현하는 **Stateful Fake ECS Adapter**를 구성했습니다.

Fake ECS는 단순히 고정된 응답을 반환하지 않고 다음 상태를 내부적으로 관리합니다.

```text
DesiredCount
RunningCount
PendingCount
Task Status
Task Protection
```

그리고 다음 lifecycle을 재현합니다.

```text
PENDING
   ↓
RUNNING
   ↓
STOPPED
```

이를 통해 실제 AWS Adapter와 Application 로직의 경계를 유지하면서 Scaling 흐름을 반복적으로 검증했습니다.

---

# 4. Scaling Strategy

## 4.1 Required Task Count

서비스 전체 유효 Session 수와 Task당 목표 수용 Session 수를 이용하여 필요한 Task 수를 계산합니다.

```text
RequiredTaskCount
=
ceil(TotalSessionCount / SessionCapacityPerTask)
```

계산 결과에는 서비스별 최소 / 최대 Task 정책을 적용합니다.

```text
minDesiredCount
≤
requiredTaskCount
≤
maxDesiredCount
```

## 4.2 Scale-out Flow

```text
Session Reports
      │
      ▼
Valid Session Aggregation
      │
      ▼
Required Task Count
      │
      ▼
Scale-out 필요
      │
      ▼
Consecutive Condition
      │
      ▼
Cooldown Check
      │
      ▼
ECS Convergence / Pending Check
      │
      ▼
desiredCount + ScaleStep
```

`PENDING` Task가 존재하거나 ECS Service가 아직 수렴하지 않은 상태에서는 추가 Scale-out을 수행하지 않습니다.

이를 통해 Task 기동 중 동일한 부하를 반복 관측하여 불필요하게 여러 Task를 추가하는 상황을 방지합니다.

## 4.3 Scale-in Flow

```text
Session Reports
      │
      ▼
RUNNING Task 기준 Report 검증
      │
      ▼
Valid Session Aggregation
      │
      ▼
Required Task Count < DesiredCount
      │
      ▼
Consecutive Condition
      │
      ▼
Scale-in Target Selection
      │
      ▼
Survivor Task Protection
      │
      ▼
Target Task Drain
      │
      ▼
SessionCount = 0
      │
      ▼
Drain Completion Streak
      │
      ▼
desiredCount 감소
      │
      ▼
APPLIED
      │
      ▼
Target Task STOPPED 확인
```

Scale-in 대상은 유효한 Session Report를 가진 RUNNING Task 중 **Session 수가 가장 적은 Task**를 우선 선택합니다.

## 4.4 Scaling Guards

| Guard | 목적 |
| --- | --- |
| Min / Max Desired Count | 허용된 Task 범위를 벗어난 Scaling 방지 |
| Scale Step | 한 번에 증감할 Task 수 제한 |
| Consecutive Count | 순간적인 Session 변화에 의한 Scaling 방지 |
| Cooldown | 짧은 시간 내 반복 Scaling 방지 |
| ECS Convergence | 이전 Scaling이 완료되지 않은 상태에서 추가 Scaling 방지 |
| Pending Guard | 신규 Task 기동 중 중복 Scale-out 방지 |
| Report Expiration | 오래된 Session Report 사용 방지 |
| ECS RUNNING Validation | Orphan / Missing Report 기반 오판 방지 |
| Drain Completion Streak | 일시적인 `sessionCount=0` 오판 방지 |
| APPLIED Timeout | Task 종료 수렴을 무한 대기하는 상황 방지 |

---

# 5. Verification

| Test Case | Scenario | Verification | Result |
| --- | --- | --- | --- |
| [TC-01](./docs/test-cases/tc-01-normal-scale-out.md) | Normal Scale-out | AWS / Application Log / ECS Console | PASS |
| [TC-02](./docs/test-cases/tc-02-normal-scale-in.md) | Normal Scale-in | AWS / Application Log / ECS Console | PASS |
| [TC-03](./docs/test-cases/tc-03-active-session-protection.md) | Active Session Protection | AWS / Application Log / ECS Console | PASS |
| [TC-04](./docs/test-cases/tc-04-drain-failure.md) | Drain Failure | AWS / Application Log / ECS Console | PASS |
| [TC-05](./docs/test-cases/tc-05-scaling-boundary.md) | Scaling Boundary | Existing AWS evidence / Unit Test | PASS |
| [TC-06](./docs/test-cases/tc-06-stale-session-report.md) | Stale Session Report | Unit Test | PASS |

* TC-01 ~ TC-04: 실제 AWS 환경에서 정상 / 실패 흐름 검증
* TC-05: 기존 AWS evidence 기반 Boundary 검증
* TC-06: Unit Test로 stale / missing / orphan report 정책 검증

---

# 6. Troubleshooting & Lessons Learned

실제 AWS 환경에서 자동 Scaling을 검증하면서 Control Plane 로직만으로는 드러나지 않는 인프라 구성 문제를 확인했습니다.

특히 `desiredCount` 증가 이후 Task가 실제로 배치될 수 있는지, 그리고 Control Plane이 WebSocket Task의 내부 API를 호출할 수 있는지는 ECS / ASG / Security Group 구성과 직접적으로 연결되어 있었습니다.

## 6.1 ECS Task Placement Failure - Insufficient Memory

Scale-out 과정에서 ECS Service의 `desiredCount`를 증가시켰지만 신규 Task가 배치되지 않는 문제가 발생했습니다.

ECS Service Event에서 Container Instance의 메모리 부족으로 Task Placement가 실패한 것을 확인했습니다.

```text
insufficient memory available
```

원인은 ECS Service의 `desiredCount`는 증가했지만, 신규 Task를 배치할 수 있는 EC2 Container Instance capacity가 충분하지 않았기 때문입니다.

단일 c7i-flex.large Container Instance에 의존하던 구성을 `c7i-flex.large` → `t3.small x 3` ASG로 변경하여 Cluster 전체 가용 Memory를 늘리고 Task를 여러 Container Instance에 분산 배치할 수 있도록 했습니다. ECS가 여러 Container Instance에 Task를 분산 배치할 수 있게 되면서 Scale-out 시 신규 Task가 정상적으로 배치되고 RUNNING 상태로 전환되는 것을 확인했습니다.

이 이슈를 통해 **ECS Service의 Scale-out은 `desiredCount` 증가만으로 완료되지 않으며, 실제 Task를 배치할 EC2 Capacity도 함께 확보되어야 한다**는 점을 확인했습니다.

## 6.2 Control Plane to WebSocket Drain Timeout

ASG 기반으로 여러 EC2 Instance를 동적으로 구성한 환경에서, Control Plane과 WebSocket Service가 서로 다른 Instance에 배치되자 `/drain` 요청이 timeout으로 실패했습니다.

오류는 다음 형태로 확인되었습니다.

```text
Client.Timeout exceeded while awaiting headers
```

원인은 WebSocket Instance의 Security Group inbound rule이 `legacy-messenger-control-plane-sg`를 Source로 허용하고 있었지만, 실제 Control Plane이 실행된 EC2 ENI에는 해당 Security Group이 연결되어 있지 않았기 때문입니다.

```text
Control Plane ENI
  └── legacy-messenger-ecs-instance-sg only
          ↓
      Drain Request
          ↓
WebSocket ENI
  └── Source: legacy-messenger-control-plane-sg required
          ↓
        DROP
          ↓
      Timeout
```

Control Plane 역할의 Instance ENI에 `legacy-messenger-control-plane-sg`가 함께 부착되도록 Launch Template / ASG 설정을 수정하여 문제를 해결했습니다.

이 이슈를 통해 Drain 요청은 애플리케이션 API 호출이지만, 실제 성공 여부는 **요청을 보내는 EC2 ENI에 어떤 Security Group이 붙어 있는지**에 의해 결정된다는 점을 확인했습니다.

POC에서는 하나의 ASG 구성 안에서 Control Plane과 WebSocket Service를 함께 운영했지만, Production에서는 **Control Plane과 WebSocket Service의 EC2 Capacity를 별도 ASG로 분리하는 구조가 더 적절하다**고 판단했습니다.

이 두 문제는 모두 Application 로직 자체의 오류라기보다 ECS 운영 환경에서 발생한 배치 / 네트워크 경계 문제였습니다. 결과적으로 Control Plane 검증에서는 Scaling Policy뿐 아니라 **EC2 Capacity, Task Placement, Security Group, ENI 연결 상태**까지 함께 확인해야 한다는 점을 정리할 수 있었습니다.

---

# 7. Project Structure

```text
.
├── cmd/
│   └── server/                 # Application entrypoint
│
├── configs/                    # Service / policy configuration
│
├── internal/
│   ├── adapters/
│   │   ├── aws/                # AWS ECS / ELB integration
│   │   ├── fake/               # Stateful Fake ECS
│   │   ├── http/               # REST Handler / DTO
│   │   ├── redis/              # Session Report storage
│   │   ├── network/            # Drain request
│   │   ├── scheduler/          # Periodic scaling execution
│   │   └── sessionprovider/    # Local Session Report simulation
│   │
│   ├── application/            # Scaling / Scale-in use cases
│   ├── bootstrap/              # Dependency wiring
│   ├── domain/                 # Scaling policy / domain state
│   └── ports/                  # External dependency interfaces
│
└── docs/
    ├── api/                    # API specification
    ├── deployment/             # Run / deployment guide
    ├── evidence/               # Verification evidence
    ├── images/                 # Architecture images
    └── test-cases/             # TC-01 ~ TC-06
```

Application 계층은 AWS SDK 등의 Infrastructure 구현에 직접 의존하지 않고 Port Interface에 의존합니다.

이를 통해 실제 AWS 환경과 Local Fake 환경에서 동일한 Scaling Usecase를 실행할 수 있도록 구성했습니다.

---

# 8. API

Control Plane은 ECS Service의 관측·제어와 Session Report 수집을 위한 REST API를 제공합니다.

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/v1/services` | 관리 대상 서비스 조회 |
| `GET` | `/api/v1/services/{serviceName}/status` | ECS Service 상태 조회 |
| `GET` | `/api/v1/services/{serviceName}/tasks` | 실행 Task 조회 |
| `GET` | `/api/v1/services/{serviceName}/target-health` | Target Health 조회 |
| `GET` | `/api/v1/services/{serviceName}/connection-pressure` | 연결 수 기반 Task 부하 조회 |
| `POST` | `/api/v1/services/{serviceName}/scale` | 수동 `desiredCount` 변경 |
| `POST` | `/api/v1/services/{serviceName}/redeploy` | ECS Service 강제 재배포 |
| `POST` | `/api/v1/services/{serviceName}/scaling-evaluate` | CloudWatch 연결 부하 기반 수동 Scaling 판단 |
| `POST` | `/api/v1/services/{serviceName}/tasks/{taskID}/session-report` | Task Session Report |

상세 Request / Response 형식은 [`docs/api/README.md`](./docs/api/README.md)를 참고합니다.

---

# 9. Running & Deployment

Application은 실행 환경에 따라 동일한 Usecase에 서로 다른 ECS Adapter를 연결할 수 있도록 구성했습니다.

```text
Application
    │
    ▼
 ECSPort
  ├── AWS ECS Adapter
  └── Fake ECS Adapter
```

## 9.1 AWS Environment

실제 ECS Service / Task를 대상으로 다음 기능을 수행합니다.

* Service / Task 상태 조회
* Target Health 조회
* `desiredCount` 변경
* Task Protection
* Force New Deployment
* Session 기반 자동 Scaling

## 9.2 Local Integration Environment

Fake ECS와 Session Report Provider를 이용하여 실제 AWS 리소스를 생성하지 않고 Scaling lifecycle을 재현할 수 있습니다.

```text
Session Report Provider
        ↓
      Redis
        ↓
Control Plane
        ↓
    Fake ECS
```

실행 환경과 배포 구성에 대한 자세한 내용은 다음 문서를 참고합니다.

* [`docs/deployment/run-and-deployment.md`](./docs/deployment/run-and-deployment.md)

---

# 10. Limitations & Production Considerations

본 프로젝트의 목적은 실제 WebSocket Session을 기반으로 ECS Task lifecycle을 제어할 수 있는지 검증하는 **Control Plane POC**입니다.

POC를 통해 핵심 Scaling workflow와 Failure Boundary를 검증했지만, Production 적용 시에는 장애 복구, 중복 실행 방지, 상태 저장 내구성, 운영 관측에 대한 추가 설계가 필요합니다.

아래 항목은 POC 범위에서 확인했거나 구현 방향을 정리한 운영 리스크입니다.

| Scenario | Consideration |
| --- | --- |
| Session Report가 중단된 Task | Missing / stale report를 구분하고, report coverage가 부족하면 Scale-in을 보류 |
| Redis 장애 | Session 상태를 신뢰할 수 없으므로 Scale-in은 fail-safe 처리하고, retry / HA 구성 필요 |
| ECS API Timeout | Retry / backoff를 적용하고, `desiredCount` 변경 요청은 idempotency를 고려 |
| `PENDING` Task 장기 지속 | Scaling timeout을 두고, ECS가 수렴하기 전 추가 Scale-out을 제한 |
| Drain이 제한 시간 내 완료되지 않음 | Drain timeout 이후 Scale-in Job을 실패 처리하고 recovery 정책 적용 |
| Scheduler 중복 실행 | Distributed lock 또는 single leader 구조로 동일 서비스의 중복 Scaling 방지 |
| Scale-out 직후 다시 Scale-out 조건 발생 | Cooldown과 ECS convergence 확인으로 연속적인 capacity 증가 제한 |
| Scale-in 처리 중 다른 Scheduler 개입 | Job locking과 state transition guard로 Scale-in 단계 충돌 방지 |

POC에서 구현한 Scale-in 흐름은 관측 상태가 불확실한 경우 Capacity 감소를 보류하는 **Fail-safe 방향**을 기본 원칙으로 두었습니다.

현재 POC는 single Control Plane / single Scheduler를 전제로 합니다.

Production 확장 시 주요 과제:

* Persistent Scale-in Job
* Distributed Lock / Leader Election
* Retry / Backoff / Idempotency
* Metrics / Alerting

---

# 11. Documentation

| Document | Description |
| --- | --- |
| [`docs/api`](./docs/api) | REST API 상세 |
| [`docs/deployment`](./docs/deployment) | 실행 및 배포 |
| [`docs/test-cases`](./docs/test-cases) | TC-01 ~ TC-06 상세 |
| [`docs/evidence`](./docs/evidence) | AWS / Application 검증 증적 |

---

# 12. Project Status

**POC Completed**

```text
TC-01 Normal Scale-out              PASS
TC-02 Normal Scale-in               PASS
TC-03 Active Session Protection     PASS
TC-04 Drain Failure                 PASS
TC-05 Scaling Boundary              PASS
TC-06 Stale Session Report          PASS
```

Release:

```text
v1.0.0-poc
```

이 프로젝트는 레거시 Messenger를 ECS로 단순 이전하는 데서 끝나지 않고, **WebSocket 서비스의 실제 Session이라는 Domain Metric을 정의하고, 이를 기반으로 안전한 ECS Scaling lifecycle을 설계·구현·검증하는 것**을 목표로 진행했습니다.
