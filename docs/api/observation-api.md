# 관측 API

## 1. 개요

관측 API는 Control Plane의 Service Registry에 등록된 서비스에 대해 AWS ECS, Elastic Load Balancing 및 CloudWatch에서 현재 운영 상태를 조회합니다.

관측 API는 AWS 리소스의 상태를 변경하지 않으며, 다음 정보를 확인하는 데 사용합니다.

* Control Plane이 관리하는 서비스 목록
* ECS Service의 실행 상태와 Deployment 진행 상태
* 서비스에 속한 ECS Task와 Container 상태
* Load Balancer Target Group의 Health 상태
* WebSocket 연결 지표를 기반으로 계산한 연결 부하

관측 API에서 사용하는 `{serviceName}`은 AWS ECS Service 이름이 아니라 Service Registry에 등록된 논리 서비스명입니다.

```text
ws → xxxxxx-ws-service
ds → xxxxxx-ds-service
ns → xxxxxx-ns-service
```

---

## 2. API 목록

| Method | Endpoint                                             | 설명                             |
| ------ | ---------------------------------------------------- | ------------------------------ |
| `GET`  | `/api/v1/services`                                   | 관리 대상 서비스 목록 조회                |
| `GET`  | `/api/v1/services/{serviceName}/status`              | 특정 ECS Service의 현재 상태 조회       |
| `GET`  | `/api/v1/services/{serviceName}/tasks`                | 서비스에 속한 ECS Task 목록 조회         |
| `GET`  | `/api/v1/services/{serviceName}/target-health`       | Target Group과 Target Health 조회 |
| `GET`  | `/api/v1/services/{serviceName}/connection-pressure` | 현재 연결 부하 조회                    |

모든 관측 API는 Request Body를 사용하지 않습니다.

---

# 3. 서비스 목록 조회

Control Plane의 Service Registry에 등록된 전체 서비스와 각 ECS Service의 현재 상태를 조회합니다.

## 3.1 요청

```http
GET /api/v1/services
```

### Path Parameters

없음

### Query Parameters

없음

### Request Body

없음

## 3.2 요청 예시

```bash
curl -X GET "${BASE_URL}/api/v1/services"
```

## 3.3 성공 응답

* HTTP Status: `200 OK`

```json
[
  {
    "serviceName": "ws",
    "ecsServiceName": "xxxxxx-ws-service",
    "status": "ACTIVE",
    "deployments": [
      {
        "status": "PRIMARY",
        "desiredCount": 2,
        "runningCount": 1,
        "pendingCount": 0,
        "rolloutState": "IN_PROGRESS"
      },
      {
        "status": "ACTIVE",
        "desiredCount": 1,
        "runningCount": 1,
        "pendingCount": 0,
        "rolloutState": "COMPLETED"
      }
    ]
  },
  {
    "serviceName": "ds",
    "ecsServiceName": "xxxxxx-ds-service",
    "status": "ACTIVE",
    "deployments": [
      {
        "status": "PRIMARY",
        "desiredCount": 0,
        "runningCount": 0,
        "pendingCount": 0,
        "rolloutState": "COMPLETED"
      }
    ]
  },
  {
    "serviceName": "ns",
    "ecsServiceName": "xxxxxx-ns-service",
    "status": "ACTIVE",
    "deployments": [
      {
        "status": "PRIMARY",
        "desiredCount": 0,
        "runningCount": 0,
        "pendingCount": 0,
        "rolloutState": "COMPLETED"
      }
    ]
  }
]
```

## 3.4 응답 필드

### 서비스 필드

| 필드               | 타입     | 설명                            |
| ---------------- | ------ | ----------------------------- |
| `serviceName`    | string | Service Registry에 등록된 논리 서비스명 |
| `ecsServiceName` | string | AWS ECS에 등록된 실제 Service 이름    |
| `status`         | string | ECS Service의 현재 상태            |
| `deployments`    | array  | ECS Service의 Deployment 목록    |

### `deployments` 필드

| 필드             | 타입      | 설명                              |
| -------------- | ------- | ------------------------------- |
| `status`       | string  | Deployment 유형 또는 상태             |
| `desiredCount` | integer | 해당 Deployment가 유지하려는 Task 수     |
| `runningCount` | integer | 해당 Deployment에서 실행 중인 Task 수    |
| `pendingCount` | integer | 해당 Deployment에서 시작 대기 중인 Task 수 |
| `rolloutState` | string  | Deployment의 배포 진행 상태            |

## 3.5 주요 상태값

### Service 상태

| 값          | 설명                             |
| ---------- | ------------------------------ |
| `ACTIVE`   | ECS Service가 활성 상태             |
| `DRAINING` | ECS Service가 삭제 또는 종료 절차를 진행 중 |
| `INACTIVE` | ECS Service가 비활성 상태            |

### Deployment 상태

| 값         | 설명                                           |
| --------- | -------------------------------------------- |
| `PRIMARY` | 현재 ECS Service가 목표 상태로 전환하고 있는 기본 Deployment |
| `ACTIVE`  | 교체 또는 종료 과정에 남아 있는 기존 Deployment             |

### Rollout 상태

| 값             | 설명                        |
| ------------- | ------------------------- |
| `IN_PROGRESS` | Task 생성, 교체 또는 종료가 진행 중   |
| `COMPLETED`   | Deployment가 목표 상태에 도달함    |
| `FAILED`      | Deployment가 정상적으로 완료되지 못함 |

## 3.6 운영 참고 사항

ECS Service가 재배포되거나 Task 수가 변경되는 동안에는 `PRIMARY`와 `ACTIVE` Deployment가 동시에 조회될 수 있습니다.

다음 응답은 신규 Deployment의 Task 2개 중 1개가 실행되었고, 기존 Deployment의 Task 1개가 아직 실행 중인 상태를 의미합니다.

```text
PRIMARY desired=2, running=1, rollout=IN_PROGRESS
ACTIVE  desired=1, running=1, rollout=COMPLETED
```

서비스 목록 API는 전체 관리 대상을 빠르게 확인하기 위한 용도입니다. 최근 Event, Task Definition 등 상세 정보는 서비스 상태 조회 API를 사용합니다.

---

# 4. 서비스 상태 조회

특정 서비스의 ECS Service 상태, Task 수, Task Definition, Deployment 진행 상태 및 최근 ECS Event를 조회합니다.

## 4.1 요청

```http
GET /api/v1/services/{serviceName}/status
```

### Path Parameters

| 이름            | 타입     | 필수 | 설명                            |
| ------------- | ------ | -: | ----------------------------- |
| `serviceName` | string |  예 | Service Registry에 등록된 논리 서비스명 |

### Query Parameters

없음

### Request Body

없음

## 4.2 요청 예시

```bash
curl -X GET "${BASE_URL}/api/v1/services/ws/status"
```

## 4.3 성공 응답

* HTTP Status: `200 OK`

```json
{
  "serviceName": "ws",
  "clusterName": "xxxxxx-cluster",
  "ecsServiceName": "xxxxxx-ws-service",
  "status": "ACTIVE",
  "desiredCount": 2,
  "runningCount": 2,
  "pendingCount": 0,
  "taskDefinition": "arn:aws:ecs:ap-northeast-2:123456789012:task-definition/xxxxxx-ws-task:7",
  "deployments": [
    {
      "status": "PRIMARY",
      "desiredCount": 2,
      "runningCount": 1,
      "pendingCount": 0,
      "rolloutState": "IN_PROGRESS"
    },
    {
      "status": "ACTIVE",
      "desiredCount": 1,
      "runningCount": 1,
      "pendingCount": 0,
      "rolloutState": "COMPLETED"
    }
  ],
  "events": [
    {
      "message": "(service xxxxxx-ws-service) has begun draining connections on 1 tasks.",
      "createdAt": "2026-07-09T14:12:15.657Z"
    },
    {
      "message": "(service xxxxxx-ws-service) has stopped 1 running tasks.",
      "createdAt": "2026-07-09T14:12:04.533Z"
    },
    {
      "message": "(service xxxxxx-ws-service) has started 1 tasks.",
      "createdAt": "2026-07-09T14:11:03.646Z"
    }
  ]
}
```

실제 응답의 `events`에는 AWS ECS에서 반환한 최근 Event가 여러 건 포함될 수 있습니다. 위 예시는 문서 가독성을 위해 일부 항목만 표시한 것입니다.

## 4.4 응답 필드

### 서비스 상태 필드

| 필드               | 타입      | 설명                            |
| ---------------- | ------- | ----------------------------- |
| `serviceName`    | string  | Service Registry에 등록된 논리 서비스명 |
| `clusterName`    | string  | ECS Cluster 이름                |
| `ecsServiceName` | string  | 실제 ECS Service 이름             |
| `status`         | string  | ECS Service 상태                |
| `desiredCount`   | integer | ECS Service가 유지하려는 Task 수     |
| `runningCount`   | integer | 현재 실행 중인 Task 수               |
| `pendingCount`   | integer | 생성 또는 배치를 기다리는 Task 수         |
| `taskDefinition` | string  | 서비스에 적용된 Task Definition ARN  |
| `deployments`    | array   | 현재 및 기존 Deployment 목록         |
| `events`         | array   | ECS Service의 최근 운영 Event      |

### `events` 필드

| 필드          | 타입     | 설명                           |
| ----------- | ------ | ---------------------------- |
| `message`   | string | ECS에서 제공하는 Event 메시지         |
| `createdAt` | string | Event 발생 시각. ISO 8601 UTC 형식 |

## 4.5 상태 해석

다음 값이 항상 동일하다고 볼 수는 없습니다.

```text
desiredCount
runningCount
pendingCount
```

예를 들어 Scale-out 또는 재배포가 진행 중이면 다음과 같은 상태가 발생할 수 있습니다.

```text
desiredCount = 3
runningCount = 2
pendingCount = 1
```

이는 ECS Service가 Task 3개를 목표로 하지만 현재 2개만 실행 중이고, 나머지 1개가 시작 중임을 의미합니다.

`events`에서는 다음과 같은 운영 상황을 확인할 수 있습니다.

* Task 시작 및 종료
* Target Group 등록 및 해제
* 연결 Draining 시작
* Steady State 도달
* Task 시작 반복 실패
* 배치 리소스 부족

## 4.6 운영 참고 사항

`status=ACTIVE`는 ECS Service 리소스가 활성 상태라는 의미입니다. 모든 Task와 Target이 정상이라는 의미는 아닙니다.

실제 서비스 정상 여부는 다음 API를 함께 확인해야 합니다.

```text
서비스 상태 조회
    ├─ Task 상태 조회
    └─ Target Health 조회
```

---

# 5. Task 목록 조회

특정 ECS Service에 속한 Task와 각 Task에서 실행 중인 Container 및 Network 정보를 조회합니다.

## 5.1 요청

```http
GET /api/v1/services/{serviceName}/tasks
```

### Path Parameters

| 이름            | 타입     | 필수 | 설명                            |
| ------------- | ------ | -: | ----------------------------- |
| `serviceName` | string |  예 | Service Registry에 등록된 논리 서비스명 |

### Query Parameters

없음

### Request Body

없음

## 5.2 요청 예시

```bash
curl -X GET "${BASE_URL}/api/v1/services/ws/tasks"
```

## 5.3 성공 응답

* HTTP Status: `200 OK`

```json
[
  {
    "taskId": "vdsavasdvsadvsadvsadvsd",
    "taskArn": "arn:aws:ecs:ap-northeast-2:123456789012:task/xxxxxx-cluster/dvasvasdvsdavsdavda",
    "taskDefinition": "xxxxxx-ws-task:7",
    "lastStatus": "RUNNING",
    "desiredStatus": "RUNNING",
    "healthStatus": "UNKNOWN",
    "launchType": "EC2",
    "availabilityZone": "ap-northeast-2c",
    "containerInstanceArn": "arn:aws:ecs:ap-northeast-2:123456789012:container-instance/xxxxxx-cluster/example",
    "createdAt": "2026-07-09T14:11:03.603Z",
    "startedAt": "2026-07-09T14:11:15.308Z",
    "containers": [
      {
        "name": "xxxxxx-ws",
        "lastStatus": "RUNNING",
        "healthStatus": "UNKNOWN",
        "image": "123456789012.dkr.ecr.ap-northeast-2.amazonaws.com/xxxxxx-ws:0.1.0",
        "runtimeId": "dsvasdvsdvsdvdsvsdvsdavasdvsdav",
        "networkBindings": [
          {
            "bindIp": "0.0.0.0",
            "containerPort": 33002,
            "hostPort": 36389,
            "protocol": "tcp"
          }
        ],
        "networkInterfaces": []
      }
    ],
    "network": {
      "modeHint": "bridge",
      "bindings": [
        {
          "bindIp": "0.0.0.0",
          "containerPort": 33002,
          "hostPort": 36389,
          "protocol": "tcp"
        }
      ]
    }
  }
]
```

실제 응답은 실행 중인 Task 수에 따라 배열 항목이 여러 개 반환될 수 있습니다.

## 5.4 응답 필드

### Task 필드

| 필드                     | 타입     | 설명                                                 |
| ---------------------- | ------ | -------------------------------------------------- |
| `taskId`               | string | ECS Task 식별자                                       |
| `taskArn`              | string | ECS Task ARN                                       |
| `taskDefinition`       | string | Task에서 사용하는 Task Definition 이름과 Revision           |
| `lastStatus`           | string | Task의 현재 상태                                        |
| `desiredStatus`        | string | ECS가 Task에 기대하는 목표 상태                              |
| `healthStatus`         | string | Task Health 상태                                     |
| `launchType`           | string | Task 실행 방식                                         |
| `availabilityZone`     | string | Task가 실행되는 Availability Zone                       |
| `containerInstanceArn` | string | EC2 Launch Type에서 Task가 배치된 Container Instance ARN |
| `capacityProviderName` | string | 사용된 Capacity Provider 이름                           |
| `createdAt`            | string | Task 생성 시각                                         |
| `startedAt`            | string | Task 실행 시작 시각                                      |
| `stoppingAt`           | string | Task 종료 절차 시작 시각                                   |
| `stoppedAt`            | string | Task 종료 완료 시각                                      |
| `stopCode`             | string | Task 중지 코드                                         |
| `stoppedReason`        | string | Task가 중지된 원인                                       |
| `containers`           | array  | Task에 포함된 Container 정보                             |
| `network`              | object | Task Network Mode와 Port Binding 요약                 |

종료 관련 필드나 Capacity Provider 정보는 값이 없으면 응답에서 생략될 수 있습니다.

### `containers` 필드

| 필드                  | 타입      | 설명                                    |
| ------------------- | ------- | ------------------------------------- |
| `name`              | string  | Container 이름                          |
| `lastStatus`        | string  | Container의 현재 상태                      |
| `healthStatus`      | string  | Container Health 상태                   |
| `image`             | string  | Container Image 주소와 Tag               |
| `runtimeId`         | string  | Container Runtime 식별자                 |
| `exitCode`          | integer | 종료된 Container의 Exit Code              |
| `reason`            | string  | Container 상태 변경 또는 종료 원인              |
| `networkBindings`   | array   | Bridge 또는 Host Mode의 Port Binding     |
| `networkInterfaces` | array   | `awsvpc` Mode에서 할당된 Network Interface |

### `networkBindings` 필드

| 필드              | 타입      | 설명                              |
| --------------- | ------- | ------------------------------- |
| `bindIp`        | string  | Host에서 Port가 Binding된 IP        |
| `containerPort` | integer | Container 내부 Port               |
| `hostPort`      | integer | EC2 Host에 동적으로 또는 고정으로 할당된 Port |
| `protocol`      | string  | Network Protocol                |

### `network` 필드

| 필드           | 타입     | 설명                             |
| ------------ | ------ | ------------------------------ |
| `modeHint`   | string | Task의 Network Mode             |
| `bindings`   | array  | Task에서 확인된 Port Binding 목록     |
| `interfaces` | array  | Task에 할당된 Network Interface 목록 |

## 5.5 주요 상태값

### Task 상태

| 값                | 설명                                   |
| ---------------- | ------------------------------------ |
| `PROVISIONING`   | Task 실행에 필요한 리소스 준비 중                |
| `PENDING`        | Container 실행 준비 또는 배치 중              |
| `ACTIVATING`     | Network와 Load Balancer 연동 등 활성화 작업 중 |
| `RUNNING`        | Task 실행 중                            |
| `DEACTIVATING`   | 종료 전 Load Balancer 등록 해제 등의 작업 중     |
| `STOPPING`       | Task 종료 중                            |
| `DEPROVISIONING` | Task 리소스 정리 중                        |
| `STOPPED`        | Task 종료 완료                           |

### Health 상태

| 값           | 설명                                |
| ----------- | --------------------------------- |
| `HEALTHY`   | Health Check 정상                   |
| `UNHEALTHY` | Health Check 실패                   |
| `UNKNOWN`   | Health Check가 정의되지 않았거나 판단 정보가 없음 |

`UNKNOWN`은 반드시 장애를 의미하지 않습니다. Task Definition에 Container Health Check가 정의되지 않은 경우에도 반환될 수 있습니다.

## 5.6 Network Mode 해석

예시 응답은 `bridge` Network Mode를 사용합니다.

```json
{
  "modeHint": "bridge",
  "bindings": [
    {
      "containerPort": 33002,
      "hostPort": 36389
    }
  ]
}
```

이는 Container 내부 `33002` Port가 EC2 Host의 `36389` Port에 연결되었음을 의미합니다.

Task가 여러 개 실행되면 각 Task에 서로 다른 Host Port가 할당될 수 있습니다.

```text
Task A: 33002 → 36388
Task B: 33002 → 36389
```

---

# 6. Target Health 조회

특정 서비스와 연결된 Target Group 및 등록된 Target의 Health 상태를 조회합니다.

## 6.1 요청

```http
GET /api/v1/services/{serviceName}/target-health
```

### Path Parameters

| 이름            | 타입     | 필수 | 설명                            |
| ------------- | ------ | -: | ----------------------------- |
| `serviceName` | string |  예 | Service Registry에 등록된 논리 서비스명 |

### Query Parameters

없음

### Request Body

없음

## 6.2 요청 예시

```bash
curl -X GET "${BASE_URL}/api/v1/services/ws/target-health"
```

## 6.3 성공 응답

* HTTP Status: `200 OK`

```json
{
  "serviceName": "ws",
  "ecsServiceName": "xxxxxx-ws-service",
  "clusterName": "xxxxxx-cluster",
  "overallStatus": "TRANSITIONING",
  "summary": {
    "total": 3,
    "healthy": 2,
    "unhealthy": 0,
    "initial": 0,
    "draining": 1,
    "unused": 0,
    "unavailable": 0
  },
  "targetGroups": [
    {
      "targetGroupArn": "arn:aws:elasticloadbalancing:ap-northeast-2:123456789012:targetgroup/xxxxxx-ws-bridge-tg/example",
      "targetGroupName": "xxxxxx-ws-bridge-tg",
      "loadBalancerType": "alb",
      "total": 3,
      "healthy": 2,
      "unhealthy": 0,
      "initial": 0,
      "draining": 1,
      "unused": 0,
      "unavailable": 0,
      "targets": [
        {
          "targetId": "i-0123456789abcdef0",
          "port": 36388,
          "state": "healthy"
        },
        {
          "targetId": "i-0123456789abcdef0",
          "port": 36389,
          "state": "healthy"
        },
        {
          "targetId": "i-0123456789abcdef0",
          "port": 36387,
          "state": "draining",
          "reason": "Target.DeregistrationInProgress",
          "description": "Target deregistration is in progress"
        }
      ]
    }
  ]
}
```

## 6.4 응답 필드

### 기본 필드

| 필드               | 타입     | 설명                            |
| ---------------- | ------ | ----------------------------- |
| `serviceName`    | string | Service Registry에 등록된 논리 서비스명 |
| `ecsServiceName` | string | 실제 ECS Service 이름             |
| `clusterName`    | string | ECS Cluster 이름                |
| `overallStatus`  | string | 전체 Target Group을 기준으로 계산한 상태  |
| `summary`        | object | 전체 Target 상태 집계               |
| `targetGroups`   | array  | 서비스에 연결된 Target Group별 상태     |

### `summary` 필드

| 필드            | 타입      | 설명                                      |
| ------------- | ------- | --------------------------------------- |
| `total`       | integer | 등록된 전체 Target 수                         |
| `healthy`     | integer | 정상 상태 Target 수                          |
| `unhealthy`   | integer | Health Check 실패 Target 수                |
| `initial`     | integer | 초기 Health Check 진행 중인 Target 수          |
| `draining`    | integer | 등록 해제 및 Connection Draining 중인 Target 수 |
| `unused`      | integer | 현재 사용되지 않는 Target 수                     |
| `unavailable` | integer | 상태를 정상적으로 확인할 수 없는 Target 수             |

### `targetGroups` 필드

| 필드                 | 타입      | 설명                            |
| ------------------ | ------- | ----------------------------- |
| `targetGroupArn`   | string  | Target Group ARN              |
| `targetGroupName`  | string  | Target Group 이름               |
| `loadBalancerType` | string  | Load Balancer 유형              |
| `total`            | integer | Target Group에 등록된 전체 Target 수 |
| `healthy`          | integer | 정상 Target 수                   |
| `unhealthy`        | integer | 비정상 Target 수                  |
| `initial`          | integer | Health Check 초기 상태 Target 수   |
| `draining`         | integer | Draining 상태 Target 수          |
| `unused`           | integer | 사용되지 않는 Target 수              |
| `unavailable`      | integer | 상태 조회가 불가능한 Target 수          |
| `targets`          | array   | 개별 Target 상태                  |

### `targets` 필드

| 필드            | 타입      | 설명                                              |
| ------------- | ------- | ----------------------------------------------- |
| `targetId`    | string  | Target 식별자. Instance Target인 경우 EC2 Instance ID |
| `port`        | integer | Target Group에 등록된 Port                          |
| `state`       | string  | Target Health 상태                                |
| `reason`      | string  | 현재 상태의 원인 코드                                    |
| `description` | string  | 현재 상태에 대한 설명                                    |

`reason`과 `description`은 추가 설명이 필요한 상태에서만 반환될 수 있습니다.

## 6.5 전체 상태

| 값               | 설명                                   |
| --------------- | ------------------------------------ |
| `HEALTHY`       | 모든 Target이 정상적으로 트래픽을 받을 수 있음        |
| `DEGRADED`      | Unhealthy 또는 Unavailable Target이 존재함 |
| `TRANSITIONING` | Initial 또는 Draining Target이 존재함      |
| `UNKNOWN`       | Target Group 또는 판단 가능한 Target 정보가 없음 |

예시 응답은 Target 3개 중 2개가 `healthy`, 1개가 `draining`이므로 전체 상태가 `TRANSITIONING`입니다.

## 6.6 Target 상태

| 값             | 설명                         |
| ------------- | -------------------------- |
| `healthy`     | Health Check 정상            |
| `unhealthy`   | Health Check 실패            |
| `initial`     | 등록 직후 초기 Health Check 진행 중 |
| `draining`    | Target 등록 해제 및 기존 연결 정리 중  |
| `unused`      | Target Group에서 사용되지 않음     |
| `unavailable` | Target 상태를 확인할 수 없음        |

## 6.7 운영 참고 사항

Scale-in 또는 재배포 중에는 종료 대상 Task가 Target Group에서 `draining` 상태로 조회될 수 있습니다.

이 상태는 즉시 장애로 판단하지 않고, 기존 연결을 정리하며 Target 등록을 해제하는 정상적인 전환 과정인지 확인해야 합니다.

---

# 7. Connection Pressure 조회

CloudWatch에서 조회한 Load Balancer 연결 지표와 현재 실행 중인 Task 수를 이용하여 Task당 연결 부하를 계산합니다.

이 API는 현재 부하를 관측하고 Scale-in 또는 Scale-out 후보를 제시하지만, ECS Service의 Task 수를 직접 변경하지 않습니다.

## 7.1 요청

```http
GET /api/v1/services/{serviceName}/connection-pressure
```

### Path Parameters

| 이름            | 타입     | 필수 | 설명                            |
| ------------- | ------ | -: | ----------------------------- |
| `serviceName` | string |  예 | Service Registry에 등록된 논리 서비스명 |

### Query Parameters

없음

### Request Body

없음

## 7.2 요청 예시

```bash
curl -X GET "${BASE_URL}/api/v1/services/ws/connection-pressure"
```

## 7.3 성공 응답

* HTTP Status: `200 OK`

```json
{
  "serviceName": "ws",
  "ecsServiceName": "xxxxxx-ws-service",
  "clusterName": "xxxxxx-cluster",
  "activeConnectionCount": 6,
  "runningTaskCount": 3,
  "desiredCount": 3,
  "connectionPerTask": 2,
  "targetConnectionsPerTask": 1500,
  "pressureStatus": "LOW",
  "scalingRecommendation": {
    "action": "SCALE_IN_CANDIDATE",
    "reason": "connectionPerTask is low compared to targetConnectionsPerTask",
    "recommendedDesiredCount": 2
  },
  "metric": {
    "namespace": "AWS/ApplicationELB",
    "metricName": "ActiveConnectionCount",
    "stat": "Average",
    "periodSeconds": 60,
    "lookbackMinutes": 5
  }
}
```

## 7.4 응답 필드

### 연결 부하 필드

| 필드                         | 타입      | 설명                                  |
| -------------------------- | ------- | ----------------------------------- |
| `serviceName`              | string  | Service Registry에 등록된 논리 서비스명       |
| `ecsServiceName`           | string  | 실제 ECS Service 이름                   |
| `clusterName`              | string  | ECS Cluster 이름                      |
| `activeConnectionCount`    | number  | CloudWatch에서 조회한 활성 연결 지표           |
| `runningTaskCount`         | integer | 현재 실행 중인 ECS Task 수                 |
| `desiredCount`             | integer | ECS Service가 유지하려는 Task 수           |
| `connectionPerTask`        | number  | 실행 중인 Task 한 개당 계산된 연결 수            |
| `targetConnectionsPerTask` | integer | Service Registry에 정의된 Task당 목표 연결 수 |
| `pressureStatus`           | string  | 현재 연결 부하 상태                         |
| `scalingRecommendation`    | object  | 현재 지표를 기반으로 계산한 스케일링 후보             |
| `metric`                   | object  | 계산에 사용된 CloudWatch Metric 조건        |

### `scalingRecommendation` 필드

| 필드                        | 타입      | 설명                        |
| ------------------------- | ------- | ------------------------- |
| `action`                  | string  | 현재 부하를 기준으로 계산한 후보 Action |
| `reason`                  | string  | Action을 권장한 이유            |
| `recommendedDesiredCount` | integer | 권장하는 ECS Service의 Task 수  |

### `metric` 필드

| 필드                | 타입      | 설명                          |
| ----------------- | ------- | --------------------------- |
| `namespace`       | string  | CloudWatch Metric Namespace |
| `metricName`      | string  | 조회한 Metric 이름               |
| `stat`            | string  | Metric 집계 방식                |
| `periodSeconds`   | integer | 하나의 Datapoint가 집계되는 기간      |
| `lookbackMinutes` | integer | 현재 시점으로부터 조회하는 과거 범위        |

## 7.5 계산값 해석

예시 응답에서는 다음과 같이 Task당 연결 수가 계산됩니다.

```text
activeConnectionCount = 6
runningTaskCount      = 3
connectionPerTask     = 6 / 3 = 2
```

Task당 연결 수 `2`가 목표 연결 수 `1500`보다 충분히 낮으므로 다음 결과가 반환됩니다.

```text
pressureStatus = LOW
action         = SCALE_IN_CANDIDATE
```

`recommendedDesiredCount=2`는 Scale-in 후보값일 뿐이며 실제 ECS Service의 `desiredCount`를 변경하지 않습니다.

실제 변경은 다음 제어 API를 별도로 호출해야 합니다.

```http
POST /api/v1/services/ws/scale
```

## 7.6 Pressure 상태

| 값        | 설명                   |
| -------- | -------------------- |
| `LOW`    | Task당 연결 부하가 목표보다 낮음 |
| `NORMAL` | Task당 연결 부하가 정상 범위   |
| `HIGH`   | Task당 연결 부하가 목표보다 높음 |

## 7.7 Scaling Recommendation 상태

| 값                     | 설명                                 |
| --------------------- | ---------------------------------- |
| `SCALE_OUT_CANDIDATE` | Task 증가 검토가 필요한 상태                 |
| `SCALE_IN_CANDIDATE`  | Task 감소 검토가 가능한 상태                 |
| `KEEP`                | 현재 Task 수 유지 권장                    |
| `NOT_SCALABLE`        | Service Registry에서 스케일링 대상이 아닌 서비스 |

실제 구현에서 사용하는 Action 이름만 문서에 포함해야 합니다.

## 7.8 오류 응답

조회 범위 안에 CloudWatch Datapoint가 없으면 연결 부하를 계산할 수 없습니다.

* HTTP Status: `500 Internal Server Error`

```json
{
  "message": "failed to get active connection count: cloudwatch metric datapoint not found"
}
```

다음과 같은 상황에서 Datapoint가 조회되지 않을 수 있습니다.

* Load Balancer에 최근 연결 또는 트래픽이 없음
* Metric 전송 또는 집계가 아직 완료되지 않음
* 조회 대상 Load Balancer 또는 Target Group 설정이 올바르지 않음
* CloudWatch 조회 기간 안에 유효한 값이 없음

## 7.9 지표 해석 시 주의사항

`ActiveConnectionCount`는 Load Balancer와 연결된 활성 연결에 대한 CloudWatch 집계 지표입니다.

따라서 이 값은 메신저 애플리케이션이 직접 관리하는 로그인 사용자 수 또는 WebSocket Session 수와 일대일로 대응하지 않을 수 있습니다.

실제 호출 결과에서도 클라이언트 한 개가 로그인한 상황에서 `activeConnectionCount=6`이 반환되었습니다.

따라서 이 API는 다음 목적으로 사용합니다.

* 현재 Load Balancer 연결 추세 확인
* Task당 연결 부하의 대략적인 관측
* 스케일링 후보 판단을 위한 보조 지표

정확한 로그인 사용자 또는 WebSocket Session 수를 기반으로 동적 확장을 판단하려면 각 WebSocket Task가 관리 중인 Session 수를 직접 보고하고, Control Plane이 이를 별도로 집계하는 구조가 필요합니다.

---

# 8. 공통 오류 응답

관측 API 처리 중 오류가 발생하면 다음 형식으로 응답합니다.

```json
{
  "message": "error message"
}
```

|                       상태 코드 | 발생 조건                               |
| --------------------------: | ----------------------------------- |
| `500 Internal Server Error` | Service Registry 조회 실패              |
| `500 Internal Server Error` | ECS Service 또는 Task 조회 실패           |
| `500 Internal Server Error` | Target Group 또는 Target Health 조회 실패 |
| `500 Internal Server Error` | CloudWatch Metric 조회 실패             |
| `500 Internal Server Error` | 응답 데이터 변환 또는 내부 처리 실패               |

현재 POC에서는 오류 유형이 세분화되지 않아 등록되지 않은 서비스와 외부 AWS API 호출 실패가 모두 `500 Internal Server Error`로 반환될 수 있습니다.

---

# 9. API별 사용 목적

| API                 | 주요 관점              | 사용 목적                                  |
| ------------------- | ------------------ | -------------------------------------- |
| 서비스 목록              | Control Plane 전체   | 관리 대상 서비스와 Deployment 상태 확인            |
| 서비스 상태              | ECS Service        | desired·running·pending 수와 최근 Event 확인 |
| Task 목록             | ECS Task·Container | 개별 Task 실행 및 Network 상태 확인             |
| Target Health       | Load Balancer      | 실제 트래픽 전달 가능 상태 확인                     |
| Connection Pressure | CloudWatch·ECS     | 현재 연결 부하와 스케일링 후보 확인                   |

서비스 운영 상태를 종합적으로 확인할 때는 다음 순서로 조회할 수 있습니다.

```text
서비스 목록
    │
    ▼
서비스 상태
    │
    ├─ Task 상세 상태
    ├─ Target Health
    └─ Connection Pressure
```
