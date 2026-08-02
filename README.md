# AWG3 RouterOS Gateway

Единый multi-architecture gateway product для полноценного AWG3 data plane в RouterOS Containers.

Статус: correction pass in progress; production implementation выполняет младшая модель после обязательных gates. Gate C/E пока не открывать.

## Product targets

| Platform | Target | Delivery |
|---|---|---|
| `linux/arm64` | RB5009 | RouterOS custom App |
| `linux/arm/v5` | hAP ac² и совместимые ARM32 | ordinary RouterOS `/container` + generated installer |
| `linux/amd64` | CI/lab и AEZA tooling/runtime | ordinary OCI/runtime |

Один source tree, один release, одна configuration model. Architecture-specific application forks запрещены.

ARMv5 steady state: только `amneziawg-go` и один статический Go supervisor. Supervisor совмещает lifecycle, lightweight health/status API и on-demand configuration control plane; постоянный configuration UI process отсутствует.

Функциональность не урезается: on-demand UI/API позволяет редактировать keys/PSK/HeaderProtectionKey, endpoint, tunnel addressing, AllowedIPs, MTU и полный AWG3 profile. Canonical editable state хранится на persistent mount в `/config/awg3.json` и `/config/secrets.json`; generation transaction и secret separation are enforced in code. API/schema/UI одинаковы на amd64, arm64 и ARMv5.

## Canonical project documents

- `PRODUCT_ARCHITECTURE.md` — product invariants, layout, schemas and build matrix.
- `docs/AWG3_AEZA_RB5009_MUTATION_RESEARCH_2026-08-01.md` — protocol/migration research.
- `docs/AWG3_MUTATION_JUNIOR_EXECUTION_HANDOFF_2026-08-02.md` — gated execution plan and junior-model instruction.

## Scope boundary

Production wave: AEZA ↔ RB5009 only.  
hAP ac²: isolated ARMv5 hardware acceptance only; its production routes/tunnels/failover are not migration scope.
