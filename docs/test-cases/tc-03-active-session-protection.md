### TC-03. Active Session Protection

#### Purpose

Scale-in 대상 Task에 활성 WebSocket session이 남아 있는 경우
Control Plane이 해당 Task의 종료를 보류하고,
session이 모두 Drain될 때까지 ECS desired count를 감소시키지 않는지 검증한다.

> 공통 Scale-in 정책 및 Drain 절차는
> [TC-02. Normal Scale-in](./tc-02-normal-scale-in.md)을 따른다.

---

#### Initial State

| Item | Value |
|---|---:|
| Total Sessions | 80 |
| Recommended Task Count | 1 |
| ECS Desired | 2 |
| ECS Running | 2 |
| ECS Pending | 0 |

예시 Task 상태:

| Task | Session Count | Status |
|---|---:|---|
| Task A | 79 | RUNNING |
| Task B | 1 | RUNNING |

Task B는 Scale-in candidate로 선정되지만,
1개의 active session이 남아 있는 상태로 테스트한다.

---

#### Test Procedure

1. Scale-in 조건을 충족시켜 Task B에 Drain 요청을 전달한다.
2. Task B의 `sessionCount=1` 상태를 유지한다.
3. Control Plane이 `WAIT_DRAIN` 상태를 기록하는지 확인한다.
4. active session이 남아 있는 동안 ECS desiredCount가 `2`로 유지되고 Task B가 RUNNING 상태를 유지하는지 확인한다.
5. 마지막 session을 종료한 뒤 `sessionCount=0` 상태가 연속 3회 확인되면 Scale-in 절차가 재개되는지 확인한다.

---

#### Pass Criteria

다음 조건을 모두 만족하면 테스트를 PASS로 판단한다.

- Drain 대상 Task에 `sessionCount > 0`인 동안 `WAIT_DRAIN` 상태가 유지된다.
- active session이 존재하는 동안 ECS desiredCount가 감소하지 않는다.
- Drain 대상 Task가 강제 종료되지 않고 RUNNING 상태를 유지한다.
- `sessionCount=0` 상태가 연속 3회 확인된 이후에만 Scale-in 절차가 계속된다.

---

#### Result

PASS

---

#### Evidence

**1. Drain Requested**

Scale-in candidate가 선정되고 Drain 요청이 전달된 것을 확인한다.

![Drain Requested](../evidence/tc-03/active-session-drain-requested.png)

---

**2. Active Session Protection**

Drain 대상 Task에 `sessionCount=1`이 남아 있어
Control Plane이 `WAIT_DRAIN` 상태를 유지하는 것을 확인한다.

이 시점에도 ECS desiredCount는 `2`이고
대상 Task는 RUNNING 상태를 유지한다.

![Active Session Protected](../evidence/tc-03/active-session-protected.png)

---

**3. Drain Completion Confirmed**

마지막 session 종료 후 `sessionCount=0` 상태가 연속 3회 확인되어
Drain 완료 조건을 충족한 것을 확인한다.

![Drain Completion Confirmed](../evidence/tc-03/active-session-drain-confirmed.png)

---

**4. Scale-in Resumed**

Drain 완료 이후 정상 Scale-in 절차가 다시 진행되는 것을 확인한다.

![Scale-in Resumed](../evidence/tc-03/active-session-scale-in-resumed.png)