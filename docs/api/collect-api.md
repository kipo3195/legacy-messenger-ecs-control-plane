# 수집 API

## 1. 개요

수집 API는 AWS ECS Service에서 실행 중인 각 Task가 현재 보유한 세션 수를 Control Plane에 주기적으로 보고하기 위한 API입니다.

Control Plane은 Task별 최신 세션 보고를 Redis에 저장하고, 수집된 값을 기반으로 다음 작업을 수행합니다.

* ECS Task별 `sessionCount` 수집
* 서비스 전체 세션 수 집계
* 보고 만료 여부 확인
* 비정상 또는 보고가 중단된 Task 판별
* 세션 부하를 기반으로 한 Scale-out 및 Scale-in 판단

```text
ECS Task
   │
   │ Session Report
   ▼
Control Plane
   │
   ├─ Task별 최신 Report 저장
   ├─ Report 만료 여부 확인
   ├─ 전체 Session Count 집계
   └─ 스케일링 정책 평가
          │
          ├─ Scale-out
          ├─ Scale-in 후보 선정
          └─ 현재 상태 유지
```

Task는 일정 주기로 이 API를 호출해야 합니다.

동일한 `serviceName`과 `taskID`로 다시 보고하면 이전 보고값은 최신 `sessionCount`, `reportedAt`, `expiresAt`으로 갱신됩니다.

---

## 2. API 목록

| Method | Endpoint                                                       | 설명               |
| ------ | -------------------------------------------------------------- | ---------------- |
| `PUT`  | `/api/v1/services/{serviceName}/tasks/{taskID}/session-report` | 현재 Task의 세션 수 보고 |

---

## 3. Task 세션 수 보고

현재 Task가 보유한 세션 수를 Control Plane에 보고합니다.

### 3.1 요청

```http
PUT /api/v1/services/{serviceName}/tasks/{taskID}/session-report
Content-Type: application/json
```

#### Path Parameters

| 이름            | 타입     | 필수 | 설명                            |
| ------------- | ------ | -: | ----------------------------- |
| `serviceName` | string |  예 | Service Registry에 등록된 논리 서비스명 |
| `taskID`      | string |  예 | 세션 수를 보고하는 ECS Task 식별자       |

API 경로의 `{serviceName}`은 실제 AWS ECS Service 이름이 아니라 Control Plane의 Service Registry에 등록된 논리 서비스명을 사용합니다.

예:

```text
ws → WebSocket Service
ds → Dispatcher Service
ns → Notificator Service
```

#### Query Parameters

없음

#### Request Body

```json
{
  "sessionCount": 250
}
```

| 필드             | 타입      | 필수 | 설명                |
| -------------- | ------- | -: | ----------------- |
| `sessionCount` | integer |  예 | 현재 Task가 보유한 세션 수 |

`sessionCount`는 0 이상의 정수여야 합니다.

세션이 없는 Task도 보고를 중단하지 않고 다음과 같이 `0`을 보고해야 합니다.

```json
{
  "sessionCount": 0
}
```

---

### 3.2 요청 예시

```bash
curl -X PUT \
  "${BASE_URL}/api/v1/services/ws/tasks/ws-task-1234/session-report" \
  -H "Content-Type: application/json" \
  -d '{
    "sessionCount": 250
  }'
```

---

### 3.3 성공 응답

* HTTP Status: `200 OK`

```json
{
  "serviceName": "ws",
  "taskId": "ws-task-1234",
  "sessionCount": 250,
  "reportedAt": "2026-08-04T15:19:03.680512+09:00",
  "expiresAt": "2026-08-04T15:20:03.680512+09:00"
}
```

---

### 3.4 응답 필드

| 필드             | 타입      | 설명                            |
| -------------- | ------- | ----------------------------- |
| `serviceName`  | string  | Service Registry에 등록된 논리 서비스명 |
| `taskId`       | string  | 세션 수를 보고한 Task 식별자            |
| `sessionCount` | integer | 해당 Task가 보고한 현재 세션 수          |
| `reportedAt`   | string  | Control Plane이 보고를 수신한 시각     |
| `expiresAt`    | string  | 해당 보고가 유효한 것으로 인정되는 만료 기준 시각  |

`reportedAt`과 `expiresAt`은 ISO 8601 형식으로 반환됩니다.

`expiresAt`은 Redis Key 자체가 삭제되는 시각을 의미하지 않습니다. Control Plane이 해당 Task의 보고를 정상 보고로 인정하는 유효기간의 기준 시각입니다.

---

### 3.5 보고 데이터 처리

Control Plane은 `serviceName`과 `taskID`를 기준으로 Task별 최신 세션 보고를 저장합니다.

동일한 Task가 다시 보고하면 기존 데이터의 `sessionCount`, `reportedAt`, `expiresAt`이 최신 값으로 갱신됩니다.

수집된 데이터는 서비스 전체 세션 수 집계와 Task별 보고 만료 여부를 판단하는 데 사용됩니다.

---

### 3.6 보고 주기와 만료 처리

Task는 설정된 보고 유효시간보다 짧은 주기로 세션 수를 보고해야 합니다.

예를 들어 다음과 같이 설정되어 있다면:

```text
보고 주기: 10초
보고 유효시간: 60초
```

Task는 정상 상태에서 약 10초마다 보고하며, Control Plane은 마지막 보고 이후 60초가 지난 Report를 만료 대상으로 판단합니다.

```text
Task 보고
   │
   ├─ 10초마다 정상 보고
   │       └─ reportedAt, expiresAt 갱신
   │
   └─ 보고 중단
           │
           ▼
       expiresAt 경과
           │
           ▼
       만료 Report 판별
```

보고 만료 여부는 Task의 비정상 상태를 판별하기 위한 기준으로 사용됩니다.

Task 종료 또는 Scale-in 여부는 별도의 정책 평가를 통해 결정됩니다.

---

### 3.7 스케일링 처리와의 관계

세션 보고 API는 Task의 현재 세션 수를 수집하고 저장하는 역할만 수행합니다.

API 호출 성공이 즉시 Scale-out 또는 Scale-in 실행을 의미하지는 않습니다.

별도의 스케일링 스케줄러가 수집된 세션 수와 현재 ECS 상태를 평가하여 스케일링 여부를 결정합니다.

---

### 3.8 요청 검증 조건

세션 보고 요청은 다음 조건을 만족해야 합니다.

* `serviceName`이 Service Registry에 등록되어 있어야 함
* `taskID`가 빈 문자열이 아니어야 함
* Request Body가 올바른 JSON 형식이어야 함
* `sessionCount`가 누락되지 않아야 함
* `sessionCount`가 정수여야 함
* `sessionCount`가 0 이상이어야 함

---

### 3.9 오류 응답

#### Request Body 형식 오류

* HTTP Status: `400 Bad Request`

```json
{
  "message": "invalid request body"
}
```

발생 가능한 조건:

* JSON 문법 오류
* `sessionCount` 누락
* `sessionCount` 타입 오류

#### 잘못된 세션 수

* HTTP Status: `400 Bad Request`

```json
{
  "message": "sessionCount must be zero or greater"
}
```

발생 가능한 조건:

* `sessionCount`가 음수인 경우

#### 등록되지 않은 서비스

* HTTP Status: `404 Not Found`

```json
{
  "message": "service not found"
}
```

발생 가능한 조건:

* `serviceName`이 Service Registry에 등록되지 않은 경우

#### Report 저장 실패

* HTTP Status: `500 Internal Server Error`

```json
{
  "message": "failed to save task session report"
}
```

발생 가능한 조건:

* Redis 연결 실패
* Redis 데이터 저장 실패
* Report 만료 정보 저장 실패
* 내부 데이터 변환 실패

---

### 3.10 참고사항

현재 POC에서는 별도의 인증 및 권한 검증을 적용하지 않았습니다.

운영 환경에 적용할 경우 ECS Task 또는 내부 서비스만 접근할 수 있도록 네트워크와 인증 정책을 추가해야 합니다.
