# AWG3: исследование мутации production-edge + RB5009

Дата исследования: 2026-08-01  
Статус: research/design only; production не изменён  
Scope: только `RB5009 / 192.168.1.1` и `production-edge / 213.176.116.165`  
За пределами scope: TEKO, KVN, China, FBSH, TeleMT, monitoring 101, policy routing, failover

## 1. Итоговый вердикт

### 1.1. Короткий ответ

**GO для дальнейшего лабораторного прототипа; NO-GO для production-мутации сейчас.**

Причины NO-GO:

1. AWG3 опубликован только 24 июля 2026 года. На 31 июля уже появились несколько исправлений в `amneziawg-go` и четыре последовательных тега kernel module. Это ещё не зрелый production baseline.
2. Текущий MikroTik-контур использует `amneziawg-mikrotik-c` / `awg-proxy`, а не `amneziawg-go` как сетевой интерфейс.
3. `awg-proxy` является прозрачным преобразователем AWG-пакетов между RouterOS WireGuard и обычным WireGuard backend на production-edge. Официальный `amneziawg-go` является полноценной реализацией WireGuard-интерфейса и не является drop-in packet proxy.
4. В проверенном source tree `amneziawg-mikrotik-c` отсутствуют AWG3 Header Protection, Content Padding и настраиваемые таймеры.
5. Поэтому замена только бинарников/tools/config внутри существующего контейнера **не доказана и с официальными бинарниками напрямую невозможна** при требовании сохранить RouterOS WG/monitoring contract.
6. В `amneziawg-tools v3.0.20260730` обнаружен как минимум один QA-сигнал: `show.c` содержит spelling `max-handshake-attemps`, тогда как конфигурационный параметр называется `MaxHandshakeAttempts`. До стенда нельзя считать round-trip `set/show/showconf` доказанным.

### 1.2. Что реально можно сохранить

Если будет создана и отдельно проверена AWG3-версия **того же transparent proxy**, можно сохранить внешний контракт полностью:

- RouterOS interface `WG-production-edge`;
- RouterOS `ListenPort = 12955`;
- tunnel subnet `10.99.99.0/24`;
- RouterOS peer endpoint на прежний IP контейнера и UDP/51820;
- существующий veth/bridge;
- MTU 1280;
- маршруты, firewall, NAT;
- container object `awg-production-edge-fixed`, root-dir/layer-dir;
- `Container_Storage_Guard` lifecycle;
- monitoring contract по `WG-production-edge` handshake/counters;
- production-edge public UDP/443;
- production-edge backend interface/address/peer identities.

Без AWG3-capable transparent proxy официальный путь требует изменить внутреннюю архитектуру: завершать AWG3 как L3-интерфейс внутри контейнера и строить отдельную локальную связь RouterOS↔container. Это уже не «замена бинарников» и создаёт двойной/вложенный tunnel contract. Для первой волны такой вариант отклонён.

## 2. Проверенные источники и версии

### 2.1. Upstream baseline

| Компонент | Проверенная версия | Дата/commit | Роль |
|---|---:|---|---|
| `amneziawg-go` | `v3.0.3` | 2026-07-31, `cf9d2dd` | userspace AWG3 network interface |
| `amneziawg-tools` | `v3.0.20260730` | 2026-07-30, `d09ecc3` | `awg`, `awg-quick`, config/UAPI tooling |
| `amneziawg-linux-kernel-module` | `v3.0.20260731-04` | 2026-07-31 | optional Linux kernel data plane |
| `amneziawg-mikrotik-c` | local checked HEAD `4d7a636` | AWG2 proxy code | current RouterOS compatibility layer; AWG3 absent |

Upstream references:

- <https://github.com/amnezia-vpn/amneziawg-go>
- <https://github.com/amnezia-vpn/amneziawg-tools>
- <https://github.com/amnezia-vpn/amneziawg-linux-kernel-module>
- <https://github.com/timbrs/amneziawg-mikrotik-c>

There are no GitHub Releases in `amneziawg-go`; production must pin a signed/recorded source tag and commit, not `master`.

### 2.2. Локальный AWG2 baseline

Источник: `AWG2_GOLDEN_CONFIGURATION.md`, snapshot 2026-07-04.

RB5009:

- container: `awg-production-edge-fixed`;
- persistent root: `/usb2-part1/controot/awg-production-edge-fixed`;
- layer dir: `/usb2-part1/clayers`;
- RouterOS interface: `WG-production-edge`;
- MTU: 1280;
- ListenPort: 12955;
- peer endpoint: container `172.18.0.2:51820`;
- allowed IPs: `10.99.99.1/32,0.0.0.0/0`;
- keepalive: 25 seconds;
- proxy remote: `213.176.116.165:443`;
- fixed outer source port: 38080;
- production-edge must remain WAN-only.

production-edge:

- service: `awg-proxy-production-edge`;
- public listener: UDP/443;
- backend: `wg-production-edge-awg`, `127.0.0.1:2057`;
- tunnel subnet: `10.99.99.0/24`;
- WARP/NAT/routing remain out of mutation scope.

## 3. Компоненты полноценного AWG3

### 3.1. `amneziawg-go`

Userspace implementation, fork of `wireguard-go`. Создаёт TUN-интерфейс, реализует WireGuard cryptographic state machine и AWG1/1.5/2/3 obfuscation.

Нужен, если:

- kernel module отсутствует или не подходит ядру;
- окружение — контейнер без возможности загрузить модуль хоста;
- требуется одинаковая реализация на разных платформах.

Не является заменой `awg-proxy`: принимает/выдаёт L3-трафик через TUN, а не оборачивает внешний RouterOS WireGuard packet stream.

Build-time: актуальный Go (upstream Dockerfile сейчас использует Go 1.25.12). Runtime: TUN device, `CAP_NET_ADMIN`, доступ к UDP socket, libc/OS primitives. Go runtime статически входит в бинарник при обычной сборке.

### 3.2. `amneziawg-tools`

Набор управления интерфейсом:

- `awg` — аналог `wg`: ключи, UAPI/netlink, `show`, `set`, `setconf`, `showconf`;
- `awg-quick` — shell orchestration: link, addresses, routes, DNS, hooks, systemd unit;
- completion/man/systemd unit — необязательная операторская обвязка.

Build-time upstream заявляет C compiler + sane libc. Для полного `awg-quick` runtime обычно нужны:

- `bash`;
- `iproute2` (`ip`);
- route/firewall helpers, которые реально использует конфигурация;
- `resolvconf`, только если применяется `DNS=`;
- `systemd`, только если выбран `awg-quick@.service`.

### 3.3. `awg`

Обязателен для конфигурирования official AWG3 interface и генерации ключей:

- `awg genkey` — private/header-protection key material;
- `awg pubkey` — public key;
- `awg setconf/showconf/show` — runtime state.

В текущем proxy architecture `awg` не управляет `awg-proxy`; там конфигурация env-only.

### 3.4. `awg-quick`

Не является протоколом и не обязателен. Это convenience lifecycle wrapper.

Для production-edge возможен, но production-профиль должен выбрать **одного** владельца lifecycle: либо `awg-quick@awg0.service`, либо собственный unit, но не оба.

Для RouterOS container `awg-quick` не решает главную проблему: он создаёт L3 AWG interface, а не transparent proxy.

### 3.5. Userspace и kernel requirements

Есть два взаимоисключающих data plane варианта:

1. Kernel module `amneziawg`: быстрее, меньше context switches; требует совместимого Linux kernel headers/DKMS или заранее собранного модуля, загрузки модуля на host и совпадения tools/UAPI.
2. `amneziawg-go`: не требует AWG kernel module; требует `/dev/net/tun` и capabilities; обычно медленнее kernel path, но для RouterOS container операционно проще.

Нельзя одновременно назначить один interface двум реализациям. `awg-quick` сначала пытается создать `type amneziawg`, а при отсутствии модуля может перейти на userspace implementation.

Для RouterOS container host kernel module практически не является самостоятельным решением: RouterOS host не предоставляет обычную Linux DKMS-модель. Реалистичен userspace, но только если выбран L3 interface design или разработан AWG3 proxy.

### 3.6. Дополнительные библиотеки

Протокол AWG3 не требует отдельного userspace crypto daemon. Header Protection реализован внутри `amneziawg-go`/kernel module (ChaCha20-based code path).

Production image должен содержать только реально используемые зависимости:

- CA certificates — только для build/download, не для numeric UDP endpoint;
- `iproute2`, `bash` — если используется `awg-quick`;
- firewall package — только если hooks реально управляют NAT/firewall;
- `resolvconf` — только при `DNS=`;
- health/debug utilities допустимы в debug image, но не обязательны в runtime.

## 4. AWG2 → AWG3: матрица совместимости

| Contract | Сохраняется при AWG3 proxy | Official AWG3 direct | Комментарий |
|---|---|---|---|
| RouterOS interface `WG-production-edge` | да | нет как AWG3 endpoint | RouterOS говорит обычным WG |
| RouterOS ListenPort 12955 | да | можно оставить, но он относится к local WG | Не AWG3 public port |
| Tunnel subnet 10.99.99.0/24 | да | да | L3 addressing не зависит от obfuscation |
| veth/bridge | да | физически да | Direct design потребует другого L3 contract поверх veth |
| MTU 1280 | да | да | Консервативно оставляем без PMTU-доказательства |
| Public UDP/443 production-edge | да | да | TCP/443 не затрагивается |
| Routes | да | не гарантировано | L3 direct interface меняет next-hop/interface semantics |
| Firewall/NAT | да | не гарантировано | Interface owner/name может измениться |
| systemd/container lifecycle | да | меняется внутренний процесс | Имена объектов можно сохранить |
| Monitoring `WG-production-edge` handshake | да | нет | Official AWG3 handshake будет внутри container |
| Existing WireGuard keys/PSK | да, если proxy preserves model | да | HeaderProtectionKey добавляется отдельно |
| Existing J/H/I/S | да после parser compatibility tests | да | AWG3 remains configurable with old fields |
| Rollback by old image/config | да | сложнее | Direct redesign имеет больше rollback surface |

Обязательно меняется в полноценном AWG3 profile:

- executable/image digest;
- tools/UAPI version;
- конфигурация: `HeaderProtectionKey`, `ContentPaddingAddition`, timing ranges;
- значение/проверка `S1-S4`: каждое должно быть не меньше 12 при Header Protection;
- protocol compatibility: оба endpoint должны понимать Header Protection и использовать один ключ;
- observability: необходимо выводить и проверять AWG3 runtime fields без раскрытия ключа.

Не требуется менять:

- public endpoint/port;
- tunnel addresses;
- WireGuard keypair/PSK;
- AllowedIPs;
- veth, bridge, MTU;
- WARP egress;
- RouterOS policy routing/failover — но только при сохранении proxy model.

## 5. Полный перечень новых механизмов AWG3

### 5.1. Header Protection

Параметр: `HeaderProtectionKey` (device, shared/server-side).

- Что делает: шифрует низкоэнтропийные поля заголовков AWG/WireGuard; nonce берётся из cryptographic padding `S1-S4` входящего пакета.
- Зачем: снижает стабильность сигнатур заголовков и корреляцию типов пакетов DPI.
- Рекомендация: отдельный 32-byte key через `awg genkey`, одинаковый на обоих endpoint; не переиспользовать WireGuard PrivateKey/PSK.
- Совместимость: несовместим с AWG2 endpoint; оба endpoint должны быть AWG3. При включении каждый `S1-S4 >= 12`.
- Производительность: дополнительный ChaCha20 operation на пакет; ожидаемо небольшой, но должен быть измерен на RB5009 CPU.
- DPI: главное нововведение v3; потенциально сильнее всего влияет на фиксированные header patterns.
- Обязательность: протокол может работать без него, но для заявленного production AWG3 profile — обязательно.

### 5.2. Content Padding

Параметр: `ContentPaddingAddition = low-high` (device, sender/client-side, uint16 range в tools).

- Что делает: добавляет случайный объём padding к содержимому исходящих сообщений.
- Зачем: размывает распределение размеров пакетов и связь между исходным payload и wire size.
- Рекомендация первой волны: `0-32`; после packet-capture/CPU/throughput тестов допустимо `16-64`. Значения выше без DPI-основания не применять.
- Совместимость: sender-side, необязательно одинаков на сторонах; для симметричной маскировки включить на обеих сторонах. AWG2 не понимает v3 framing.
- Производительность: увеличивает bandwidth и иногда packet size; высокий диапазон повышает overhead/fragmentation risk.
- DPI: снижает устойчивость size-based fingerprinting.
- Обязательность: нет; в максимальном стабильном профиле — включить консервативно.

### 5.3. RekeyAfterTime

- Что делает: задаёт интервал, после которого инициатор пытается обновить ключи.
- WireGuard default: 120 s.
- Рекомендация: `110-130` s.
- Совместимость: sender-local; одинаковое значение не требуется.
- Производительность: слишком низкое значение увеличивает handshakes/junk/CPS traffic и CPU.
- DPI: range устраняет идеально фиксированный rekey cadence.
- Обязательность: нет; включить для timing randomization.

### 5.4. RekeyTimeout

- Что делает: интервал повторной отправки handshake при отсутствии ответа.
- WireGuard default: 5 s плюс встроенный jitter.
- Рекомендация: `4-6` s.
- Совместимость: local.
- Производительность/стабильность: слишком мало создаёт burst/retry load; слишком много замедляет recovery.
- DPI: размывает retry cadence.
- Обязательность: нет.

### 5.5. RejectAfterTime

- Что делает: срок жизни keypair, после которого входящие данные отклоняются и требуется новый handshake.
- WireGuard default: 180 s.
- Рекомендация: `175-190` s; всегда выше верхней границы RekeyAfterTime с достаточным recovery window.
- Совместимость: local.
- Производительность/стабильность: слишком низкое значение вызывает data blackouts при loss; слишком высокое расширяет lifetime старого keypair.
- DPI: range размывает forced-rekey boundary.
- Обязательность: нет.

### 5.6. KeepaliveTimeout

- Что делает: после отсутствия исходящих данных планирует authenticated keepalive.
- WireGuard default internal timeout: 10 s.
- Рекомендация: `9-11` s.
- Совместимость: local.
- Производительность: слишком низкое значение добавляет фоновые пакеты; слишком высокое ухудшает NAT/recovery behavior.
- DPI: range уменьшает фиксированную периодичность event-driven keepalive.
- Обязательность: нет.

### 5.7. MaxHandshakeAttempts

- Что делает: ограничивает количество повторов handshake timer до прекращения очереди/очистки состояния.
- WireGuard-derived default в source: 18 (`90/5`).
- Рекомендация: `16-20`.
- Совместимость: local.
- Производительность: высокая граница продлевает retry traffic при недоступном endpoint; низкая — ухудшает recovery.
- DPI: range меняет длину retry train, но это не основной anti-DPI механизм.
- Обязательность: нет.

### 5.8. PersistentKeepalive как range

- Что изменилось: peer parameter теперь допускает `low-high`, а не только фиксированное число.
- Рекомендация для NATed RB5009: `23-27` s. На production-edge server peer — off/0, если серверу не нужно инициировать NAT keepalive.
- Совместимость: old tools/binaries ожидают scalar; v3 range требует v3 обеих управляющих частей, но runtime choice локален.
- Производительность: средняя частота остаётся около прежних 25 s.
- DPI: устраняет точный 25-second cadence.
- Обязательность: для NATed client — рекомендуется; для server — нет.

### 5.9. Что не является новым в AWG3

Следующие механизмы уже существовали до v3 и должны быть сохранены/перепроверены, но не записываться как новые:

- `Jc`, `Jmin`, `Jmax` junk packets;
- `S1-S4` message padding (S3/S4 — AWG2);
- ranged `H1-H4` message headers (AWG2);
- `I1-I5` custom signature packets/CPS (AWG1.5);
- WireGuard keys, PSK, AllowedIPs, Endpoint, ListenPort;
- `PersistentKeepalive` как фиксированное значение.

## 6. Рекомендуемый production-профиль

### 6.1. Принципы

- Максимальный функционал означает включить все независимые v3-механизмы, а не максимизировать числовые значения.
- Не менять доказанный AWG2 camouflage envelope без отдельного A/B DPI evidence.
- Не допускать IP fragmentation: итоговый outer datagram обязан оставаться ниже фактического path MTU.
- HeaderProtectionKey — новый отдельный secret.
- Timing ranges должны центрироваться вокруг WireGuard defaults.
- Server и client получают одинаковые `S/H` и HeaderProtectionKey; client-side J/I/Content/Timings могут отличаться.

### 6.2. Target profile `AWG3-production-edge-RB5009-P1`

| Parameter | RB5009 sender | production-edge sender | Причина |
|---|---:|---:|---|
| Jc | 4 | 0 | junk только на инициаторе достаточно |
| Jmin/Jmax | 50/1000 | 0/0 | сохранить working AWG2 envelope; 1000 < MTU 1280 |
| S1/S2/S3/S4 | 84/40/46/20 | same | все >= 12; сохраняем baseline |
| H1-H4 | existing validated ranges | same | не менять DPI envelope в той же волне |
| I1-I5 | existing validated templates | empty initially on server | сохранить client CPS; не дублировать лишний traffic |
| HeaderProtectionKey | same generated key | same generated key | обязательный v3 feature |
| ContentPaddingAddition | 0-32 | 0-32 | симметричная умеренная size randomization |
| RekeyAfterTime | 110-130 | 110-130 | around 120 |
| RekeyTimeout | 4-6 | 4-6 | around 5 |
| RejectAfterTime | 175-190 | 175-190 | around 180; recovery margin |
| KeepaliveTimeout | 9-11 | 9-11 | around 10 |
| MaxHandshakeAttempts | 16-20 | 16-20 | around 18 |
| PersistentKeepalive | 23-27 | off | NAT client only |
| MTU | 1280 | 1280 | preserve known stable value |

`Jmax=1000` плюс максимум content padding 32 не доказывает отсутствие fragmentation для каждого packet class. Перед production обязателен pcap-based outer-size test и PMTU test; при превышении outer path limit снижать ContentPaddingAddition, не MTU вслепую.

## 7. Целевые конфигурации (не для применения)

### 7.1. production-edge — official AWG3 reference

Этот config корректен для **direct official AWG3 interface**, но не является drop-in replacement текущего `awg-proxy-production-edge`, пока RB5009-side transparent proxy не поддерживает AWG3.

```ini
# /etc/amnezia/awg-production-edge.conf — DESIGN ONLY
[Interface]
PrivateKey = <EXISTING_production-edge_WG_PRIVATE_KEY>
Address = 10.99.99.1/24
ListenPort = 443
MTU = 1280

Jc = 0
Jmin = 0
Jmax = 0
S1 = 84
S2 = 40
S3 = 46
S4 = 20
H1 = <EXISTING_VALIDATED_H1_RANGE>
H2 = <EXISTING_VALIDATED_H2_RANGE>
H3 = <EXISTING_VALIDATED_H3_RANGE>
H4 = <EXISTING_VALIDATED_H4_RANGE>
HeaderProtectionKey = <NEW_SHARED_AWG3_HEADER_KEY>
ContentPaddingAddition = 0-32
RekeyAfterTime = 110-130
RekeyTimeout = 4-6
RejectAfterTime = 175-190
KeepaliveTimeout = 9-11
MaxHandshakeAttempts = 16-20

[Peer]
PublicKey = <EXISTING_RB5009_WG_PUBLIC_KEY>
PresharedKey = <EXISTING_PSK>
AllowedIPs = 10.99.99.4/32
PersistentKeepalive = 0
```

production-edge routes, WARP policy, NAT and firewall rules are intentionally absent: they must be retained byte-for-byte from live baseline, not regenerated from this research document.

### 7.2. RB5009 container — required AWG3 proxy contract

Это **спецификация требуемого proxy**, не подтверждённый набор поддерживаемых env текущего бинарника:

```dotenv
AWG_MODE=normal
AWG_LISTEN=:51820
AWG_REMOTE=213.176.116.165:443
AWG_SRC_PORT=38080
AWG_JC=4
AWG_JMIN=50
AWG_JMAX=1000
AWG_S1=84
AWG_S2=40
AWG_S3=46
AWG_S4=20
AWG_H1=<EXISTING_VALIDATED_H1_RANGE>
AWG_H2=<EXISTING_VALIDATED_H2_RANGE>
AWG_H3=<EXISTING_VALIDATED_H3_RANGE>
AWG_H4=<EXISTING_VALIDATED_H4_RANGE>
AWG_I1=<EXISTING_VALIDATED_TEMPLATE>
AWG_I2=<EXISTING_VALIDATED_TEMPLATE>
AWG_I3=<EXISTING_VALIDATED_TEMPLATE>
AWG_I4=<EXISTING_VALIDATED_TEMPLATE>
AWG_I5=<EXISTING_VALIDATED_TEMPLATE>
AWG_HEADER_PROTECTION_KEY=<NEW_SHARED_AWG3_HEADER_KEY>
AWG_CONTENT_PADDING_ADDITION=0-32
AWG_REKEY_AFTER_TIME=110-130
AWG_REKEY_TIMEOUT=4-6
AWG_REJECT_AFTER_TIME=175-190
AWG_KEEPALIVE_TIMEOUT=9-11
AWG_MAX_HANDSHAKE_ATTEMPTS=16-20
AWG_PERSISTENT_KEEPALIVE=23-27
AWG_SERVER_PUB=<EXISTING_production-edge_WG_PUBLIC_KEY>
AWG_CLIENT_PUB=<EXISTING_RB5009_WG_PUBLIC_KEY>
AWG_LOG_LEVEL=info
AWG_TIMEOUT=60
AWG_NO_GRO=1
```

**Текущий `awg-proxy` эти новые env не реализует.** Запуск с ними без проверки может молча проигнорировать поля и остаться AWG2, что опаснее явного fail-fast. Будущий binary обязан завершаться с ошибкой на неизвестных/некорректных AWG3 parameters и печатать protocol version/config fingerprint без secret values.

### 7.3. RouterOS contract — без изменений

```routeros
# DESIGN ASSERTIONS, not mutation commands
# interface: WG-production-edge
# mtu: 1280
# listen-port: 12955
# peer endpoint: 172.18.0.2:51820
# allowed-address: 10.99.99.1/32,0.0.0.0/0
# persistent-keepalive: 25s
```

RouterOS keepalive остаётся 25 s, потому что RouterOS видит локальный обычный WG peer/proxy. AWG3 ranged PersistentKeepalive относится к AWG3 implementation/proxy outer behavior и не должен подменять RouterOS field без подтверждённой реализации.

## 8. План мутации после закрытия gates

### Phase 0 — обязательные предварительные gates

1. Зафиксировать exact live read-only snapshots production-edge и RB5009.
2. Зафиксировать image digest, binary SHA-256, config hashes, service/container definitions, routes/rules/firewall/NAT, sockets, keys-as-public-identities.
3. Получить AWG3-capable `amneziawg-mikrotik-c` build или реализовать AWG3 в том же proxy model.
4. Unit tests: v2 vectors, v3 header protection, content padding, all timer ranges, invalid range rejection, `S1-S4 < 12` rejection.
5. Interop matrix: go↔go, proxy↔go, proxy↔kernel module; ARM64 RouterOS container binary.
6. Доказать сохранение outer source port 38080 и WAN-only conntrack return path.
7. 24-hour lab soak, затем 72-hour pre-production soak с packet loss/reorder/NAT rebinding/restart tests.
8. Только после этого назначить immutable versions/digests production baseline.

### Phase 1 — production-edge first

production-edge нельзя переключать на AWG3 раньше client readiness. Поэтому «production-edge first» означает подготовить artefacts и atomic rollback, затем короткое coordinated cutover window.

1. Read-only audit и backup:
   - unit, env/config, executable/image, backend WG config;
   - `ip -br a`, routes/rules, `ss -lunp`, firewall/NAT, WARP state;
   - current handshakes/counters.
2. Подготовить AWG3 binary/config вне active paths.
3. Проверить hashes, owner/mode, syntax/parser offline.
4. Не запускать второй постоянный service. Допустим только foreground loopback/offline self-test без public listener.
5. В coordinated window остановить `awg-proxy-production-edge` и запустить AWG3 replacement под **тем же unit name**, UDP/443 и backend contract.
6. Проверить socket ownership, process version/config fingerprint и отсутствие второго listener.
7. Если RB5009 cutover не начат немедленно или server health failed — rollback production-edge.

### Phase 2 — RB5009

1. Safe-mode/console-access readiness и экспорт relevant RouterOS objects.
2. Backup container object/envlist/mounts/veth/bridge/storage guard.
3. Убедиться, что persistent storage — `usb2-part1`, не `usb-1` tmpfs.
4. Подготовить replacement image root/layers заранее, но не создавать второй постоянный service.
5. Atomic mutation существующего lifecycle contract:
   - тот же container logical name;
   - тот же veth;
   - тот же local UDP/51820;
   - тот же outer source 38080;
   - тот же RouterOS WG interface/peer.
6. Запустить container и проверить startup fail-fast/version/config fingerprint.
7. Не менять routes, policy routing, failover, NAT или monitoring.

### Phase 3 — проверки

Порядок обязательный:

1. production-edge: ровно один UDP/443 owner; ожидаемый PID/version.
2. RB5009 container: running; veth/bridge unchanged; local UDP/51820 owner.
3. Outer conntrack: destination production-edge/443, source port 38080, reply-dst только WAN address; не IPTV/LTE.
4. Fresh RouterOS `WG-production-edge` handshake после cutover timestamp.
5. RX и TX растут в обе стороны; не принимать старые accumulated counters.
6. `ping 10.99.99.1`.
7. Controlled payload through production-edge table without changing selector/failover.
8. DNS and WARP egress checks only inside production-edge path.
9. Packet capture confirms:
   - Header Protection enabled;
   - no stable AWG2 header values;
   - content padding varies;
   - no fragmentation;
   - timing ranges statistically observed.
10. CPU, memory, packet loss, RTT, throughput; compare with AWG2 baseline.
11. Restart RB container, restart production-edge unit, WAN DHCP renewal/NAT rebinding.
12. Soak and reconnect tests; monitoring 101 remains untouched but continues to see the same `WG-production-edge` contract.

### Phase 4 — acceptance

Production acceptance requires all:

- zero unexpected changes in route/rule/firewall/NAT diffs;
- no other host/service changed;
- fresh handshake and bidirectional payload;
- WAN-only outer path;
- no fragmentation at profile maximum;
- CPU headroom within agreed threshold;
- successful independent restart/reconnect;
- rollback rehearsal passed;
- at least 72 hours stable observation after initial 24-hour gate.

## 9. Rollback

### 9.1. Trigger conditions

Immediate rollback on any:

- no fresh handshake within two normal rekey windows;
- TX without RX;
- outer conntrack uses IPTV/LTE or wrong source address;
- duplicate/missing UDP/443 owner;
- unknown parameters ignored instead of fail-fast;
- fragmentation at normal payload;
- CPU saturation, sustained loss/latency regression;
- routes/rules/firewall/NAT differ outside approved object;
- monitoring contract disappears;
- restart/reconnect fails.

### 9.2. Rollback order

1. RB5009 first: stop mutated container, restore exact AWG2 image digest/env/config under existing object/veth/lifecycle, start it.
2. production-edge second: stop AWG3 replacement, restore exact AWG2 binary/config/unit content, start same `awg-proxy-production-edge` unit.
3. Проверить one-owner sockets, fresh AWG2 handshake, RX/TX, `10.99.99.1`, WAN-only conntrack.
4. Сравнить routes/rules/firewall/NAT with pre-change hashes.
5. Не включать KVN/FBSH failover как способ скрыть неуспешный rollback; failover вне scope.

Rollback не должен требовать package manager, image pull или rebuild. Все exact AWG2 artifacts должны быть локально доступны до cutover.

## 10. Риски и проверки

| Риск | Влияние | Проверка/mitigation |
|---|---|---|
| Очень молодой AWG3 release | regressions | pin tag+commit+hash; lab/soak; no `master` |
| Proxy не поддерживает v3 | silent AWG2 или outage | feature tests; fail on unknown config; pcap proof |
| Header key mismatch | полный handshake failure | compare secret hashes locally, never print key |
| `S1-S4 < 12` | config reject/broken HP | static validation and negative test |
| Tools/UAPI mismatch | fields lost/misreported | exact matched versions; set/show/showconf round-trip |
| `MaxHandshakeAttempts` spelling defect | false observability | direct config/UAPI test; do not trust one `show` path |
| Content padding fragmentation | loss/DPI signal | outer pcap, PMTU, lower 0-32 first |
| Timer ranges violate relationships | flaps/drop | validate min/max inequalities; loss/reorder test |
| CPU cost on RB5009 | latency/loss | sustained throughput + CPU profile |
| RouterOS storage regression | container fails after reboot | `usb2-part1`; reboot/restart validation |
| Wrong WAN source/return path | tunnel down | conntrack `reply-dst-address`; DHCP renewal test |
| Identity split | TX no RX | public-key chain validation end to end |
| Monitoring loses semantics | invisible failure | same `WG-production-edge` interface and fresh timestamps |
| Rollback depends on network | prolonged outage | local immutable AWG2 artifacts before window |

## 11. Открытые технические вопросы

До реализации должны быть закрыты:

1. Кто является владельцем AWG3 implementation для `amneziawg-mikrotik-c`, и есть ли upstream roadmap/branch?
2. Будет ли proxy сохранять transparent packet transform, server session routing и multi-peer semantics AWG2?
3. Как именно Content Padding и Header Protection должны быть реализованы в proxy без full WireGuard key state?
4. Совпадают ли wire format и crypto test vectors `proxy↔amneziawg-go v3.0.3`?
5. Как proxy будет выражать ranged timers, если WireGuard state machine остаётся в RouterOS/native backend?
6. Если timers принадлежат endpoint state machine, возможно ли вообще честно реализовать их в прозрачном proxy? Вероятный ответ: нет; тогда «все AWG3 функции» и «тот же RouterOS WG contract» логически несовместимы.
7. Поддерживает ли production target `amneziawg-tools v3` все fields без silent truncation/typos?
8. Каков измеренный outer packet-size максимум при текущих I/J/S и ContentPadding 0-32?
9. Какой rollback RTO принимается для coordinated production-edge/RB5009 cutover?

## 12. Архитектурное решение

Для первой волны допустим только один из двух решений:

### Решение A — preferred

Дождаться/разработать AWG3-capable transparent `awg-proxy`, сохранить весь внешний contract, а v3 timing features включать только если они действительно применимы к packet-proxy model и подтверждены interop tests.

Плюсы: минимальная mutation surface, лучший rollback, monitoring unchanged.  
Минус: Header/Content можно реализовать, но state-machine timings могут оказаться принципиально недоступны proxy.

### Решение B — не для первой волны

Перейти на full official AWG3 interface внутри RB container и L3-routing contract между RouterOS и container.

Плюсы: все официальные AWG3 функции.  
Минусы: меняются data plane, routing/monitoring semantics, появляется локальная вложенность; это противоречит текущим ограничениям.

**Вывод:** требования «тот же RouterOS WG/monitoring contract» и «использовать весь official AWG3, включая state-machine timers» пока нельзя одновременно выполнить доказанным способом. Production migration должна оставаться заблокированной до решения этого противоречия.
