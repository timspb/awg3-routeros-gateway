# Handoff младшей модели: AWG3 RouterOS Gateway и мутация production-edge ↔ RB5009

Дата: 2026-08-02  
Роль исполнителя: выполнить изменение и собрать доказательства  
Роль принимающей модели: проверить evidence, diff scope, acceptance и cleanup  
Execution scope: production-edge, RB5009/192.168.1.1 и существующая запись Monitoring 101 `RB5009 → production-edge`; hAP ac² участвует только как ARMv5 hardware-acceptance target

Продуктовый scope: единый `AWG3 RouterOS Gateway` с OCI targets `linux/arm64`, `linux/arm/v5`, `linux/amd64`.

## 0. Единый продукт — обязательный инвариант

Нельзя создавать отдельные ARM/ARM64 codebases, forks, entrypoints, environment schemas или feature profiles.

Один source tree обязан выпускать один multi-architecture OCI product:

| OCI platform | Назначение | Deployment adapter |
|---|---|---|
| `linux/arm64` | RB5009 production | RouterOS custom `/app` |
| `linux/arm/v5` | hAP ac² hardware acceptance и ARM32 RouterOS | ordinary `/container` + generated `.rsc` installer |
| `linux/amd64` | CI, interop и лаборатория | ordinary OCI runtime/container |

Во всех targets одинаковы:

- application source and version;
- AWG3 feature set;
- interface naming and tunnel-address model;
- environment and secrets schemas;
- entrypoint behavior;
- health/status API and web UI;
- Monitoring 101 contract;
- validation/error semantics;
- OCI labels and build provenance.

ARMv5 steady state имеет отдельный resource constraint, но не отдельную application model: постоянно работают только `amneziawg-go` и один статический Go supervisor. Health/status API встроен в supervisor; configuration UI включается on-demand.

Различаются только deployment adapters:

- `deploy/routeros-app-arm64/` — manifest/yaml/metadata для `/app`;
- `deploy/routeros-container-armv5/` — generated RouterOS installer для `/container`;
- `deploy/oci-amd64/` — CI/lab invocation only.

`/app` для ARM32 не проектировать и не эмулировать.

## 1. Решение и границы полномочий

Это production-изменение разрешено только как последовательность gated phases.

Исполнитель имеет право:

- выполнять read-only аудит production-edge, RB5009 и relevant Monitoring 101 runtime;
- собрать official AWG3 из закреплённых commit SHA;
- выполнить краткоживущий capability probe RouterOS Container;
- выполнить отдельный non-production hardware acceptance на RB5009 arm64 и hAP ac² ARMv5;
- после PASS всех preflight gates провести coordinated mutation production-edge и RB5009;
- изменить только production-edge-specific routes/rules/container/monitoring provider;
- после полного acceptance удалить старые production-edge AWG2 artifacts.

Исполнитель не имеет права:

- менять KVN, TEKO, VLESS/Xray, FBSH, China, AGH, TeleMT, failover selector или другие nodes;
- включать hAP ac² в production-edge routing/migration; на нём разрешён только isolated product acceptance;
- собирать ARM32 как generic `linux/arm` без `GOARM=5` и OCI variant `v5`;
- добавлять на ARMv5 Python, Node.js, nginx, systemd, database, отдельный UI daemon или тяжёлый framework;
- хранить secrets в image/App manifest/installer/env report либо логировать request bodies;
- использовать `latest`, непроверенный сторонний image или неприкреплённый source tree;
- печатать private keys, PSK, HeaderProtectionKey, tokens или passwords;
- продолжать после failed gate;
- удалять старый рабочий контур до доказанного запуска candidate и наличия точного восстановимого AWG2 source/digest;
- считать наличие параметра в конфиге доказательством его работы;
- делать reboot production-edge без отдельной оценки безопасности текущего окна;
- коммитить production secrets или live configs с ключами.

## 2. Обязательное уточнение исходного задания

Требования «никаких backup/rollback artifacts» и «полностью удалить старую реализацию» не отменяют rollback requirement.

Разрешённая модель rollback:

1. Не создавать новые `.bak/.old/tar/zip` и rollback units.
2. До cutover доказать, что exact AWG2 восстановим из уже существующего golden snapshot, immutable image/source и сохранённых secret sources.
3. До финального acceptance старый stopped production rootfs/object не считается backup-копией и не удаляется.
4. После PASS всего acceptance старый object/rootfs удаляется; дальнейший rollback должен быть воспроизводим из зафиксированного AWG2 digest/source, а не из складированной копии.
5. Если exact AWG2 восстановить нельзя — **STOP, NO-GO**. Нельзя превращать миграцию в необратимую.

## 3. Источники истины

Читать до любых действий:

1. `E:\CODEX\My_Local\OPS_PLAYBOOK.md`
2. `E:\CODEX\My_Local\nodes.json`
3. `E:\CODEX\My_Local\ACCESS_INDEX.md`
4. `E:\CODEX\My_Local\MASTER_NETWORK_MAP.md`
5. `E:\CODEX\My_Local\AWG2_GOLDEN_CONFIGURATION.md`
6. `E:\CODEX\My_Local\AWG2_GOLDEN_STATE_2026-07-04.md`
7. `E:\CODEX\My_Local\AWG3_ROUTEROS_GATEWAY\docs\AWG3_PRODUCTION_EDGE_RB5009_MUTATION_RESEARCH_2026-08-01.md`
8. `E:\CODEX\My_Local\AWG3_ROUTEROS_GATEWAY\PRODUCT_ARCHITECTURE.md`

Документы и старые snapshots — orientation only. Перед mutation каждое имя, address, rule, route и lifecycle owner подтвердить live.

## 4. Главные stop-gates

### Gate A — scope and recovery

PASS только если:

- exact targets однозначно сопоставлены production-edge и RB5009;
- есть current live export/report без secret values;
- известен exact AWG2 image/binary/config source для rollback;
- операторский доступ к обоим узлам независим от production-edge tunnel;
- изменение production-edge не оборвёт текущую SSH/control session;
- KVN/FBSH/direct WAN доступны как control path, но не будут использоваться как скрытая acceptance substitution.

Иначе STOP.

### Gate B — official source pin

Не брать автоматически самый новый tag.

1. Проверить official repos и release/tag history на момент исполнения.
2. Выбрать один согласованный commit `amneziawg-go` и один `amneziawg-tools`.
3. Записать repo URL, tag, full commit SHA, source archive SHA-256, build container/toolchain digest.
4. Обе архитектуры собрать из того же source pair.
5. Если после исследованного `amneziawg-go v3.0.3` появился новый fix, не обновляться молча: описать fix и обоснование выбора.
6. Один release version должен породить три OCI manifests/images из одного Dockerfile/build graph.
7. ARM32 Go build обязан иметь `GOOS=linux GOARCH=arm GOARM=5`; OCI descriptor — `os=linux`, `architecture=arm`, `variant=v5`.
8. C binaries и runtime dependencies (`awg`, libc, `ip`, firewall helper, init/health binary) проверить как ARM EABI, совместимый с ARMv5; одного правильного `amneziawg-go` недостаточно.
9. Multi-arch index не должен направлять `linux/arm/v5` на arm/v7 или arm64 layer.

Иначе STOP.

### Gate C — RouterOS `/dev/net/tun`

Это главный capability gate.

Read-only сначала:

- `/system resource print`
- `/system package print detail`
- `/system device-mode print`
- `/container config print detail`
- `/container print detail`
- `/container mounts print detail`
- `/interface veth print detail`
- `/disk print detail`

Probe должен быть краткоживущим и не создавать transport до production-edge:

1. Передать реальный host device через RouterOS container `devices=` mechanism.
2. Внутри probe подтвердить character device `/dev/net/tun` и успешный `TUNSETIFF`.
3. Создать тестовый TUN с уникальным временным именем без endpoint/route в production tables.
4. Подтвердить `CAP_NET_ADMIN` фактической операцией, не чтением capability string.
5. Подтвердить IP forwarding между existing veth namespace и test TUN локальными packets/counters.
6. Запустить pinned ARM64 `amneziawg-go` в foreground и подтвердить создание/удаление interface.
7. Удалить probe container/rootfs/import artifacts/test routes/interfaces.

PASS evidence:

- RouterOS version/architecture;
- exact `devices=` mapping;
- `ls -l /dev/net/tun` без чувствительных данных;
- successful interface create/delete;
- packet counters in both directions;
- zero remaining probe artifacts.

Если device отсутствует, `TUNSETIFF` возвращает permission/unsupported, forwarding не работает или требуется изменение device-mode с physical confirmation — STOP до разрушения AWG2.

### Gate D — unified multi-architecture build and parser

PASS только если для amd64, arm64 и arm/v5:

- binaries report expected pinned version/build metadata;
- image architecture совпадает target;
- runtime image не содержит package manager/build toolchain;
- `awg setconf → awg showconf` сохраняет каждый v3 field;
- `awg show/dump` не теряет ranges;
- проверена известная зона `MaxHandshakeAttempts/max-handshake-attemps`;
- invalid ranges и `HeaderProtectionKey` при `S1-S4 < 12` завершаются fail-fast;
- private/header keys маскируются в evidence.
- image labels/source revision/version совпадают на всех architectures;
- environment-schema и secrets-schema hashes совпадают;
- health/status API и web UI contract tests проходят одинаково;
- AWG3 profile renderer выдаёт семантически одинаковый config;
- `docker buildx imagetools inspect` или эквивалент подтверждает ровно обязательные platforms;
- ARMv5 artifact проверен не только `file/readelf`, но и запуском на реальном hAP ac².
- ARMv5 steady-state process tree содержит ровно supervisor и `amneziawg-go`;
- supervisor статический `CGO_ENABLED=0`, `GOARCH=arm`, `GOARM=5`;
- configuration files `/config/awg3.json` и `/config/secrets.json` имеют mode `0600`;
- configuration UI closed by default на ARMv5 и не имеет listening socket;
- Apply/Cancel/idle-timeout закрывают UI listener;
- config/secrets применяются одной crash-safe generation transaction, не двумя независимыми unsafe writes.

Иначе STOP.

### Gate E — route design and no recursion

До cutover подготовить и доказать статически:

- veth RouterOS-facing IP и container IP остаются прежними;
- production-edge routing table остаётся прежней;
- default/required routes в этой table указывают на existing container gateway;
- public endpoint `213.176.116.165/32` никогда не lookup через production-edge table;
- outer AWG3 packets выходят main/WAN;
- production-edge остаётся WAN-only, не LTE и не IPTV;
- route/rule ordering не меняет KVN/FBSH/direct ISP paths;
- endpoint exception обновляется при WAN DHCP change либо использует доказанный динамический mechanism; одноразовый `/32` через текущий DHCP gateway не является durable fix.

Иначе STOP.

## 5. План исполнения

### Phase 1 — live inventory

Собрать один рабочий Markdown evidence report, без secret values и backup archives.

production-edge:

- OS/kernel/architecture;
- `ip -br link/address`, routes, rules;
- nftables/iptables/UFW effective state;
- UDP/TCP listeners с PID/process/unit ownership;
- AWG2 proxy unit, backend WG interface/config metadata;
- tunnel IP/subnet/MTU/peer public identities/AllowedIPs;
- WARP/NAT/forwarding semantics;
- VLESS/Xray/AGH units, listeners, health baseline;
- disk/RAM/CPU baseline.

RB5009:

- RouterOS version/architecture/packages/device-mode;
- production-edge WG interface, peer, address, routes/rules/marks/comments;
- exact ownership: убедиться, что interface не имеет чужих peers;
- container object/env/mounts/veth/bridge/root-dir/layer-dir;
- `Container_Storage_Guard`, WAN DHCP hook, `RU_Uplink_Control` references;
- `usb2-part1` storage/free space; `usb-1` не считать persistent;
- KVN/FBSH/direct path baselines;
- current Monitoring 101 logical edge/provider fields.

Deliverable checkpoint: таблица `object → current value → planned action → rollback source`.

### Phase 2 — reproducible build

Собрать вне production runtime:

- production-edge/CI amd64 binary/runtime;
- RB5009 arm64 OCI image;
- hAP ac² compatible `linux/arm/v5` OCI image;
- один multi-architecture OCI index, закреплённый digest;
- ARM64 RouterOS custom App adapter;
- generated ARMv5 `/container` installer script.

Минимальный runtime:

- `amneziawg-go`;
- matched `awg`/tools;
- `iproute2`;
- минимальный init/entrypoint/health check;
- firewall/NAT tooling только если live design докажет его необходимость.

ARMv5 runtime дополнительно:

- PID 1: единый статический Go supervisor;
- child: `amneziawg-go`;
- health/status API встроен в supervisor;
- embedded static UI допустим только как on-demand routes того же process;
- никаких Python/Node/nginx/systemd/database/separate UI process;
- persistent mount `/config` с `awg3.json` и `secrets.json`, оба `0600`;
- secrets отсутствуют во всех image layers и build logs.

Build acceptance:

- pinned SHAs and hashes;
- SBOM/file listing;
- no package manager/compiler/source in runtime;
- deterministic config render with placeholders;
- container exits non-zero on missing TUN, invalid config or unknown required parameter.
- ARM64 App и ARMv5 installer передают одну и ту же environment/secrets model;
- adapters не содержат собственного AWG3 profile logic;
- generated installer идемпотентен, проверяет architecture/RouterOS/container capability/storage и не удаляет чужие objects;
- web UI/status endpoints возвращают одну schema на всех platforms.
- supervisor одинаково валидирует/маскирует/применяет config на всех platforms;
- ARMv5 UI activation не создаёт третий process;
- measured steady-state RSS/PSS и per-process table приложены к evidence.

### Phase 2A — deployment adapters

ARM64 `/app` package:

- custom App manifest указывает только pinned `linux/arm64` product image/digest;
- декларирует required hardware device для TUN, veth/network, mounts и environment variables;
- не создаёт новую routing table или tunnel subnet;
- не хранит secrets в manifest;
- после deploy container/lifecycle ownership однозначно виден через `/app` и Monitoring 101.

ARMv5 `/container` installer:

- generated `.rsc` использует тот же image repository/version и pinned arm/v5 digest;
- проверяет `architecture-name`, RouterOS version, container package/device-mode, storage и отсутствие name/address collision;
- создаёт ordinary `/container`, veth/mount/env wiring только для isolated acceptance namespace;
- имеет generated uninstall/cleanup section, удаляющий только objects с уникальным acceptance prefix;
- не изменяет production hAP routes, failover, KVN/production-edge/WG-VPN paths;
- не содержит secret values: secrets вводятся/подключаются отдельно по общей schema.
- создаёт persistent `/config` mount и проверяет права `0600`;
- создаёт RouterOS control actions для authenticated `ui-open`/`ui-close` через management veth;
- configuration listener никогда не публикуется на WAN/tunnel и отсутствует в normal mode;
- временные RouterOS firewall/listener artifacts автоматически удаляются после Apply/Cancel/timeout.

Не фиксировать в design конкретный RouterOS control transport (`fetch`, temporary firewall rule, local HTTP, file trigger и т. п.) до проверки на live-equivalent RouterOS version. Implementation gate выбирает минимальный supported mechanism и доказывает authentication, отсутствие credential в history/output, cleanup и reboot behavior.

### Phase 2B — обязательный hardware acceptance

RB5009 arm64:

- проверить custom App install/start/stop/update/remove lifecycle;
- TUN device, forwarding, AWG3 process, health/status/web UI;
- reboot persistence и resource usage;
- этот acceptance может быть совмещён с staged production candidate только после Gates A–E.

hAP ac² ARMv5:

- только ordinary `/container`; `/app` не использовать;
- подтвердить actual CPU/ABI execution всех runtime binaries;
- isolated TUN/veth forwarding и lab AWG3 interop, не production-edge path;
- проверить install/start/stop/reboot/uninstall generated script;
- подтвердить cleanup до нуля acceptance artifacts;
- не менять существующие hAP production tunnels/routes/failover.
- подтвердить steady-state processes: только supervisor и `amneziawg-go`;
- измерить RSS/PSS supervisor, AWG и общий container memory;
- открыть UI через generated RouterOS control action, выполнить Validate, Cancel и убедиться в закрытии listener;
- повторить с Apply и idle timeout;
- проверить generation-consistent `awg3.json`/`secrets.json`, mode `0600`, masked effective config и отсутствие secrets в logs;
- проверить expected owner обоих файлов; wrong owner и `0644`/`0640` должны fail closed;
- аварийно прервать apply между staging/commit/restart и доказать startup recovery к одной целой generation.

Hardware acceptance считается PASS только при отдельных evidence tables для RB5009 и hAP ac². Успех arm64 не заменяет ARMv5 test и наоборот.

### Phase 3 — final profile

Начальная рекомендация из исследования, корректировать только по тестам:

- existing J/S/H/I profile как baseline;
- `HeaderProtectionKey`: отдельный новый shared key;
- `ContentPaddingAddition = 0-32` на обеих сторонах;
- `RekeyAfterTime = 110-130`;
- `RekeyTimeout = 4-6`;
- `RejectAfterTime = 175-190`;
- `KeepaliveTimeout = 9-11`;
- `MaxHandshakeAttempts = 16-20`;
- client `PersistentKeepalive = 23-27`, server off;
- MTU 1280;
- каждый `S1-S4 >= 12`.

Не копировать secret values в report. Для equality давать SHA-256/HMAC fingerprints, которые не позволяют восстановить secret.

До production провести isolated interop test `arm64 client ↔ amd64 server` с теми же binaries/profile и доказать handshake, traffic, showconf/dump, padding/timing behavior.

### Phase 3A — configuration control plane acceptance

Общий API/UI contract тестируется на amd64, arm64 и arm/v5:

1. `run` поднимает tunnel и status API, но на ARMv5 не открывает configuration UI/listener.
2. Authenticated RouterOS control action открывает UI только на management veth и на ограниченный TTL.
3. UI редактирует endpoint, tunnel addressing, AllowedIPs, MTU, keys, PSK, HeaderProtectionKey, ContentPadding, J/S/H/I и все timers.
4. Key generation не пишет secrets в logs/status/effective config.
5. Validation отклоняет schema/cross-field/AWG parser errors до active change.
6. Apply staging использует тот же filesystem, mode `0600`, generation binding и atomic commit.
7. Successful Apply перезапускает tunnel и закрывает UI.
8. Failed Apply возвращает предыдущую complete generation и рабочий tunnel.
9. Cancel и idle/absolute timeout закрывают listener и очищают candidate buffers.
10. Effective config маскирует secrets, но показывает safe fingerprints и реально активные значения.
11. Explicit `ui-close`, supervisor/container restart и RouterOS reboot закрывают listener.
12. Unauthenticated open rejected; repeated authenticated open имеет документированное idempotent/conflict behavior.
13. Listener bind только management-veth; нет WAN/tunnel/public/dstnat exposure.
14. Generated secret живёт только в session, до Apply не сохраняется, после Cancel/timeout очищается настолько, насколько практически возможно в Go.

Fault-injection обязателен после каждой стадии: staging первого файла, staging второго, schema validation, AWG parser validation, fsync, commit marker/current-generation update, между commit и restart, во время readiness, после readiness до API response. После restart/reboot выбирается ровно старая либо новая complete generation.

ARM64 может иметь `ui_mode=always`; ARMv5 только `ui_mode=on_demand`. Других различий API/schema/UI logic быть не должно.

### Phase 4 — staged production-edge cutover

1. Проверить active control path и baseline VLESS/Xray/AGH непосредственно перед окном.
2. Подготовить candidate files/config, не занимая public UDP/443 вторым permanent service.
3. Остановить только old `awg-proxy-production-edge`/backend данного production-edge contour.
4. Не удалять old artifacts на этом этапе.
5. Запустить official AWG3 endpoint под согласованным lifecycle owner и прежним logical interface/tunnel IP/UDP port.
6. Проверить one-owner UDP listener, interface/address/routes/NAT и отсутствие изменений VLESS/Xray/AGH.
7. Перейти к RB5009 немедленно.

Если любой check не проходит — восстановить production-edge AWG2 из preflight recovery source и STOP.

### Phase 5 — staged RB5009 cutover

1. Остановить old `awg-production-edge-fixed` proxy container.
2. Не удалять его rootfs/object до working AWG3 payload acceptance.
3. Развернуть new ARM64 gateway через подготовленный custom App adapter на existing RouterOS-facing network contract; App не должен автоматически создавать конфликтующую адресацию/маршруты.
4. Внутри container поднять official AWG3 interface с прежним logical tunnel name/IP/MTU.
5. Включить IP forwarding; применять NAT внутри container только если inventory доказал необходимость.
6. Переключить existing production-edge routing table next-hop с RouterOS WG на existing container gateway.
7. Создать durable main/WAN endpoint exclusion без LTE/IPTV fallback.
8. Только после fresh AWG3 handshake и bidirectional payload удалить production-edge native WG peer/interface/address из RouterOS.
9. Адаптировать `Container_Storage_Guard` к одному новому container owner, не трогая KVN.

До удаления RouterOS WG обязательно проверить, что interface не имеет других peers/addresses/dependencies.

### Phase 6 — Monitoring 101

Только после transport PASS:

- сохранить тот же logical edge `RB5009 → production-edge`;
- заменить provider на container/`amneziawg-go`;
- установить `transport_family=awg3`;
- сохранить logical interface identity/tunnel addressing;
- брать latest handshake/RX/TX из AWG3 runtime, не из удалённого RouterOS WG;
- добавить process/container, endpoint, profile-feature, egress/path probes;
- удалить old AWG2/proxy-specific provider/evidence;
- не создавать второй edge и не менять остальные nodes.

Monitoring PASS требует fresh runtime timestamp и корреляцию с live `awg show`, а не только metadata config.

### Phase 7 — acceptance before cleanup

Минимальная матрица:

| Group | Required evidence |
|---|---|
| Transport | interface UP, fresh handshake, RX/TX growth, tunnel ping |
| Routing | lookup from relevant sources, production-edge table next-hop, no recursion/leak |
| Outer path | pcap/conntrack shows UDP/443 through main/WAN only |
| Egress | expected public IP, TCP, UDP, DNS, upload/download |
| MTU | PMTU, large payload, no blackhole, no fragment at max J/padding |
| AWG3 | Header Protection, content padding both ways, J/S/H/I, timer ranges observed |
| Lifecycle | independent service/container restart and automatic recovery |
| Stability | 30 min idle + resume, sustained load, resource samples |
| RB reboot | container/TUN/routes/handshake/egress restored |
| ARM64 product | custom App lifecycle, status/UI/API, reboot, resource usage |
| ARMv5 product | hAP ac² ordinary-container install, runtime, reboot, uninstall/clean state |
| ARMv5 memory | only two steady processes; measured RSS/PSS within declared budget |
| Config control | open/apply/cancel/timeout, atomic rollback, 0600, redaction |
| Config security | wrong owner/mode fail closed; no secrets in image/metadata/installer/logs/status/temp files |
| production-edge restart | service restores without touching VLESS/Xray |
| Unchanged | KVN, FBSH, direct ISP, TEKO/VLESS, Xray, AGH |
| Monitoring | exactly one production-edge edge, fresh AWG3 provider data |

production-edge reboot выполнить только если control path, window и service boot persistence позволяют безопасно это сделать. Если reboot небезопасен, acceptance остаётся **incomplete**, а не PASS.

### Phase 8 — destructive cleanup

Cleanup разрешён только после PASS Phase 7 и отдельного финального pre-cleanup diff.

Удалить только точно сопоставленные production-edge-contour artifacts:

- old `awg-proxy` binary/service/config/backend WG interface;
- old RB5009 AWG2 container/rootfs/env/mounts;
- old RouterOS production-edge WG peer/interface/address;
- transient probe/build/import artifacts;
- old monitoring provider/evidence;
- unused old keys после доказательства, что они не принадлежат другим peers.

Перед каждым remove выполнить read-only dependency search. Если найден внешний consumer — не удалять, STOP и описать зависимость.

После cleanup повторить всю unchanged-contour и core transport проверку. Final state должен содержать один production-edge transport, один container owner, один Monitoring edge и ноль disabled legacy objects.

## 6. Rollback checkpoints

| Checkpoint | Действие rollback |
|---|---|
| capability/build failed | удалить только probe/build artifacts; production untouched |
| production-edge candidate failed before RB cutover | stop candidate; restore old production-edge proxy/backend |
| RB candidate failed before RouterOS WG deletion | stop candidate; start old container; old RouterOS WG remains |
| failed after WG deletion but before cleanup | restore RouterOS WG from live inventory, start old container, restore production-edge proxy |
| failed after cleanup | reconstruct exact AWG2 from pre-proven immutable source/digests; this path must be rehearsed before cleanup |

Rollback trigger: no fresh handshake within two rekey windows, TX without RX, wrong WAN source, recursion, route diff outside scope, VLESS/Xray/AGH regression, KVN/FBSH regression, parser silent-ignore, fragmentation, resource saturation или failed restart.

## 7. Evidence format исполнителя

Один итоговый отчёт должен содержать:

1. Exact before-state и время снятия.
2. Source repos/tags/full SHAs/hashes/toolchain.
3. Capability gate commands и результаты.
4. Build artifacts/digests/architectures/SBOM summary.
5. Before/after object mapping.
6. Final topology/interface names/addresses/routes.
7. Полный AWG3 profile без secret values.
8. Каждый change на production-edge, RB5009 и Monitoring 101.
9. Каждый deleted artifact с доказательством принадлежности production-edge contour.
10. Таблицу acceptance `test → command/probe → timestamp → result → evidence excerpt`.
11. CPU/RAM/RTT/loss/throughput before/after.
12. Unchanged-contour evidence.
13. Remaining limitations; никакого скрытого `PASS with caveats`.
14. Multi-arch index digest и per-platform manifest/layer digests.
15. Отдельные RB5009 arm64 и hAP ac² ARMv5 hardware-acceptance tables.
16. ARMv5 process tree and memory measurements in idle, UI-open and load states.
17. Config transaction/crash-recovery and secret-redaction evidence.
18. Exact RouterOS control mechanism gate result without prematurely assuming its implementation.

Не использовать формулировки `работает`, `применилось`, `не затронуто` без command output, timestamp и target identity.

## 8. Инструкция младшей модели — копировать целиком

```text
Ты исполнитель единого multi-architecture продукта AWG3 RouterOS Gateway и production-мутации production-edge ↔ RB5009 на официальный AWG3.

Работай строго по E:\CODEX\My_Local\AWG3_ROUTEROS_GATEWAY\docs\AWG3_MUTATION_JUNIOR_EXECUTION_HANDOFF_2026-08-02.md, PRODUCT_ARCHITECTURE.md и исходному production-промпту. Handoff определяет stop-gates, порядок, rollback и evidence; исходный промпт определяет конечную архитектуру.

Правила:
1. Начни только с live read-only inventory. Старые docs не являются live truth.
2. Не показывай и не коммить private keys, PSK, HeaderProtectionKey, passwords или tokens.
3. Не трогай KVN, TEKO, VLESS/Xray, AGH, FBSH, China, TeleMT, failover и чужие nodes.
4. До любой mutation пройди Gates A-E. Failed gate означает немедленный STOP с точным blocker evidence.
5. Главный Gate C: реальный /dev/net/tun + TUNSETIFF + CAP_NET_ADMIN + veth↔TUN forwarding + pinned ARM64 amneziawg-go в RouterOS Container. Не имитируй его чтением документации.
6. Не удаляй старый AWG2 до working AWG3 acceptance. Не создавай .bak/.old/archive/rollback units. До cutover докажи exact recovery из существующего immutable source/digest.
7. Pin official amneziawg-go и amneziawg-tools full commit SHA. production-edge и ARM64 image собирай из одного source pair. Не используй latest или third-party image.
8. Выполни production-edge cutover, затем RB5009 cutover, затем Monitoring 101 provider update. Сохрани existing veth/link addressing/tunnel subnet/tunnel IP/logical production-edge edge/table/comments/operational intent.
9. Public production-edge UDP endpoint обязан идти main/WAN, никогда через production-edge table, LTE или IPTV. Докажи route lookup и pcap/conntrack.
10. Подтверди каждый AWG3 field через setconf/showconf/dump и поведение трафика. Учти spelling/round-trip risk MaxHandshakeAttempts.
11. RouterOS native production-edge WG и old proxy удаляй только после fresh AWG3 handshake, bidirectional payload и pre-cleanup acceptance. Перед remove ищи foreign dependencies.
12. После cleanup повтори transport, reboot/restart, MTU, idle/load, unchanged KVN/FBSH/direct/VLESS/Xray/AGH и Monitoring checks.
13. При любом rollback trigger немедленно восстанови последний доказанный working state и остановись. Не маскируй failure переключением failover.
14. Веди один evidence report с timestamps и точными command outputs без secrets. Не объявляй PASS по конфигу без runtime proof.
15. Выпускай один OCI product для linux/arm64, linux/arm/v5 и linux/amd64. ARM32: GOARCH=arm, GOARM=5, OCI linux/arm/v5. Проверь архитектуру всех runtime dependencies.
16. Не создавай отдельные ARM/ARM64 codebases или schemas. Различаются только adapters: custom /app для arm64 и generated ordinary /container installer для arm/v5.
17. Проведи отдельный hardware acceptance на RB5009 arm64 и hAP ac² ARMv5. hAP используется только для isolated acceptance; его production routing/tunnels/failover не меняй.
18. На ARMv5 постоянно допускаются только amneziawg-go и один статический Go supervisor. Health/status API и on-demand UI реализуй внутри supervisor. Python, Node.js, nginx, systemd, database, отдельный UI process и тяжёлые frameworks запрещены.
19. Храни common configuration только в /config/awg3.json и /config/secrets.json с mode 0600. Secrets не должны попадать в image, manifests, installer, logs, status или masked effective config.
20. В normal ARMv5 run mode configuration listener закрыт. Открывай его только authenticated RouterOS control action; Apply, Cancel, idle timeout и absolute timeout обязаны закрыть listener.
21. Apply реализуй как crash-safe versioned transaction обоих JSON: same-filesystem staging, validation, generation binding, atomic commit, tunnel restart/readiness и automatic restore прежней complete generation при failure.
22. Не утверждай заранее конкретный RouterOS UI-control transport. Выбери его только после implementation gate на целевой RouterOS version; докажи authentication, отсутствие credential в history/output, automatic cleanup и reboot behavior.
23. UI listener обязан закрываться после Apply, Cancel, explicit close, idle/absolute timeout, supervisor/container restart и RouterOS reboot. Bind только management veth; никаких WAN/tunnel/public/dstnat surfaces.
24. Проведи fault injection после каждой стадии config transaction и докажи startup recovery только к одной complete generation.

Не запрашивай дополнительное согласование после PASS всех gates. Но ни один failed или incomplete gate обходить нельзя.
```

## 9. Чек-лист приёмки старшей моделью

Работа не принимается, если отсутствует хотя бы одно:

- live before-state, а не только документы;
- доказанный TUN capability на реальном RB5009;
- full official source SHAs и artifact hashes;
- matched amd64/arm64 versions;
- один multi-arch OCI index с `linux/amd64`, `linux/arm64`, `linux/arm/v5` и per-platform digests;
- `GOARM=5` и ABI/runtime proof для каждого ARMv5 binary/dependency;
- один environment/secrets/status/UI/Monitoring contract для всех targets;
- ARM64 custom App package и ARMv5 generated `/container` installer;
- отдельный hardware acceptance на RB5009 и hAP ac²;
- ARMv5 steady-state process/RSS proof и отсутствие запрещённых runtime components;
- common config/API/UI contract и persistent files `/config/awg3.json`, `/config/secrets.json` mode `0600`;
- on-demand UI lifecycle evidence for Apply/Cancel/timeout;
- atomic two-file transaction, crash recovery and secret-redaction tests;
- wrong permissions/owner fail-closed tests and absence of secrets in image/OCI metadata/installer/logs/status/temp files;
- full UI listener lifecycle including unauthenticated/repeated open, restart and reboot;
- parser round-trip каждого AWG3 parameter;
- no-recursion/main-WAN proof;
- fresh handshake/counter growth/payload;
- MTU/no-fragment proof;
- restart + RB reboot recovery;
- unchanged KVN/FBSH/direct/VLESS/Xray/AGH evidence;
- exactly one Monitoring production-edge edge with fresh runtime;
- dependency-checked cleanup;
- final filesystem/container/service inventory;
- truthful limitations and exact rollback result if one occurred.

Автоматический reject:

- secret leakage;
- использование third-party image/`latest`;
- destructive cleanup до acceptance;
- static DHCP `/32` workaround без renewal mechanism;
- production-edge outer path через LTE/IPTV;
- изменение KVN/failover/TEKO/VLESS/Xray/AGH;
- два постоянных production-edge tunnels/containers/Monitoring records;
- `PASS` только на основании config presence;
- reboot test пропущен, но отмечен PASS;
- старый disabled WG/container оставлен после заявленного success.
