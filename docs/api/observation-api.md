# API 상세 문서

## 1. 개요

`legacy-messenger-control-plane`은 AWS ECS 환경에서 실행되는 레거시 메신저 서비스를 관측하고 제어하기 위한 REST API를 제공합니다.

API는 역할에 따라 다음 세 영역으로 구분합니다.

| 구분 | 역할                                          | 상세 문서                                      |
| -- | ------------------------------------------- | ------------------------------------------ |
| 관측 | ECS Service, Task, Target Health 및 연결 부하 조회 | [observation-api.md](./observation-api.md) |
| 제어 | ECS Service의 `desiredCount` 변경 및 강제 재배포     | [control-api.md](./control-api.md)         |
| 판단 | 현재 연결 부하를 기반으로 스케일링 필요 여부와 권장 Task 수 계산     | [scaling-api.md](./scaling-api.md)         |
| 수집 | 현재 ECS Service의 taskID의 Session Count 수집    | [collect-api.md](./collect-api.md)         |

---

## 2. 공통 요청 정보

### 2.1 Base Path

모든 Control Plane API는 다음 경로를 기준으로 합니다.

```text
/api/v1
```

로컬 실행 환경이 다음과 같다면:

```text
http://localhost:33001
```

전체 요청 URL은 다음과 같이 구성됩니다.

```text
http://localhost:33001/api/v1/services
```

실행 환경의 Host와 Port는 환경 설정에 따라 달라질 수 있습니다.

### 2.2 Content-Type

요청 Body를 사용하는 API는 JSON 형식을 사용합니다.

```http
Content-Type: application/json
```

모든 성공 및 오류 응답도 JSON 형식으로 반환합니다.

---

## 3. 서비스 식별자

API 경로의 `{serviceName}`은 AWS ECS에 등록된 실제 Service 이름이 아니라, Control Plane의 Service Registry에 등록된 **논리 서비스명**을 의미합니다.

현재 Service Registry에 등록된 서비스는 다음과 같습니다.

| `serviceName` | ECS Service 이름      | 설명                  |
| ------------- | ------------------- | ------------------- |
| `ws`          | `xxxxxx-ws-service` | WebSocket Service   |
| `ds`          | `xxxxxx-ds-service` | Dispatcher Service  |
| `ns`          | `xxxxxx-ns-service` | Notificator Service |

예를 들어 WebSocket 서비스의 상태를 조회할 때는 다음과 같이 논리 서비스명인 `ws`를 사용합니다.

```http
GET /api/v1/services/ws
```

실제 ECS Service 이름은 API 경로에 직접 사용하지 않습니다.

```http
GET /api/v1/services/xxxxxx-ws-service
```

Control Plane은 요청받은 논리 서비스명을 Service Registry에서 조회한 뒤, 내부적으로 실제 ECS Service 이름과 운영 정책을 확인합니다.

Service Registry에는 서비스별로 다음 정보가 정의됩니다.

* 실제 ECS Service 이름
* 서비스 표시 이름
* 스케일링 가능 여부
* 최소 Task 수
* 최대 Task 수
* Load Balancer 유형
* Task당 목표 연결 수

등록되지 않은 `serviceName`은 관리 대상 서비스로 처리하지 않습니다.

---

## 4. 인증 및 접근 제어

현재 POC에서는 Control Plane REST API 호출자에 대한 애플리케이션 수준의 인증 및 권한 검증을 구현하지 않았습니다.

따라서 현재 API 호출에는 별도의 `Authorization` 헤더가 필요하지 않습니다.

```http
GET /api/v1/services
```

다만 Control Plane이 AWS ECS, Elastic Load Balancing 및 CloudWatch API를 호출하기 위해서는 실행 환경에 AWS 인증 정보와 필요한 IAM 권한이 설정되어 있어야 합니다.

인증의 적용 범위는 다음과 같이 구분됩니다.

| 구분                      | 현재 적용 여부 | 설명                             |
| ----------------------- | -------- | ------------------------------ |
| API 호출자 → Control Plane | 미적용      | 별도의 사용자 인증 및 권한 검증 없음          |
| Control Plane → AWS API | 적용       | AWS Credentials 또는 IAM Role 사용 |

운영 환경에 적용할 경우에는 다음 보안 구성이 추가로 필요합니다.

* Control Plane 접근 가능 네트워크 제한
* 운영자 또는 관리자 인증
* 관측 API와 제어 API의 권한 분리
* 제어 요청에 대한 감사 로그
* 비인가 요청에 대한 접근 차단

특히 제어 API는 실제 ECS Service의 상태를 변경하므로 외부에 공개하지 않고, 내부 운영 환경에서 제한적으로 사용하는 것을 전제로 합니다.

---

## 5. API 목록

### 5.1 관측 API

AWS ECS, Elastic Load Balancing 및 CloudWatch에서 서비스 운영 상태를 조회합니다.

관측 API는 AWS 리소스 상태를 변경하지 않습니다.

| Method | Endpoint                                             | 설명                                       |
| ------ | ---------------------------------------------------- | ---------------------------------------- |
| `GET`  | `/api/v1/services`                                   | 관리 대상 서비스 목록 조회                          |
| `GET`  | `/api/v1/services/{serviceName}/status`              | 특정 ECS Service의 현재 상태 조회                 |
| `GET`  | `/api/v1/services/{serviceName}/tasks`               | 서비스에서 실행 중인 Task 목록과 상세 상태 조회            |
| `GET`  | `/api/v1/services/{serviceName}/target-health`       | 서비스와 연결된 Target Group 및 Target Health 조회 |
| `GET`  | `/api/v1/services/{serviceName}/connection-pressure` | 연결 수와 실행 중인 Task 수를 기반으로 현재 연결 부하 조회     |

상세 요청 및 응답 형식은 [관측 API 문서](./observation-api.md)를 참고합니다.

### 5.2 제어 API

AWS ECS Service의 실행 상태를 변경합니다.

| Method | Endpoint                                  | 설명                                   |
| ------ | ----------------------------------------- | ------------------------------------ |
| `POST` | `/api/v1/services/{serviceName}/scale`    | ECS Service의 `desiredCount` 변경       |
| `POST` | `/api/v1/services/{serviceName}/redeploy` | `forceNewDeployment`를 이용한 서비스 강제 재배포 |

제어 요청에는 Service Registry에 정의된 스케일링 가능 여부와 최소·최대 Task 수 등의 운영 정책이 적용됩니다.

상세 요청 및 응답 형식은 [제어 API 문서](./control-api.md)를 참고합니다.

### 5.3 스케일링 판단 API

현재 연결 부하와 Service Registry의 운영 정책을 이용하여 Scale-out, Scale-in 또는 현재 상태 유지 여부를 계산합니다.

| Method | Endpoint                                          | 설명                                |
| ------ | ------------------------------------------------- | --------------------------------- |
| `POST` | `/api/v1/services/{serviceName}/scaling-evaluate` | 스케일링 필요 여부 및 권장 `desiredCount` 계산 |

스케일링 판단 API는 권장 결과만 반환하며, ECS Service의 `desiredCount`를 직접 변경하지 않습니다.

실제 Task 수를 변경하려면 제어 API의 Scale 요청을 별도로 호출해야 합니다.

상세 요청 및 응답 형식은 [스케일링 판단 API 문서](./scaling-api.md)를 참고합니다.

---

## 6. 공통 성공 응답

API 호출이 정상적으로 처리되면 각 API의 목적에 맞는 JSON 데이터를 반환합니다.

```http
HTTP/1.1 200 OK
Content-Type: application/json
```

성공 응답의 필드와 상태값은 API마다 다르므로 각 상세 문서에서 별도로 설명합니다.

---

## 7. 공통 오류 응답

API 처리 중 오류가 발생하면 다음 형식으로 오류 메시지를 반환합니다.

```json
{
  "message": "error message"
}
```

| 필드        | 타입     | 설명            |
| --------- | ------ | ------------- |
| `message` | string | 요청 처리에 실패한 원인 |

현재 구현에서 사용하는 주요 HTTP 상태 코드는 다음과 같습니다.

|                       상태 코드 | 설명                                                  |
| --------------------------: | --------------------------------------------------- |
|                    `200 OK` | 요청 처리 성공                                            |
|           `400 Bad Request` | 요청 Body 형식 오류 또는 필수 입력값 누락                          |
| `500 Internal Server Error` | Service Registry 조회, 운영 정책 검증 또는 AWS API 호출 중 오류 발생 |

현재 POC에서는 등록되지 않은 서비스, 스케일링 정책 위반, AWS API 호출 실패 등의 오류가 대부분 `500 Internal Server Error`로 반환될 수 있습니다.

오류 유형별 상세 발생 조건은 각 API 문서에서 설명합니다.

---

## 8. AWS 리소스 상태와 응답 시점

관측 API 응답은 API가 호출된 시점에 AWS에서 조회한 상태를 기준으로 합니다.

ECS Service, Task, Deployment 및 Target Health 상태는 AWS 내부 처리에 따라 지속적으로 변경될 수 있으므로, API 응답은 특정 시점의 상태를 나타내는 값으로 해석해야 합니다.

특히 다음 작업은 요청 직후 즉시 완료되지 않을 수 있습니다.

* ECS Service의 `desiredCount` 변경
* 신규 Task 생성 및 실행
* 기존 Task 종료
* 강제 재배포
* Load Balancer Target 등록
* Target Health Check 통과

제어 API가 성공 응답을 반환했다는 것은 AWS에 변경 요청이 정상적으로 전달되었다는 의미이며, 모든 Task 전환과 Health Check가 완료되었다는 의미는 아닙니다.

제어 요청 이후에는 관측 API를 이용하여 실제 반영 상태를 확인해야 합니다.

```text
제어 API 호출
      │
      ▼
ECS 변경 요청
      │
      ▼
Service 및 Task 상태 조회
      │
      ▼
Target Health 확인
```

---

## 9. 관측·판단·제어 API의 관계

Control Plane은 상태 조회, 스케일링 판단 및 실제 제어를 서로 분리합니다.

```text
관측 API
ECS / ELB / CloudWatch 상태 조회
        │
        ▼
스케일링 판단 API
현재 부하와 운영 정책 비교
        │
        ▼
권장 Action 및 desiredCount 반환
        │
        ▼
제어 API
운영자의 확인 후 실제 ECS 상태 변경
```

각 API의 역할은 다음과 같습니다.

| 구분          | AWS 상태 조회 | 판단 결과 계산 | AWS 상태 변경 |
| ----------- | --------: | -------: | --------: |
| 관측 API      |         O | 일부 부하 계산 |         X |
| 스케일링 판단 API |         O |        O |         X |
| 제어 API      |         O |    정책 검증 |         O |

스케일링 판단 결과가 `SCALE_OUT` 또는 `SCALE_IN`으로 반환되더라도 실제 ECS Service의 Task 수는 자동으로 변경되지 않습니다.

현재 구조에서는 판단 결과를 확인한 뒤 제어 API를 별도로 호출해야 합니다.

---

