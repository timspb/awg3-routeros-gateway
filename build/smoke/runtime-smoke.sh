#!/bin/sh
set -eu

STATE_DIR="${AWG3_SMOKE_STATE_DIR:-/tmp/awg3-smoke}"
mkdir -p "${STATE_DIR}"
PATH="/smoke/bin:${PATH}"

/gateway --mode validate --config /smoke/awg3.json --secrets /smoke/secrets.json

rm -f "${STATE_DIR}"/*.state "${STATE_DIR}"/*.commands "${STATE_DIR}"/*.exit \
	"${STATE_DIR}/gateway.log" "${STATE_DIR}/amneziawg-go.pid" \
	"${STATE_DIR}/sysctl.ipv4" "${STATE_DIR}/sysctl.ipv6"

/gateway \
	--mode run \
	--config /smoke/awg3.json \
	--secrets /smoke/secrets.json \
	--artifact-manifest /etc/awg3/runtime-artifacts-smoke.json \
	--status-listen 127.0.0.1:18080 \
	--config-listen 127.0.0.1:18081 \
	--runtime-binary /smoke/bin/amneziawg-go \
	--awg-binary /smoke/bin/awg \
	--ip-binary /smoke/bin/ip \
	--sysctl-binary /smoke/bin/sysctl \
	--parser-validator-binary /usr/local/bin/awg3-parser-validate \
	>"${STATE_DIR}/gateway.log" 2>&1 &
gateway_pid=$!

child_pid=""
attempts=0
while [ ${attempts} -lt 100 ]; do
	if [ -f "${STATE_DIR}/amneziawg-go.pid" ]; then
		child_pid="$(cat "${STATE_DIR}/amneziawg-go.pid")"
		break
	fi
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		cat "${STATE_DIR}/gateway.log"
		exit 1
	fi
	attempts=$((attempts + 1))
	sleep 0.1
done

if [ -z "${child_pid}" ]; then
	cat "${STATE_DIR}/gateway.log"
	echo "runtime child pid file did not appear" >&2
	exit 1
fi

# Supervisor.Run starts the status listener only after Runtime.Start has
# completed its apply and readiness checks. Do not race shutdown against
# those checks merely because the child has written its pid file.
attempts=0
while [ ${attempts} -lt 100 ]; do
	if ss -ltnp 2>/dev/null | grep -q "127.0.0.1:18080.*pid=${gateway_pid},"; then
		break
	fi
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		cat "${STATE_DIR}/gateway.log"
		exit 1
	fi
	attempts=$((attempts + 1))
	sleep 0.1
done

if [ ${attempts} -ge 100 ]; then
	cat "${STATE_DIR}/gateway.log"
	echo "gateway status listener did not become ready" >&2
	exit 1
fi

ready_attempts=0
while [ ${ready_attempts} -lt 100 ]; do
	if curl --fail --silent --show-error --max-time 1 http://127.0.0.1:18080/healthz >/dev/null 2>&1; then
		break
	fi
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		cat "${STATE_DIR}/gateway.log"
		echo "gateway exited before healthz became ready" >&2
		exit 1
	fi
	ready_attempts=$((ready_attempts + 1))
	sleep 0.1
done

if [ ${ready_attempts} -ge 100 ]; then
	cat "${STATE_DIR}/gateway.log"
	echo "healthz did not become ready" >&2
	exit 1
fi

kill -TERM "${gateway_pid}" 2>/dev/null || true
gateway_rc=0
if wait "${gateway_pid}"; then
	gateway_rc=0
else
	gateway_rc=$?
fi

if [ "${gateway_rc}" -ne 0 ] && [ "${gateway_rc}" -ne 143 ]; then
	cat "${STATE_DIR}/gateway.log"
	echo "gateway exited with ${gateway_rc} during shutdown" >&2
	exit 1
fi

if kill -0 "${child_pid}" 2>/dev/null; then
	echo "runtime child still alive after gateway shutdown" >&2
	exit 1
fi

if [ "$(cat "${STATE_DIR}/amneziawg-go.exit" 2>/dev/null || true)" != "signal=TERM status=0" ]; then
	cat "${STATE_DIR}/gateway.log"
	if [ -f "${STATE_DIR}/amneziawg-go.exit" ]; then
		sed -n '1p' "${STATE_DIR}/amneziawg-go.exit"
	else
		echo "runtime child exit record is missing" >&2
	fi
	echo "runtime child did not record a controlled TERM exit" >&2
	exit 1
fi

for leaked_state in interface.state address.state tunnel-routes.state endpoint-route.state; do
	if [ -e "${STATE_DIR}/${leaked_state}" ]; then
		echo "runtime cleanup leaked ${leaked_state}" >&2
		exit 1
	fi
done

if [ "$(cat "${STATE_DIR}/sysctl.ipv4")" != "0" ] || [ "$(cat "${STATE_DIR}/sysctl.ipv6")" != "0" ]; then
	echo "runtime cleanup did not restore forwarding sysctls" >&2
	exit 1
fi

if grep -q '^rule add ' "${STATE_DIR}/ip.commands"; then
	echo "runtime smoke unexpectedly created a policy rule" >&2
	exit 1
fi

rm -f "${STATE_DIR}/amneziawg-go.pid"
