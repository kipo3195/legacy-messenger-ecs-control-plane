# 실행 및 배포

이 문서는 `legacy-messenger-control-plane`을 **Linux 서버에서 실행하기 위한 빌드 및 배포 절차**를 설명합니다.

현재 POC에서는 Control Plane 자체를 ECS Service로 배포하지 않고, Linux용 실행 파일로 빌드한 뒤 별도 Linux 서버에서 독립 프로세스로 실행했습니다.

실행된 Control Plane은 AWS SDK를 통해 ECS, ELB, CloudWatch 리소스를 조회하고 제어합니다.

## 1. Linux 실행 파일 빌드

Linux AMD64 환경을 대상으로 실행 파일을 빌드합니다.

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -o legacy-messenger-control-plane .
```

빌드된 파일 형식을 확인합니다.

```bash
file legacy-messenger-control-plane
```

정상적으로 빌드된 경우 `ELF 64-bit` 실행 파일로 표시됩니다.

## 2. 서버로 파일 전달

빌드한 실행 파일과 설정 파일을 Linux 서버로 전달합니다.

```bash
scp legacy-messenger-control-plane <USER>@<SERVER_HOST>:/home/<USER>/control-plane/
scp -r configs <USER>@<SERVER_HOST>:/home/<USER>/control-plane/
```

서버에서 실행 권한을 부여합니다.

```bash
chmod +x legacy-messenger-control-plane
```

## 3. 실행 환경 설정

AWS Region과 애플리케이션 설정 경로를 지정합니다.

```bash
export AWS_REGION=ap-northeast-2
export ECS_CLUSTER_NAME=xxxxxxx-cluster
export SERVICE_REGISTRY_PATH=./configs/services.yaml
export AUTO_SCALE_IN_APPLIED_TIMEOUT_SECONDS=300
```

| 환경변수                                  | 설명                                      | 기본값 |
| ------------------------------------- | --------------------------------------- | --- |
| `AUTO_SCALE_IN_APPLIED_TIMEOUT_SECONDS` | Scale-in `APPLIED` 이후 대상 Task 종료 대기 시간(초) | 300 |

AWS 인증 상태를 확인합니다.

```bash
aws sts get-caller-identity
```

## 4. 애플리케이션 실행

```bash
./legacy-messenger-control-plane
```

백그라운드에서 실행하는 경우 다음과 같이 실행합니다.

```bash
nohup ./legacy-messenger-control-plane > control-plane.log 2>&1 &
```

로그를 확인합니다.

```bash
tail -f control-plane.log
```

## 5. 실행 확인

서비스 목록 조회 API를 호출하여 정상 실행 여부를 확인합니다.

```bash
curl http://localhost:8080/api/v1/services
```
