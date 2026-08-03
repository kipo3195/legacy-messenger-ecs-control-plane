# Legacy Messenger Control Plane

`legacy-messenger-control-plane`은 Java 기반 레거시 메신저를
AWS ECS 환경에서 운영하기 위한 Go 기반 Control Plane POC입니다.

AWS ECS·ELB·CloudWatch에 분산된 운영 기능을 REST API로 통합하고,
각 WebSocket Task의 실제 세션 수를 직접 보고받아 Redis에 집계하는 구조를 구현하고,
Scale-out과 Drain 기반 Scale-in을 자동으로 판단하고 실행하는 구조를 구현했습니다.

프로젝트는 다음 3단계로 검증합니다.

1. **AWS 운영 API 검증**
   실제 AWS ECS 환경에서 Service·Task 상태 조회, Target Health 조회,
   `desiredCount` 변경 및 강제 재배포 기능을 검증합니다.

2. **Fake ECS 기반 자동 스케일링 통합 검증**
   Fake ECS와 Session Report Provider를 활용하여 세션 보고, Redis 집계,
   Task의 `PENDING → RUNNING` 상태 전이, 자동 Scale-out 및
   Drain 기반 Scale-in의 전체 흐름을 검증합니다.

3. **실제 AWS 자동 스케일링 통합 검증**
   세션 기반 스케일링 판단 결과를 실제 ECS Service에 반영하고,
   신규 Task 투입과 Drain 대상 Task 종료까지의 전체 동작을 검증합니다.

현재 기준 1단계와 2단계 검증을 완료했으며,
마지막 단계로 실제 AWS 환경에서 Control Plane과 ECS Service 간
세션 기반 자동 Scale-out 및 Drain 기반 Scale-in을 검증할 예정입니다.

## 목차

1. [프로젝트 개요](#1-프로젝트-개요)
2. [프로젝트 배경 및 문제 정의](#2-프로젝트-배경-및-문제-정의)
3. [프로젝트 목표](#3-프로젝트-목표)
4. [주요 기능](#4-주요-기능)
5. [시스템 아키텍처](#5-시스템-아키텍처)
6. [핵심 설계와 기술적 의사결정](#6-핵심-설계와-기술적-의사결정)
7. [API 명세](#7-api-명세)
8. [실행 및 배포 방법](#8-실행-및-배포-방법)
9. [검증 시나리오 및 결과](#9-검증-시나리오-및-결과)
10. [현재 한계 및 실제 AWS 후속 검증](#10-현재-한계-및-실제-aws-후속-검증)

## 1. 프로젝트 개요

`legacy-messenger-control-plane`은 기존 Java 기반 레거시 메신저를
AWS ECS 환경에서 운영하기 위해 필요한 **관측·제어·세션 기반 자동 스케일링 기능을 Go 기반 REST API로 구현한 Control Plane POC**입니다.

선행 프로젝트인 [Legacy Messenger ECS Ops POC](https://github.com/kipo3195/legacy-messenger-ecs-ops-poc)에서는 서버별로 직접 실행하던 Java 메신저 서비스를 컨테이너 이미지로 전환하고, AWS ECS EC2 환경에서 ECS Service와 Task 단위로 배포·운영할 수 있는 기반을 마련했습니다.

그러나 ECS 전환 이후에도 서비스 상태 조회, 실행 중인 Task 확인, Target Health 점검, `desiredCount` 변경 및 재배포 등의 운영 작업은 AWS Console, AWS CLI, CloudWatch와 개별 Shell Script에 분산되어 있었습니다.

또한 업무용 메신저의 WebSocket 연결은 사용자가 로그인한 동안 장시간 유지되며, 출근 시간대와 같은 특정 시간에 로그인 요청과 신규 연결이 집중될 수 있습니다. 이러한 서비스는 CPU와 메모리 사용률만으로 실제 세션 부하를 판단하기 어렵고, Task별로 관리 중인 세션 수가 서로 다를 수 있으므로 실제 사용자 세션을 기준으로 한 스케일링 판단 구조가 필요합니다.

본 프로젝트는 이러한 문제를 해결하기 위해 다음 기능을 하나의 Control Plane으로 통합했습니다.

| 영역    | 역할                                                        |
| ----- | --------------------------------------------------------- |
| 관측    | ECS Service·Task 상태, Deployment, Target Health 및 연결 부하 조회 |
| 제어    | ECS Service의 `desiredCount` 변경 및 강제 재배포                   |
| 수집    | 각 WebSocket Task가 보고하는 실제 세션 수 수신                         |
| 저장·집계 | Redis에 Task별 최신 세션 수와 보고 시각을 저장하고 유효한 세션만 집계              |
| 판단    | 전체 세션 수와 운영 정책을 기반으로 Scale-out, Scale-in 또는 유지 여부 계산      |
| 실행    | Scale-out 즉시 실행 및 Drain 기반 Scale-in 처리                    |

프로젝트 검증은 다음 3단계로 구성했습니다.

### 1단계. 실제 AWS 운영 API 검증 — 완료

실제 AWS ECS, Elastic Load Balancing API를 연동하여 다음 기능을 검증했습니다.

* ECS Service와 Task 상태 조회
* `desiredCount`, `runningCount`, `pendingCount` 확인
* Target Group과 Target Health 조회
* 운영자 요청에 따른 `desiredCount` 변경
* `forceNewDeployment`를 이용한 Task 재배포

이 단계에서는 Control Plane API의 응답과 AWS Console에서 확인한 실제 리소스 상태를 비교하여 AWS Adapter의 관측 및 제어 기능을 검증했습니다.

### 2단계. Fake ECS 기반 자동 스케일링 통합 검증 — 완료

실제 AWS 환경에서 반복적으로 Task 상태 전이와 스케일링 경계조건을 테스트할 경우 비용과 테스트 시간이 증가하므로, 실제 `ECSPort`와 동일한 인터페이스를 구현하는 Fake ECS와 별도의 Session Provider를 구성했습니다.

이를 통해 다음 흐름을 로컬 환경에서 검증했습니다.

* Session Report Provider의 Task별 세션 수 주기 보고
* Redis를 이용한 최신 세션 상태 저장 및 만료 관리
* 서비스 전체 유효 세션 수 집계
* 최소·최대 Task 수, Cooldown, 연속 조건을 반영한 Scaling Policy 평가
* Scale-out 시 `desiredCount` 증가와 신규 Task의 `PENDING → RUNNING` 전이
* 신규 Running Task의 Session Report Provider 등록 및 세션 보고 시작
* Scale-in 대상 Task 선정과 중복 선택 방지를 위한 보호 처리
* Drain 요청 이후 Session Provider의 세션 수 감소를 이용한 Drain 완료 조건 검증
* Drain 완료 후 `desiredCount` 감소 및 Task 종료

Fake ECS는 정해진 응답만 반환하는 단순 Mock이 아니라,
`desiredCount`, `runningCount`, `pendingCount`와 개별 Task 상태를 보유하고 실제 ECS와 유사한 상태 전이를 재현하는 Fake ECS로 구현했습니다.

### 3단계. 실제 AWS 자동 스케일링 통합 검증 — 예정

마지막 단계에서는 2단계에서 검증한 세션 기반 자동 스케일링 흐름을 실제 AWS ECS 환경에 연결합니다.

주요 검증 대상은 다음과 같습니다.

* 실제 세션 집계 결과에 따른 ECS Service 자동 Scale-out
* 신규 Task의 `PENDING → RUNNING` 전환
* 신규 Task의 Load Balancer Target 등록 및 Health 확인
* 실제 WebSocket Task에 대한 Drain 요청
* Drain 완료 후 ECS Service의 `desiredCount` 감소
* Control Plane이 선택한 Drain 대상과 실제 종료 Task의 관계
* AWS API 오류와 상태 전이 지연 상황에서의 처리

현재 1단계와 2단계 검증을 완료했으며,
마지막 단계로 실제 AWS 환경에서 Control Plane과 ECS Service 간
세션 기반 Scale-out 및 Drain 기반 Scale-in의 전체 통합 동작을 검증할 예정입니다.


## 2. 프로젝트 배경 및 문제 정의

### 2.1 레거시 메신저의 ECS 전환

기존 레거시 메신저는 각 서버에서 Java 프로세스를 직접 실행하고,
Shell Script와 운영 명령을 이용하여 서비스를 기동·종료·재배포하는 방식으로 운영되었습니다.

선행 프로젝트에서는 Java 서비스를 컨테이너 이미지로 전환하고 AWS ECS EC2 환경에 배포했습니다. 이를 통해 메신저 서비스를 ECS Service와 Task 단위로 실행하고, `desiredCount` 변경과 `forceNewDeployment`를 이용하여 서비스의 실행 수와 배포 상태를 제어할 수 있는 기반을 마련했습니다.

그러나 실행 환경을 ECS로 전환한 것만으로 운영 자동화가 완성된 것은 아니었습니다. 기존 서버 및 프로세스 중심 운영 방식이 AWS Console, AWS CLI, CloudWatch 및 개별 Shell Script를 사용하는 형태로 바뀌었을 뿐, 서비스 관측과 제어 기능은 여전히 여러 도구에 분산되어 있었습니다.

### 2.2 분산된 ECS 운영 인터페이스

ECS 전환 이후 서비스 운영을 위해 다음 작업이 필요했습니다.

* ECS Service의 `desiredCount`, `runningCount`, `pendingCount` 확인
* 실행 중인 Task 목록과 Task별 상태 확인
* Load Balancer Target Group의 Health 상태 확인
* `desiredCount` 변경을 통한 서비스 기동·종료 및 수평 확장
* `forceNewDeployment`를 이용한 Task 재배포
* CloudWatch 지표를 이용한 서비스 부하 확인

각 작업은 AWS Console, AWS CLI 또는 개별 Shell Script를 통해 수행할 수 있었지만, 이를 하나의 일관된 인터페이스로 제공하는 계층은 존재하지 않았습니다.

이러한 구조에서는 운영자가 여러 AWS 리소스의 상태를 각각 확인해야 하며, 관리자 화면이나 외부 자동화 시스템에서 동일한 운영 기능을 재사용하기도 어렵습니다. 또한 서비스별 최소·최대 Task 수와 확장 가능 여부 같은 운영 정책이 명령 실행자나 개별 Script에 의존할 수 있습니다.

따라서 ECS, Elastic Load Balancing 및 CloudWatch에 분산된 운영 정보를 서비스 단위로 조합하고, 관측과 제어 기능을 REST API로 표준화할 필요가 있었습니다.

### 2.3 WebSocket 서비스의 부하 특성

일반적인 HTTP 요청은 대부분 짧은 시간 안에 처리된 후 연결이 종료되지만, WebSocket 연결은 사용자가 로그인한 동안 장시간 유지됩니다.

WebSocket Task는 연결이 유지되는 동안 사용자 세션과 연결 상태를 계속 관리해야 합니다. 특히 업무용 메신저는 출근 시간대에 로그인과 WebSocket 연결이 짧은 시간 안에 집중될 수 있으므로, 신규 연결을 처리할 수 있는 여유 용량을 빠르게 확보해야 합니다.

그러나 CPU와 메모리 사용률만으로는 각 Task가 실제로 몇 개의 사용자 세션을 관리하고 있는지 알기 어렵습니다. CPU 사용률이 낮더라도 이미 많은 장기 연결을 유지하고 있을 수 있으며, 동일한 CPU 사용률을 보이는 Task라도 실제 세션 수는 서로 다를 수 있습니다.

따라서 WebSocket 서비스의 확장 필요 여부를 판단하려면 다음 정보를 함께 고려해야 합니다.

* 서비스 전체 세션 수
* 실행 중인 WebSocket Task 수
* Task별 실제 로그인 세션 수
* Task별 세션 편중 여부
* 서비스별 목표 세션 수
* 서비스별 최소·최대 Task 수
* 현재 `PENDING` 상태의 Task 존재 여부

### 2.4 CloudWatch 연결 지표 기반 초기 판단

초기 POC에서는 ALB의 `ActiveConnectionCount`와 ECS Service의 실행 Task 수를 이용하여 Task당 평균 연결 부하를 계산했습니다.

이 방식은 WebSocket 애플리케이션을 수정하지 않고도 기존 AWS 환경에서 연결 수 기반 스케일링 판단 흐름을 빠르게 구현할 수 있다는 장점이 있었습니다.

```text
Task당 평균 연결 수
= ActiveConnectionCount / Running Task Count
```

이를 통해 전체 연결 수가 현재 실행 중인 Task 수에 비해 높은지 낮은지를 계산하고, `SCALE_OUT`, `SCALE_IN`, `MAINTAIN` 판단과 권장 `desiredCount`를 반환하는 구조를 구현했습니다.

다만 `ActiveConnectionCount`는 Load Balancer가 관측한 네트워크 연결 수이며, WebSocket 애플리케이션이 관리하는 인증 완료 사용자 세션 수와 동일하지 않습니다.

또한 다음과 같은 한계가 있습니다.

* CloudWatch 집계 주기로 인해 즉각적인 상태 반영이 어려움
* 서비스 전체 연결 수만 확인할 수 있고 Task별 연결 수는 확인할 수 없음
* 특정 Task에 연결이 편중되었는지 판단할 수 없음
* 네트워크 연결과 실제 로그인 세션을 구분할 수 없음
* 보고가 중단되거나 종료된 Task의 상태를 애플리케이션 기준으로 판별할 수 없음

따라서 CloudWatch 연결 지표는 초기 스케일링 판단 구조를 검증하는 데에는 적합했지만, 실제 WebSocket 세션 부하를 기반으로 자동 스케일링을 실행하기 위한 최종 지표로는 한계가 있었습니다.

### 2.5 Task별 실제 세션 보고 구조의 필요성

CloudWatch 지표의 한계를 보완하기 위해 각 WebSocket Task가 자신이 관리 중인 실제 로그인 세션 수를 Control Plane에 직접 보고하도록 구조를 확장했습니다.

각 Task는 자신의 식별자와 현재 세션 수를 주기적으로 보고하고, Control Plane은 Redis에 Task별 최신 상태를 저장합니다.

```text
WebSocket Task
      │
      │ Session Report
      ▼
Control Plane
      │
      ▼
Redis
 ├── Task별 최신 세션 수
 └── 보고 만료 시각
```

Control Plane은 보고 시각이 유효한 Task만 집계하여 서비스 전체 세션 수를 계산하고, 보고가 일정 시간 동안 갱신되지 않은 Task는 스케일링 계산에서 제외합니다.

이를 통해 다음 문제를 해결할 수 있습니다.

* 네트워크 연결 수와 실제 로그인 세션 수의 차이
* Task별 세션 분포 확인 불가
* 종료된 Task의 오래된 세션 수가 계속 합산되는 문제
* 특정 Task에 세션이 편중된 상황을 고려하지 못하는 문제
* Scale-in 대상 Task를 선정할 수 없는 문제

### 2.6 자동 Scale-out과 Scale-in의 실행 방식 차이

세션 기반 스케일링 판단 결과를 실제 실행으로 연결할 때 Scale-out과 Scale-in은 동일한 방식으로 처리할 수 없습니다.

Scale-out은 신규 Task를 추가하는 작업이므로 기존 사용자 연결에 직접적인 영향을 주지 않습니다.

```text
세션 증가
→ 필요 Task 수 증가
→ desiredCount 증가
→ 신규 Task PENDING
→ RUNNING 전환
→ 세션 보고 시작
```

반면 Scale-in은 실행 중인 WebSocket Task를 제거하므로 해당 Task가 관리하는 기존 연결에 영향을 줄 수 있습니다.

따라서 단순히 `desiredCount`를 즉시 감소시키는 대신 다음 흐름이 필요합니다.

```text
세션 감소
→ Scale-in 대상 Task 선정
→ 대상 Task 보호
→ Drain 요청
→ 기존 세션 종료 확인
→ desiredCount 감소
→ Task 종료
```

이에 따라 Scale-out은 정책 조건을 통과하면 즉시 실행하고, Scale-in은 별도의 Job과 Coordinator를 통해 Drain 완료 후 실행하도록 분리했습니다.

### 2.7 실제 AWS 반복 검증의 한계

Task 상태 전이와 스케일링 정책을 실제 AWS 환경에서 반복 검증할 경우 다음과 같은 제약이 있습니다.

* ECS Task 기동 및 종료마다 대기 시간이 발생함
* 반복적인 Task 실행으로 AWS 비용이 발생함
* Cooldown, 연속 조건, Pending 중복 차단 같은 경계조건을 재현하기 어려움
* 특정 Task를 Drain 대상으로 선택하는 상황을 반복 구성하기 어려움
* 오류 조건과 비정상 상태를 의도적으로 만들기 어려움

따라서 실제 `ECSPort`와 동일한 인터페이스를 구현하는 Fake ECS를 구성하고, 별도의 Session Provider를 통해 WebSocket Task의 세션 보고 동작을 재현했습니다.

Fake ECS는 다음 상태를 내부적으로 관리합니다.

* Service의 `desiredCount`
* 실행 중인 `runningCount`
* 시작 중인 `pendingCount`
* 개별 Task의 `PENDING`, `RUNNING`, `STOPPED` 상태
* Task 기동 지연
* Scale-in 대상 Task 보호 상태

이를 통해 실제 AWS Adapter와 Application Usecase의 경계를 유지하면서, 세션 보고부터 자동 Scale-out 및 Drain 기반 Scale-in까지의 전체 흐름을 로컬에서 반복 검증할 수 있도록 했습니다.

### 2.8 해결해야 할 문제

본 프로젝트에서는 다음 문제를 해결 대상으로 정의했습니다.

| 구분        | 기존 상태                              | 해결 방향                                      | 현재 상태      |
| --------- | ---------------------------------- | ------------------------------------------ | ---------- |
| 운영 인터페이스  | AWS Console, CLI, Shell Script로 분산 | ECS 운영 기능을 REST API로 통합                    | 완료         |
| 서비스 관측    | ECS, ELB, CloudWatch 상태를 각각 조회     | 서비스 단위 상태 응답으로 조합                          | 완료         |
| 서비스 제어    | 운영자가 AWS 명령을 직접 실행                 | 정책 검증 후 Task 수 변경 및 재배포                    | 완료         |
| 부하 지표     | CPU·메모리 및 ALB 연결 수 중심              | Task별 실제 세션 수 직접 보고                        | Mock 검증 완료 |
| 상태 저장     | Task별 최신 세션 상태 저장소 없음              | Redis 기반 최신 세션 및 만료 관리                     | Mock 검증 완료 |
| Scale-out | 운영자가 수동으로 Task 수 증가                | 세션 집계 결과에 따라 자동 실행                         | Mock 검증 완료 |
| Scale-in  | Task 수를 즉시 감소                      | 대상 선정, 보호, Drain 후 감소                      | Mock 검증 완료 |
| 상태 전이 검증  | 실제 AWS에서만 확인 가능                    | Fake ECS로 `PENDING → RUNNING → STOPPED` 재현 | Mock 검증 완료         |
| 실제 통합     | 수동 AWS 제어까지만 검증                    | 실제 AWS에서 자동 Scale-in/out 검증                | 예정         |


## 3. 프로젝트 목표

본 프로젝트의 목표는 레거시 메신저의 ECS 운영 기능을 Control Plane으로 통합하고, 각 WebSocket Task가 보고하는 실제 세션 수를 기반으로 서비스 확장과 축소를 자동으로 판단·실행할 수 있는 구조를 구현하는 것입니다.

주요 목표는 다음과 같습니다.

* ECS Service, Task, Target Health 및 운영 상태를 하나의 REST API 계층에서 조회
* 서비스별 최소·최대 Task 수와 확장 가능 여부를 기준으로 안전하게 `desiredCount` 변경
* WebSocket Task별 세션 수를 Redis에 저장·집계하여 실제 세션 부하 계산
* 세션 수와 ECS Service 상태를 기반으로 Scale-out, Scale-in 또는 유지 여부 판단
* Cooldown, 연속 조건 및 Pending 상태를 반영하여 중복 스케일링 방지
* Scale-out은 자동 실행하고, Scale-in은 대상 Task의 Drain 완료 후 실행

현재 실제 AWS ECS API를 이용한 운영 기능 검증과 Fake ECS·Session Provider 기반 자동 스케일링 검증을 완료했습니다.

다음 단계에서는 실제 AWS ECS 환경에서 세션 기반 자동 Scale-out과 Drain 기반 Scale-in의 통합 동작을 검증할 예정입니다.


## 4. 주요 기능

### 4.1 ECS 운영 기능 통합

AWS ECS·ELB·CloudWatch에 분산된 운영 기능을 하나의 REST API 계층으로 통합했습니다.

* ECS Service와 Task 상태 조회
* `desiredCount`, `runningCount`, `pendingCount` 확인
* Target Group과 Target Health 조회
* `desiredCount` 변경 및 `forceNewDeployment` 실행
* 서비스별 최소·최대 Task 수 검증

### 4.2 Task별 실제 세션 수집 및 Redis 집계

각 WebSocket Task가 자신이 관리하는 실제 세션 수를 주기적으로 보고하고, Control Plane은 Redis에 Task별 최신 상태를 저장합니다.

* Task별 세션 수와 보고 시각 저장
* 보고 만료 시각 관리
* 보고가 중단된 Task를 전체 집계에서 제외
* 서비스 전체 유효 세션 수와 Task별 세션 분포 계산

CloudWatch의 `ActiveConnectionCount`는 운영 관측에 활용하고, 자동 스케일링 판단에는 Task가 직접 보고한 실제 세션 수를 사용합니다.

### 4.3 세션 기반 자동 Scale-out

서비스 전체 세션 수와 현재 ECS 상태를 기반으로 필요한 Task 수를 계산하고, 정책 조건을 만족하면 `desiredCount`를 자동으로 증가시킵니다.

* 최소·최대 Task 수와 Scale Step 적용
* 연속 조건과 Cooldown 적용
* `pendingCount`가 존재할 때 중복 Scale-out 차단
* 신규 Task의 `PENDING → RUNNING` 상태 전이 확인
* 신규 Running Task의 세션 보고 흐름 연결

### 4.4 Drain 기반 Scale-in

Scale-in은 기존 WebSocket 연결에 영향을 줄 수 있으므로, Task 수를 즉시 감소시키지 않고 대상 Task의 세션을 정리한 후 실행합니다.

* 세션 수가 가장 적은 Task를 Scale-in 대상으로 선정
* 처리 중인 Task의 중복 선택을 방지하기 위한 보호
* 대상 Task에 Drain 요청
* 세션 감소 및 Drain 완료 확인
* 완료 후 `desiredCount` 감소

Scale-out은 즉시 실행하고, Scale-in은 별도의 Job과 Coordinator를 통해 단계적으로 처리하도록 분리했습니다.

### 4.5 실제 AWS와 Fake ECS를 이용한 단계별 검증

Application Usecase가 AWS SDK에 직접 의존하지 않도록 `ECSPort`를 정의하고, 실제 AWS Adapter와 Fake ECS Adapter를 동일한 인터페이스로 구성했습니다.

* 실제 AWS에서 ECS 상태 조회, 수동 Scale 및 재배포 검증
* Fake ECS에서 `PENDING`, `RUNNING`, `STOPPED` 상태 전이 재현
* Session Provider를 이용한 Task별 세션 보고 재현
* 세션 보고부터 자동 Scale-out과 Drain 기반 Scale-in까지 로컬 통합 검증

현재 실제 AWS 운영 API 검증과 Fake ECS 기반 자동 스케일링 검증을 완료했으며, 실제 AWS 환경의 자동 Scale-in/out 통합 검증을 남겨두고 있습니다.


## 5. 시스템 아키텍처

`legacy-messenger-control-plane`은 실제 사용자 메시지와 WebSocket 연결을 처리하는 Data Plane과 분리되어, ECS Service의 상태를 관측하고 세션 부하를 기반으로 실행 상태를 제어하는 Control Plane으로 동작합니다.

WebSocket Task는 자신이 관리 중인 실제 세션 수를 Control Plane에 주기적으로 보고합니다. Control Plane은 Redis에 저장된 Task별 최신 세션 상태를 집계하고, Scaling Policy를 적용하여 자동 Scale-out 또는 Drain 기반 Scale-in을 실행합니다.

![Legacy Messenger Control Plane Architecture](./docs/images/control-plane-architecture.png)

### 5.1 전체 구성

| 구성요소                 | 역할                                       |
| -------------------- | ---------------------------------------- |
| WebSocket Task       | 사용자 연결 처리 및 Task별 세션 수 보고                |
| Redis                | Task별 최신 세션 수와 보고 만료 상태 저장               |
| Scaling Scheduler    | 세션 집계 및 스케일링 평가 실행                       |
| Scaling Policy       | 최소·최대 Task 수, Cooldown 및 연속 조건 적용        |
| Scale-in Coordinator | 대상 Task 선정, 보호, Drain 및 Scale-in 처리      |
| Amazon ECS           | Service·Task 조회, `desiredCount` 변경 및 재배포 |
| ELB·CloudWatch       | Target Health와 연결 부하 관측                  |
| Service Registry     | AWS 리소스 매핑과 서비스별 운영 정책 관리                |

전체 동작 흐름은 다음과 같습니다.

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
        │
        ├── Scaling Scheduler
        │
        └── Scaling Policy
        │
   ┌────┴──────────┐
   ▼               ▼
Scale-out     Scale-in Coordinator
   │               │
   │          Select → Protect
   │               │
   │             Drain
   │               │
   └───────┬───────┘
           ▼
         ECSPort
           │
           ▼
      Amazon ECS
```

### 5.2 Data Plane과 Control Plane 분리

사용자의 WebSocket 연결과 메시지 요청은 Data Plane에 해당하는 WebSocket Task가 처리하며, Control Plane은 사용자 트래픽 경로에 직접 참여하지 않습니다.

Control Plane은 다음 운영 책임을 담당합니다.

* ECS Service와 Task 상태 관측
* Task별 세션 상태 수집 및 집계
* 스케일링 정책 평가
* 자동 Scale-out
* Drain 기반 Scale-in
* 운영자 요청에 따른 수동 제어 및 재배포

이를 통해 사용자 트래픽 처리와 운영 제어의 책임을 분리했습니다.

### 5.3 세션 기반 자동 스케일링

각 WebSocket Task가 보고한 세션 수는 Redis에 최신 상태로 저장되며, 보고가 유효한 Task의 세션 수만 서비스 전체 부하에 포함됩니다.

```text
Task별 Session Report
        │
        ▼
Redis 최신 상태 저장
        │
        ▼
유효 세션 집계
        │
        ▼
Scaling Policy
        │
   ┌────┴─────┐
   ▼          ▼
Scale-out   Scale-in Job
```

Scale-out은 정책 조건을 만족하면 `desiredCount`를 증가시키고, Scale-in은 세션이 가장 적은 Task를 선정하여 Drain 완료 후 `desiredCount`를 감소시킵니다.

### 5.4 AWS Adapter와 Fake ECS Adapter

Application Usecase가 AWS SDK에 직접 의존하지 않도록 ECS 연동 기능을 `ECSPort`로 추상화했습니다.

```text
Application Usecase
        │
        ▼
      ECSPort
      ├── AWS ECS Adapter
      └── Fake ECS Adapter
```

AWS ECS Adapter는 실제 AWS 리소스를 조회·제어하고, Fake ECS Adapter는 Service와 Task 상태를 내부적으로 관리하며 `PENDING → RUNNING → STOPPED` 상태 전이를 재현합니다.

이를 통해 핵심 스케일링 로직을 변경하지 않고 실제 AWS 환경과 로컬 통합 검증 환경을 교체할 수 있도록 구성했습니다.


## 6. 핵심 설계와 기술적 의사결정

### 6.1 독립 Control Plane 애플리케이션 구성

기존 레거시 메신저 운영 환경에서는 Switch Service가 Shell Script를 실행하여 서버의 서비스를 기동하거나 종료했습니다.

ECS 전환 이후에는 단순 기동·종료뿐 아니라 다음 기능이 함께 필요해졌습니다.

* ECS Service와 Task 상태 조회
* Target Health 확인
* `desiredCount` 변경 및 재배포
* 서비스별 운영 정책 적용
* 세션 부하 기반 스케일링 판단
* 주기적인 세션 집계와 자동 실행

기존 Switch Service나 개별 Shell Script를 확장하는 방식도 가능했지만, 이 경우 AWS 리소스 조회, 오류 처리 및 운영 정책이 여러 Script에 분산될 수 있습니다.

또한 본 프로젝트는 단발성 명령 실행뿐 아니라 Task별 세션 보고를 수집하고 짧은 주기로 스케일링을 평가해야 하므로, 지속적으로 실행되는 독립 Go 애플리케이션 형태의 Control Plane으로 구성했습니다.

### 6.2 Usecase와 외부 인프라의 분리

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
      └── Redis Adapter
```

각 계층의 책임은 다음과 같습니다.

| 계층                  | 책임                              |
| ------------------- | ------------------------------- |
| HTTP Handler        | 요청 검증 및 HTTP 응답 처리              |
| Application Usecase | 세션 집계, 스케일링 판단 및 실행 흐름 처리       |
| Port Interface      | Application 계층이 필요로 하는 외부 기능 정의 |
| Adapter             | AWS SDK, Redis 및 외부 API 연동      |

이를 통해 핵심 스케일링 로직을 변경하지 않고 실제 AWS ECS와 Fake ECS를 교체할 수 있도록 구성했습니다.

Fake ECS는 고정된 응답을 반환하는 단순 Stub이 아니라, Service와 Task 상태를 보유하고 `PENDING → RUNNING → STOPPED` 전이를 재현하는 상태 기반 Adapter로 구현했습니다.

### 6.3 CloudWatch 연결 지표에서 실제 세션 보고로 전환

초기 POC에서는 WebSocket 애플리케이션을 수정하지 않고 스케일링 판단 흐름을 검증하기 위해 ALB의 `ActiveConnectionCount`를 사용했습니다.

```text
Task당 평균 연결 수
= ActiveConnectionCount / Running Task Count
```

이 방식은 전체 연결 부하를 빠르게 확인할 수 있지만 다음 한계가 있습니다.

* 네트워크 연결 수와 인증된 로그인 세션 수가 다를 수 있음
* Task별 세션 분포를 확인할 수 없음
* 특정 Task의 세션 편중을 판단할 수 없음
* Scale-in 대상 Task를 선정하기 어려움

이 한계를 보완하기 위해 각 WebSocket Task가 실제 세션 수를 Control Plane에 직접 보고하도록 확장했습니다.

Control Plane은 Redis에 Task별 최신 세션 수와 보고 시각을 저장하고, 유효한 보고만 집계하여 자동 스케일링에 사용합니다.

CloudWatch 연결 지표는 운영 관측 목적으로 유지하고, 자동 스케일링의 기준은 Task가 직접 보고한 실제 세션 수로 분리했습니다.

### 6.4 Scale-out과 Scale-in 실행 방식 분리

Scale-out과 Scale-in은 기존 사용자 연결에 미치는 영향이 다르므로 동일한 방식으로 처리하지 않았습니다.

Scale-out은 신규 Task를 추가하는 작업이므로 정책 조건을 만족하면 `desiredCount`를 즉시 증가시킵니다.

```text
세션 증가
→ Scaling Policy 평가
→ desiredCount 증가
→ 신규 Task PENDING
→ RUNNING 전환
```

반면 Scale-in은 실행 중인 WebSocket 연결에 영향을 줄 수 있으므로 즉시 `desiredCount`를 감소시키지 않습니다.

```text
세션 감소
→ Scale-in Job 생성
→ 대상 Task 선정 및 보호
→ Drain 요청
→ 세션 종료 확인
→ desiredCount 감소
```

Scale-in은 별도의 Coordinator에서 처리하며, 세션 수가 가장 적은 Task를 대상으로 선정하고 Drain 완료 후 Task 수를 감소시킵니다.

또한 일시적인 세션 변화와 중복 실행을 방지하기 위해 다음 정책을 적용했습니다.

* Scale-out 및 Scale-in 연속 조건
* Cooldown
* 한 번에 증감 가능한 Scale Step
* `pendingCount`가 존재할 때 추가 Scale-out 차단
* Scale-in 처리 중인 Task 보호

## 7. API 명세

Control Plane은 ECS 서비스의 관측, 제어 및 스케일링 판단을 위한 REST API를 제공합니다.

### 7.1 API 목록

| 구분 | Method | Endpoint                                             | 설명                             |
| -- | ------ | ---------------------------------------------------- | ------------------------------ |
| 관측 | GET    | `/api/v1/services`                                   | 관리 대상 서비스 목록 조회                |
| 관측 | GET    | `/api/v1/services/{serviceName}/status`              | 특정 ECS Service 상태 조회           |
| 관측 | GET    | `/api/v1/services/{serviceName}/tasks`               | 실행 중인 Task 목록 및 상태 조회          |
| 관측 | GET    | `/api/v1/services/{serviceName}/target-health`       | Target Group Health 상태 조회      |
| 관측 | GET    | `/api/v1/services/{serviceName}/connection-pressure` | 연결 수 기반 Task 부하 조회             |
| 제어 | POST   | `/api/v1/services/{serviceName}/scale`               | ECS Service의 desiredCount 수동 변경|
| 제어 | POST   | `/api/v1/services/{serviceName}/redeploy`            | ECS Service 강제 재배포             |
| 판단 | POST   | `/api/v1/services/{serviceName}/scaling-evaluate`    | CloudWatch 연결 부하 기반 수동 스케일링 판단        |
| 수집 | POST   | `/api/v1/services/{serviceName}/tasks/{taskID}/session-report`    | ECS Task의 session count 수집         |


상세 요청 및 응답 형식은 [API 상세 문서](./docs/api/README.md)를 참고합니다.

## 8. 실행 및 배포 방법

`legacy-messenger-control-plane`은 Go 애플리케이션으로 구현했으며, 로컬 또는 Linux 환경에서 실행할 수 있습니다.

프로젝트는 실행 환경에 따라 동일한 Application Usecase에 다음 ECS Adapter를 연결하도록 구성했습니다.

* 실제 AWS ECS 연동을 위한 AWS ECS Adapter
* 로컬 통합 검증을 위한 Fake ECS Adapter

현재 실제 AWS ECS API를 이용한 서비스 관측·수동 제어 검증과, Fake ECS·Redis·Session Provider를 이용한 세션 기반 자동 스케일링 검증을 완료했습니다.

실제 AWS 환경의 자동 Scale-out 및 Drain 기반 Scale-in 통합 검증을 완료한 뒤, 최종 실행 구성과 환경변수, 실행 순서 및 배포 방법을 이 섹션에 정리할 예정입니다.

기존 AWS 운영 API 검증 당시의 Linux 실행 및 배포 방법은 [`docs/deployment/run-and-deployment.md`](docs/deployment/run-and-deployment.md)를 참고합니다.


## 9. 검증 시나리오 및 결과

본 프로젝트의 최종 검증 목적은 각 WebSocket Task가 보고하는 실제 세션 수를 기준으로 필요한 ECS Task 수를 계산하고, Scale-out과 Scale-in을 실제 운영 흐름에 맞게 안전하게 실행할 수 있는지 확인하는 것입니다.

특히 다음 항목을 중심으로 검증합니다.

* Task별 세션 보고가 Redis에 정확하게 저장·집계되는지
* 전체 세션 수와 운영 정책을 기준으로 적정 `desiredCount`를 계산하는지
* Scale-out 시 신규 Task가 정상적으로 기동되고 세션 보고를 시작하는지
* `PENDING` Task가 존재할 때 중복 Scale-out을 방지하는지
* Scale-in 시 세션이 적은 Task를 선정하고 Drain 완료 후 Task 수를 감소시키는지
* 실제 AWS ECS 환경에서도 Mock 환경과 동일한 스케일링 흐름이 동작하는지

검증은 실제 AWS 환경과 로컬 통합 검증 환경을 나누어 3단계로 진행합니다.

### 9.1 검증 단계 요약

| 단계  | 검증 환경                           | 주요 검증 내용                              | 상태 |
| --- | ------------------------------- | ------------------------------------- | -- |
| 1단계 | 실제 AWS ECS·ELB                  | 서비스 관측, 수동 `desiredCount` 변경, 재배포     | 완료 |
| 2단계 | Fake ECS·Redis·Session Provider | 세션 기반 자동 Scale-out과 Drain 기반 Scale-in | 완료 |
| 3단계 | 실제 AWS ECS·Redis·WS Task        | 실제 AWS 자동 Scale-in/out 통합 동작          | 예정 |

### 9.2 1단계: 실제 AWS 운영 API 검증

Control Plane을 Linux 서버에서 실행한 뒤 실제 AWS ECS와 Elastic Load Balancing API를 연동하여 관측 및 수동 제어 기능을 검증했습니다.

#### 검증 환경

| 구분            | 구성                        |
| ------------- | ------------------------- |
| Control Plane | Go 기반 REST API            |
| 실행 환경         | Linux 서버에서 바이너리 직접 실행     |
| 관리 대상         | AWS ECS EC2 기반 레거시 메신저    |
| AWS Region    | `ap-northeast-2`          |
| 검증한 연동        | ECS API, ELB API          |
| 확인 방법         | API 응답과 AWS Console 상태 비교 |

#### 검증 시나리오

| 검증 시나리오           | 검증 내용                                             | 결과 |
| ----------------- | ------------------------------------------------- | -- |
| 관리 대상 서비스 조회      | Service Registry 등록 정보 확인                         | 성공 |
| ECS Service 상태 조회 | `desiredCount`, `runningCount`, `pendingCount` 확인 | 성공 |
| 실행 Task 조회        | Task 상태와 배치 정보 확인                                 | 성공 |
| Target Health 조회  | Load Balancer Target 상태 확인                        | 성공 |
| 수동 Scale-out      | `desiredCount` 증가와 신규 Task 실행 확인                  | 성공 |
| 수동 Scale-in       | `desiredCount` 감소와 Task 종료 확인                     | 성공 |
| 강제 재배포            | `forceNewDeployment`를 통한 Task 교체 확인               | 성공 |

API에서 반환한 Service, Task 및 Target Health 정보가 AWS Console의 실제 상태와 일치했습니다.

또한 운영자가 명시적으로 요청한 `desiredCount` 변경과 재배포가 실제 ECS Service와 Target Group에 반영되는 것을 확인했습니다.

> 이 단계에서는 AWS Adapter의 관측·제어 기능을 검증했으며, 세션 집계 결과에 따른 자동 스케일링은 포함하지 않았습니다.

### 9.3 2단계: Fake ECS 기반 자동 스케일링 검증

실제 AWS 환경에서 반복적으로 Task 상태 전이와 스케일링 경계조건을 재현하기 위해 Fake ECS와 Session Provider를 구성했습니다.

#### 검증 환경

| 구성요소                 | 역할                                        |
| -------------------- | ----------------------------------------- |
| Control Plane        | 세션 집계, Scaling Policy 평가 및 실행             |
| Redis                | Task별 최신 세션 수와 보고 만료 상태 저장                |
| Session Provider     | Task별 세션 수를 주기적으로 보고                      |
| Fake ECS             | Desired, Running, Pending 및 Task 상태 전이 재현 |
| Scaling Scheduler    | 짧은 주기로 자동 스케일링 평가                         |
| Scale-in Coordinator | 대상 선정, 보호, Drain 및 Task 감소 처리             |

#### 주요 검증 시나리오

| 검증 시나리오           | 검증 내용                           | 결과 |
| ----------------- | ------------------------------- | -- |
| 세션 리포트 수집         | Task별 세션 수와 보고 시각 저장            | 성공 |
| 보고 만료 처리          | 만료 Task를 전체 세션 합산에서 제외          | 성공 |
| 자동 Scale-out      | 세션 증가에 따라 `desiredCount` 증가     | 성공 |
| Task 상태 전이        | 신규 Task의 `PENDING → RUNNING` 전환 | 성공 |
| 중복 Scale-out 차단   | Pending 상태에서 추가 확장 방지           | 성공 |
| Scale-in 후보 선정    | 세션 수가 적은 Task 선택                | 성공 |
| Task 보호           | 처리 중인 Task의 중복 선택 방지            | 성공 |
| Drain 기반 Scale-in | Session Provider가 보고하는 세션 수가 0이 된 후 desiredCount 감소  | 성공 |

2단계 검증을 통해 세션 보고부터 Redis 집계, Scaling Policy 평가, Scale-out 실행 및 Drain 기반 Scale-in까지 전체 흐름이 로컬 환경에서 동작함을 확인했습니다.

Fake ECS는 고정 응답을 반환하는 단순 Mock이 아니라, 실제 ECS와 유사한 Task 생명주기와 Service 상태를 보유하는 상태 기반 Adapter로 사용했습니다.

### 9.4 3단계: 실제 AWS 자동 스케일링 통합 검증

마지막 단계에서는 2단계에서 검증한 세션 기반 자동 스케일링 흐름을 실제 AWS ECS 환경에 연결합니다.

주요 검증 항목은 다음과 같습니다.

#### 자동 Scale-out

```text
세션 증가
→ Control Plane 집계
→ SCALE_OUT 판단
→ 실제 ECS desiredCount 증가
→ 신규 Task PENDING
→ RUNNING
→ Target Group Healthy
→ 신규 Task 세션 보고 시작
```

확인 항목:

* Scaling Policy의 판단값과 실제 `desiredCount` 변경값이 일치하는지
* 신규 Task가 `PENDING → RUNNING`으로 전환되는지
* 신규 Task가 Target Group Health Check를 통과하는지
* 신규 Task가 정상적으로 세션 보고를 시작하는지
* Task 기동 중 중복 Scale-out이 발생하지 않는지

#### Drain 기반 Scale-in

```text
세션 감소
→ SCALE_IN 판단
→ 대상 Task 선정 및 보호
→ 실제 WS Task Drain
→ 세션 0 확인
→ desiredCount 감소
→ Task 종료 확인
```

확인 항목:

* 세션이 가장 적은 Task가 Drain 대상으로 선정되는지
* Drain 대상 Task에 신규 연결이 배정되지 않는지
* 기존 세션 종료 후에만 Task 수가 감소하는지
* Control Plane이 선정한 Task와 실제 ECS 종료 Task가 일치하는지
* Drain 또는 AWS API 실패 시 작업이 안전하게 중단되는지

3단계 검증 결과는 실제 AWS 통합 테스트 완료 후 추가할 예정입니다.

### 9.5 현재 검증 상태

현재까지 다음 범위를 완료했습니다.

* 실제 AWS ECS와 ELB의 관측 및 수동 제어 기능 검증
* Task별 세션 보고와 Redis 기반 최신 상태 관리
* 세션 기반 Scaling Policy
* 자동 Scale-out
* Fake ECS의 `PENDING → RUNNING → STOPPED` 상태 전이
* 대상 Task 선정과 Drain 기반 Scale-in 흐름

남은 검증은 실제 AWS ECS 환경에서 세션 기반 자동 Scale-out과 Drain 기반 Scale-in이 Mock 환경과 동일하게 동작하는지 확인하는 것입니다.


## 10. 현재 한계 및 실제 AWS 후속 검증

현재까지 실제 AWS ECS·ELB API를 이용한 서비스 관측과 수동 제어 기능을 검증했으며, Fake ECS·Redis·Session Provider를 이용하여 세션 기반 자동 Scale-out과 Drain 기반 Scale-in의 전체 흐름을 검증했습니다.

다만 2단계 검증은 상태 전이와 스케일링 정책을 반복적으로 확인하기 위한 로컬 통합 환경을 기준으로 수행했습니다. 따라서 실제 AWS ECS 환경에서 발생하는 Task 기동 지연, Load Balancer 등록, Task 종료 선택 및 AWS API 오류까지 포함한 최종 통합 동작은 추가 검증이 필요합니다.

### 10.1 실제 AWS 자동 Scale-out 검증

세션 집계 결과에 따라 Control Plane이 실제 ECS Service의 `desiredCount`를 증가시키고, 신규 Task가 트래픽 처리와 세션 보고에 참여하는 전체 흐름을 검증합니다.

주요 확인 항목은 다음과 같습니다.

* 세션 증가에 따른 Scale-out 판단과 실제 `desiredCount` 변경
* 신규 Task의 `PENDING → RUNNING` 상태 전이
* Target Group 등록과 Health Check 통과
* 신규 Task의 세션 보고 시작
* Task 기동 중 중복 Scale-out 방지
* Scale-out 판단부터 신규 Task 투입까지의 소요 시간

### 10.2 실제 AWS Drain 기반 Scale-in 검증

Scale-in은 기존 WebSocket 연결에 영향을 줄 수 있으므로, 대상 Task의 Drain 완료와 실제 ECS Task 종료 흐름을 함께 검증합니다.

주요 확인 항목은 다음과 같습니다.

* 세션 수가 가장 적은 Task의 Scale-in 후보 선정
* Drain 대상 Task의 신규 연결 차단
* 기존 세션 감소 및 Drain 완료 확인
* Drain 완료 후 `desiredCount` 감소
* Control Plane이 선정한 Task와 실제 ECS가 종료한 Task의 일치 여부
* Drain 실패 또는 시간 초과 시 Scale-in 중단 처리

특히 ECS Service의 `desiredCount`만 감소시킬 경우 실제 종료 Task의 선택은 ECS Scheduler가 담당하므로, Control Plane이 Drain한 Task와 실제 종료 Task가 동일하게 유지되는지 확인해야 합니다.

### 10.3 실제 부하 환경에서의 정책 조정

현재 Scaling Policy는 Fake ECS와 Session Provider를 이용한 반복 검증을 통해 동작 흐름을 확인한 상태입니다.

실제 운영 수준의 동시 접속 환경에서는 다음 정책값을 추가로 조정해야 합니다.

* Task당 목표 세션 수
* Scale-out 및 Scale-in 임계치
* 연속 조건 횟수
* Cooldown 시간
* 한 번에 증감할 수 있는 Scale Step
* 세션 리포트 만료 기준
* Drain 완료 판단 기준

실제 Task 기동 시간과 세션 증가 속도를 측정하여 신규 Task가 준비되기 전에 기존 Task의 수용 한계를 초과하지 않도록 여유 용량을 반영할 필요가 있습니다.

### 10.4 장애 및 복구 시나리오

현재 POC는 정상적인 스케일링 흐름을 중심으로 검증했습니다. 실제 운영 적용을 위해서는 다음 장애 상황의 처리도 추가로 검증해야 합니다.

* Redis 장애 또는 세션 리포트 조회 실패
* AWS ECS API 호출 실패와 Throttling
* 신규 Task가 `PENDING` 상태에 장시간 머무는 경우
* 신규 Task가 Target Group Health Check를 통과하지 못하는 경우
* Drain API 실패 또는 대상 Task 응답 중단
* Control Plane 재시작 시 진행 중인 Scale-in Job 복구
* Control Plane 다중 인스턴스 실행 시 중복 스케일링 방지

현재 Redis 장애 시 세션 정보를 신뢰할 수 없는 상황에서 Scale-in을 실행하지 않는 등, 서비스 축소보다 기존 용량 유지를 우선하는 보수적인 장애 처리 정책이 필요합니다.

### 10.5 후속 검증 결과 정리

실제 AWS 자동 스케일링 통합 테스트가 완료되면 다음 결과를 추가할 예정입니다.

* 자동 Scale-out 및 Scale-in 실행 로그
* ECS Service와 Task 상태 변화
* Target Group Health 변화
* 세션 수와 권장·실제 `desiredCount` 비교
* Scale-out 반응 시간과 Drain 소요 시간
* 발생한 오류와 대응 과정
* Mock 환경과 실제 AWS 환경의 차이

의미 있는 운영 이슈가 확인되면 원인, 대응 과정 및 재검증 결과를 별도 트러블슈팅 문서로 정리할 예정입니다.

