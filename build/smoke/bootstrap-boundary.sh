#!/bin/sh
set -eu

STATE_DIR="${AWG3_SMOKE_STATE_DIR:-/tmp/awg3-bootstrap-boundary}"
BOOTSTRAP_DIR="${AWG3_BOOTSTRAP_DIR:-/bootstrap}"
CONFIG_DIR="${AWG3_CONFIG_DIR:-/config}"
mkdir -p "${STATE_DIR}" "${BOOTSTRAP_DIR}" "${CONFIG_DIR}"
PATH="/smoke/bin:${PATH}"

cleanup() {
	kill "${gateway_pid:-}" 2>/dev/null || true
	wait "${gateway_pid:-}" 2>/dev/null || true
}

trap cleanup EXIT INT TERM

rm -rf "${CONFIG_DIR}/.awg3-generations"
rm -f "${CONFIG_DIR}/current-generation.json" "${CONFIG_DIR}/awg3.json" "${CONFIG_DIR}/secrets.json"
cp /smoke/awg3.json "${BOOTSTRAP_DIR}/awg3.json"
cp /smoke/secrets.json "${BOOTSTRAP_DIR}/secrets.json"

/usr/local/bin/bootstrap-entrypoint \
	--mode run \
	--status-listen 127.0.0.1:18180 \
	--config-listen 127.0.0.1:18181 \
	--artifact-manifest /etc/awg3/runtime-artifacts-smoke.json \
	--runtime-binary /smoke/bin/amneziawg-go \
	--awg-binary /smoke/bin/awg \
	--ip-binary /smoke/bin/ip \
	--sysctl-binary /smoke/bin/sysctl \
	--parser-validator-binary /usr/local/bin/awg3-parser-validate \
	>"${STATE_DIR}/first-boot.log" 2>&1 &
gateway_pid=$!

attempts=0
while [ "${attempts}" -lt 100 ]; do
	if [ -f "${CONFIG_DIR}/awg3.json" ] && [ -f "${CONFIG_DIR}/secrets.json" ]; then
		break
	fi
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		cat "${STATE_DIR}/first-boot.log"
		echo "bootstrap gateway exited before config was imported" >&2
		exit 1
	fi
	attempts=$((attempts + 1))
	sleep 0.1
done

if [ "$(stat -c '%a' "${CONFIG_DIR}/awg3.json")" != "600" ] || [ "$(stat -c '%a' "${CONFIG_DIR}/secrets.json")" != "600" ]; then
	echo "bootstrap copied files are not 0600" >&2
	exit 1
fi

if [ "$(cat "/proc/${gateway_pid}/comm" 2>/dev/null || true)" != "gateway" ]; then
	cat "${STATE_DIR}/first-boot.log"
	echo "bootstrap process did not exec gateway" >&2
	exit 1
fi

if ! curl --fail --silent --show-error --max-time 1 http://127.0.0.1:18180/healthz >/dev/null 2>&1; then
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		cat "${STATE_DIR}/first-boot.log"
		echo "bootstrap gateway exited before healthz became ready" >&2
		exit 1
	fi
fi

kill -TERM "${gateway_pid}" 2>/dev/null || true
wait "${gateway_pid}" 2>/dev/null || true

first_sum_awg3="$(sha256sum "${CONFIG_DIR}/awg3.json" | awk '{print $1}')"
first_sum_secrets="$(sha256sum "${CONFIG_DIR}/secrets.json" | awk '{print $1}')"

sed -i 's/"generation": "gen-smoke"/"generation": "gen-smoke-bootstrap"/' "${BOOTSTRAP_DIR}/awg3.json"
/usr/local/bin/bootstrap-entrypoint --mode status >/dev/null 2>&1

if [ "${first_sum_awg3}" != "$(sha256sum "${CONFIG_DIR}/awg3.json" | awk '{print $1}')" ] || [ "${first_sum_secrets}" != "$(sha256sum "${CONFIG_DIR}/secrets.json" | awk '{print $1}')" ]; then
	echo "existing config state was overwritten by bootstrap" >&2
	exit 1
fi

rm -f "${BOOTSTRAP_DIR}/secrets.json"
rm -f "${CONFIG_DIR}/awg3.json" "${CONFIG_DIR}/secrets.json" "${CONFIG_DIR}/current-generation.json"
rm -rf "${CONFIG_DIR}/.awg3-generations"
if /usr/local/bin/bootstrap-entrypoint --mode status >/dev/null 2>&1; then
	echo "partial bootstrap unexpectedly succeeded" >&2
	exit 1
fi

if [ -e "${CONFIG_DIR}/awg3.json" ] || [ -e "${CONFIG_DIR}/secrets.json" ]; then
	echo "partial bootstrap left config state behind" >&2
	exit 1
fi
