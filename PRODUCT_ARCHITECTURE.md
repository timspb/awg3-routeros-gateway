# AWG3 RouterOS Gateway — product architecture

## 1. Product invariant

`AWG3 RouterOS Gateway` is one application built from one source tree into a multi-platform OCI index.

Forbidden:

- ARM-specific or ARM64-specific forks;
- different environment/secrets schemas;
- adapter-owned AWG3 configuration logic;
- different API/UI/Monitoring semantics;
- platform-dependent interface or tunnel naming rules.

Allowed platform differences:

- compiler target and compatible runtime layers;
- image manifest/layer digest;
- RouterOS deployment adapter;
- optional low-level implementation needed solely for ABI/device integration, hidden behind the same application contract.

## 2. Build matrix

| Field | amd64 | arm64 | ARM32 |
|---|---|---|---|
| OCI platform | `linux/amd64` | `linux/arm64` | `linux/arm/v5` |
| Go target | `GOARCH=amd64` | `GOARCH=arm64` | `GOARCH=arm GOARM=5` |
| Primary use | CI/lab | RB5009 production | hAP ac² acceptance/deployment |
| RouterOS adapter | none/lab | custom `/app` | ordinary `/container` installer |
| Hardware acceptance | CI interop | RB5009 | hAP ac² |

Every binary and shared/static runtime dependency must match the target ABI. Build acceptance must inspect and execute:

- `amneziawg-go`;
- `awg`/amneziawg-tools;
- `ip`/iproute2;
- firewall/NAT helper if retained;
- init/entrypoint;
- health/status API and web UI server;
- libc/loader/shared libraries, if dynamically linked.

ARM32 acceptance requires actual ARMv5-compatible execution. `file`/`readelf` inspection alone is insufficient.

### 2.1. ARMv5 memory-minimal runtime

В steady state на `linux/arm/v5` постоянно работают только:

1. `amneziawg-go`;
2. один минимальный статически собранный Go supervisor.

Лёгкий health/status API является частью supervisor, а не третьим процессом. Отдельный постоянный web UI process запрещён.

В ARMv5 runtime запрещены:

- Python;
- Node.js;
- nginx или другой reverse proxy;
- systemd;
- база данных;
- отдельный frontend/backend daemon;
- тяжёлые web frameworks;
- shell-based постоянный process manager.

Supervisor должен использовать Go standard library или сопоставимо лёгкие compile-time dependencies, собираться статически с `CGO_ENABLED=0`, `GOARCH=arm`, `GOARM=5` и не требовать dynamic loader. Если `amneziawg-go`, `awg` или `iproute2` остаются динамическими, их loader/libs должны быть ARMv5-compatible и включены явно; предпочтителен проверенный статический/минимальный вариант.

## 3. Canonical repository layout

The implementing model should create this structure without architecture forks:

```text
AWG3_ROUTEROS_GATEWAY/
  README.md
  PRODUCT_ARCHITECTURE.md
  docs/
  cmd/gateway/                 # one gateway/status/UI process
  internal/
    config/                    # one environment/config schema
    runtime/                   # AWG lifecycle and forwarding
    control/                   # atomic config and on-demand UI state
    health/                    # one health/status contract
    monitoring/                # Monitoring 101 projection
  web/                         # one web UI
  api/                         # versioned status schema
  build/
    Dockerfile                 # one multi-stage multi-platform build
    bake.hcl                   # amd64/arm64/arm-v5 matrix
    checks/                    # ABI, schema and image checks
  deploy/
    routeros-app-arm64/        # deployment metadata only
    routeros-container-armv5/  # installer generator/templates only
    oci-amd64/                 # CI/lab adapter only
  schemas/
    awg3.schema.json
    secrets.schema.json
    status.schema.json
    control-api.openapi.yaml
  tests/
    contract/
    interop/
    hardware/
```

No directory such as `src-arm`, `src-arm64`, `config-arm` or `ui-arm` is permitted.

## 4. Common runtime contract

### 4.1. Interface and network

The common config model must express, without platform defaults:

- AWG interface logical name;
- tunnel address/CIDR;
- peer endpoint and AllowedIPs;
- MTU;
- RouterOS-facing veth address/gateway;
- forwarding mode;
- main/WAN endpoint exclusion;
- optional NAT mode derived from the live design.

The production-edge/RB5009 production deployment reads existing live values; the product must not invent subnet/interface/table names.

### 4.2. Environment schema

One versioned schema must cover:

- application/logging/lifecycle;
- interface/address/endpoint/MTU;
- full AWG3 J/S/H/I profile;
- Header Protection and Content Padding;
- timing ranges and ranged PersistentKeepalive;
- forwarding/NAT mode;
- health/status listener;
- Monitoring identity labels.

Unknown required variables and invalid ranges must fail closed. Environment parsing must be identical on all architectures.

Environment variables are bootstrap/deployment wiring only. Editable tunnel configuration is canonical in mounted persistent files:

- `/config/awg3.json` — non-secret configuration, mode `0600`;
- `/config/secrets.json` — private keys, PSK, HeaderProtectionKey and control credentials, mode `0600`.

No secret value may be present in image layers, OCI labels, App manifest, generated RouterOS installer, logs, status API or effective-config output.

### 4.3. Secrets schema

One schema, values supplied outside OCI image/manifests/install scripts:

- WireGuard/AWG private key;
- preshared key;
- HeaderProtectionKey;
- optional API authentication material.

Logs/API/UI/Monitoring must never reveal these values. Equality evidence uses non-reversible fingerprints.

### 4.4. Supervisor and configuration control plane

Один supervisor реализует:

- lifecycle `amneziawg-go` без systemd;
- lightweight health/status API;
- чтение и проверку common config/secrets schemas;
- masked effective configuration;
- key generation through local official tooling/library;
- atomic apply and controlled tunnel restart;
- on-demand configuration UI state machine.

Required supervisor modes:

- `run` — normal steady state; configuration UI/listener закрыт;
- `validate` — проверить candidate config/secrets без применения;
- `apply` — atomic apply через control API/CLI;
- `ui-open` — открыть configuration listener на ограниченный срок;
- `ui-close` — немедленно закрыть listener;
- `status` — machine-readable status without secrets.

On ARMv5 supervisor постоянно обслуживает только лёгкий health/status API. UI assets могут быть embedded в тот же binary, но UI routes/listener не активны в `run` mode.

On-demand lifecycle:

1. RouterOS adapter вызывает authenticated local control action по существующему veth через поддерживаемый RouterOS mechanism.
2. Supervisor открывает configuration listener только на management/veth address, никогда на tunnel/WAN address.
3. Listener имеет короткий configurable idle TTL и absolute maximum lifetime.
4. `Apply`, `Cancel`, explicit close или timeout закрывают listener и очищают in-memory candidate/secrets buffers.
5. Открытие/закрытие UI логируется без token, keys, parameter values и request bodies.

Конкретный RouterOS transport (`fetch`, temporary rule, local HTTP action, file trigger или другой mechanism) **не фиксируется архитектурой до hardware gate**. Implementation gate обязан выбрать минимальный поддерживаемый mechanism на фактической RouterOS version и доказать:

- authentication без раскрытия credential в command history/output;
- отсутствие постоянного configuration listener/dstnat;
- automatic cleanup после Apply/Cancel/timeout/reboot;
- предсказуемый repeated `ui-open` (idempotent refresh текущей session либо explicit conflict; выбранное поведение фиксируется API contract);
- работу `ui-close` и recovery после container/RouterOS restart.

ARM64 `/app` может постоянно публиковать UI URL, но использует тот же supervisor, routes, API, forms, schemas и apply engine. Разница — deployment policy `ui_mode=always` против ARMv5 `ui_mode=on_demand`, а не другой UI/codebase.

Configuration UI/API обязаны поддерживать:

- endpoint;
- tunnel addresses;
- AllowedIPs;
- MTU;
- peer private/public keys;
- PSK;
- HeaderProtectionKey;
- ContentPaddingAddition;
- J/S/H/I;
- all AWG3 timers and ranged PersistentKeepalive;
- key generation;
- validation;
- atomic apply;
- tunnel restart;
- masked effective configuration.

Atomic apply algorithm:

1. Принять candidate documents с size limits и authenticated request.
2. Записать temporary files **в том же `/config` filesystem** с mode `0600`.
3. `fsync` files; parse schema; validate cross-field rules; run AWG config parser/`showconf` dry validation.
4. Не изменяя active files, запустить candidate preflight и подготовить restart.
5. Выполнить same-filesystem atomic rename; `fsync` directory.
6. Перезапустить tunnel под supervisor и дождаться bounded readiness.
7. Если readiness failed, восстановить предыдущие in-memory/file contents атомарно в рамках транзакции и вернуть explicit failure; не оставлять partial pair `awg3.json`/`secrets.json`.

Поскольку обновляются два файла, supervisor должен применять их как одну versioned transaction: matching generation ID в обоих documents, staging directory/file pair и commit marker/current-generation pointer либо эквивалентный crash-safe mechanism. Два независимых rename без generation binding недостаточны.

Secret handling:

- file permissions и expected owner проверяются на startup и apply; mode шире `0600` либо wrong owner — fail closed;
- JSON request bodies/secrets не попадают в access/error logs;
- effective config возвращает mask и optional non-reversible fingerprint;
- temporary files удаляются/замещаются при success/failure/startup recovery;
- key generation возвращает secret только в authenticated apply session и не сохраняет его вне transaction.

Startup recovery:

- обнаружить incomplete staging/generation/commit marker;
- выбрать только последнюю полностью committed согласованную пару config/secrets;
- никогда не смешивать documents разных generation IDs;
- удалить либо безопасно изолировать incomplete staging без его применения;
- перед запуском tunnel повторить permission/owner/schema/cross-field/AWG parser validation;
- configuration listener после supervisor/container/RouterOS restart всегда закрыт на ARMv5 независимо от pre-crash session state.

Key generation lifetime:

- generated secret существует только в текущей authenticated configuration session;
- до Apply не попадает в canonical files;
- Cancel/timeout/close удаляют reference и очищают buffers настолько, насколько практически возможно в Go runtime;
- status/effective config/logging никогда не возвращают generated secret.

### 4.5. Health/status API and UI

The same versioned response must expose:

- product/version/source revision/platform;
- container/process uptime;
- interface state/address/MTU;
- endpoint and latest handshake;
- RX/TX counters;
- enabled AWG3 feature names and safe parameter metadata;
- route/forwarding/endpoint-exclusion status;
- egress/path probe state;
- validation errors and degraded reason.

Web UI is a view over the same API, not a separate status implementation.

На ARMv5 постоянно доступен только lightweight status surface. Configuration routes возвращают closed/not-active, пока RouterOS control action не открыл timed session. На ARM64 App UI URL может быть постоянным. JSON formats, endpoints and semantics identical.

### 4.6. Monitoring 101

One projection contract for every deployment:

- `transport_family=awg3`;
- `runtime_provider=container/amneziawg-go`;
- stable logical edge/interface identity;
- tunnel addressing;
- handshake and RX/TX;
- process/container state;
- safe AWG3 profile/features;
- egress/path probes.

The production mutation updates the existing `RB5009 → production-edge` edge. ARMv5 hardware acceptance must use an isolated test identity and must be removed after the test.

## 5. Deployment adapters

### 5.1. ARM64 RouterOS custom App

The adapter may define:

- pinned arm64 image digest;
- required TUN hardware device;
- veth/network/mount wiring;
- references to common environment/secrets inputs;
- lifecycle metadata and UI link.

It may not own routes, subnets or AWG3 parameter defaults that are absent from the common schema.

### 5.2. ARMv5 RouterOS `/container`

The generated installer must:

- reject non-ARM/unsupported RouterOS targets;
- use the pinned `linux/arm/v5` manifest digest;
- validate container/device-mode/storage/TUN prerequisites;
- create namespaced veth/mount/env/container objects;
- be idempotent and collision-safe;
- provide precise cleanup for objects it created;
- never embed secrets;
- avoid all production route/failover changes during hAP acceptance.
- создавать RouterOS-side on-demand UI open/close actions только на management veth;
- не публиковать постоянный configuration dstnat/listener;
- передавать UI TTL/control bootstrap без secret values в script output;
- обеспечивать cleanup временных firewall/control artifacts после Apply/Cancel/timeout.

Выбор конкретного RouterOS control mechanism является implementation/hardware gate, а не заранее утверждённой частью installer design.

RouterOS `/app` is not an ARM32 delivery mechanism.

## 6. CI and release gates

A release is invalid unless all pass:

1. One source revision and one semantic version.
2. OCI index contains exactly required `amd64`, `arm64`, `arm/v5` targets.
3. Per-platform ABI/dependency inspection.
4. Schema hashes identical across images.
5. Config render and API/UI contract tests identical.
6. AWG3 parser/showconf/dump round-trip on all targets.
7. amd64↔arm64, amd64↔arm/v5 and arm64↔arm/v5 interop where hardware permits.
8. RB5009 custom App hardware acceptance.
9. hAP ac² generated `/container` hardware acceptance.
10. Reboot/lifecycle/resource/cleanup evidence for both RouterOS devices.
11. ARMv5 steady-state process/RSS proof: только `amneziawg-go` + supervisor.
12. On-demand UI open/apply/cancel/timeout/close tests and zero listener after close.
13. Crash-consistent two-file atomic configuration transaction tests.
14. Secret redaction and `0600` permission negative tests.
15. Fault injection после каждого шага transaction: first/second staging, schema validation, AWG parser validation, fsync, commit marker, pre-restart, readiness и post-readiness/pre-response.
16. UI lifecycle: unauthenticated reject, management-only bind, repeated open, Apply/Cancel/explicit/idle/absolute/restart/reboot close, zero remaining socket.

## 7. Scope-safe hardware acceptance

RB5009 acceptance is a prerequisite to the production production-edge mutation and may transition into the staged candidate after all stop-gates.

hAP ac² acceptance is isolated:

- no production production-edge/KVN/FBSH endpoint or policy-table mutation;
- no failover/monitoring production-edge changes;
- unique temporary object prefix;
- local/lab AWG3 peer only;
- final proof of zero remaining acceptance artifacts.
