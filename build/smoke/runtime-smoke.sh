#!/bin/sh
set -eu

STATE_DIR="${AWG3_SMOKE_STATE_DIR:-/tmp/awg3-smoke}"
mkdir -p "${STATE_DIR}"
PATH="/smoke/bin:${PATH}"

/gateway --mode validate --config /smoke/awg3.json --secrets /smoke/secrets.json

rm -f "${STATE_DIR}/gateway.log" "${STATE_DIR}/amneziawg-go.pid"

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

kill -TERM "${gateway_pid}"
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

test ! -f "${STATE_DIR}/amneziawg-go.pid"
