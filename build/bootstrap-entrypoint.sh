#!/bin/sh
set -eu

CONFIG_DIR="${AWG3_CONFIG_DIR:-/config}"
BOOTSTRAP_DIR="${AWG3_BOOTSTRAP_DIR:-/bootstrap}"
STATE_DIR="${AWG3_BOOTSTRAP_STATE_DIR:-${CONFIG_DIR}/.awg3-bootstrap}"

has_valid_committed_state() {
	if [ -f "${CONFIG_DIR}/current-generation.json" ]; then
		return 0
	fi
	if [ -d "${CONFIG_DIR}/.awg3-generations" ] && find "${CONFIG_DIR}/.awg3-generations" -mindepth 2 -maxdepth 2 -name committed.json -type f -print -quit 2>/dev/null | grep -q .; then
		return 0
	fi
	if [ -f "${CONFIG_DIR}/awg3.json" ] && [ -f "${CONFIG_DIR}/secrets.json" ] && /gateway --mode validate --config "${CONFIG_DIR}/awg3.json" --secrets "${CONFIG_DIR}/secrets.json" >/dev/null 2>&1; then
		return 0
	fi
	return 1
}

bootstrap_first_run() {
	if [ ! -f "${BOOTSTRAP_DIR}/awg3.json" ] || [ ! -f "${BOOTSTRAP_DIR}/secrets.json" ]; then
		echo "bootstrap files are missing" >&2
		exit 1
	fi

	rm -rf "${STATE_DIR}"
	mkdir -p "${STATE_DIR}"

	install -m 0600 "${BOOTSTRAP_DIR}/awg3.json" "${STATE_DIR}/awg3.json"
	install -m 0600 "${BOOTSTRAP_DIR}/secrets.json" "${STATE_DIR}/secrets.json"

	if ! /gateway --mode validate --config "${STATE_DIR}/awg3.json" --secrets "${STATE_DIR}/secrets.json" >/dev/null 2>&1; then
		echo "bootstrap config validation failed" >&2
		rm -rf "${STATE_DIR}"
		exit 1
	fi

	mkdir -p "${CONFIG_DIR}"
	if [ -e "${CONFIG_DIR}/awg3.json" ] || [ -e "${CONFIG_DIR}/secrets.json" ]; then
		echo "existing config state detected unexpectedly" >&2
		rm -rf "${STATE_DIR}"
		exit 1
	fi

	mv "${STATE_DIR}/awg3.json" "${CONFIG_DIR}/awg3.json"
	if ! mv "${STATE_DIR}/secrets.json" "${CONFIG_DIR}/secrets.json"; then
		rm -f "${CONFIG_DIR}/awg3.json"
		rm -rf "${STATE_DIR}"
		exit 1
	fi

	if [ "$(stat -c '%a' "${CONFIG_DIR}/awg3.json")" != "600" ] || [ "$(stat -c '%a' "${CONFIG_DIR}/secrets.json")" != "600" ]; then
		echo "bootstrap copied files must be mode 0600" >&2
		rm -f "${CONFIG_DIR}/awg3.json" "${CONFIG_DIR}/secrets.json"
		rm -rf "${STATE_DIR}"
		exit 1
	fi

	rm -rf "${STATE_DIR}"
}

if has_valid_committed_state; then
	exec /gateway "$@"
fi

bootstrap_first_run
exec /gateway "$@"
