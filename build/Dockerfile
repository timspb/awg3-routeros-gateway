ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

FROM --platform=$TARGETPLATFORM golang:1.23-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG GATEWAY_SOURCE_COMMIT
ARG AWG_GO_REPO=https://github.com/amnezia-vpn/amneziawg-go.git
ARG AWG_GO_COMMIT=cf9d2dd202821301f7039093b0a1b3d4b574c47c
ARG AWG_TOOLS_REPO=https://github.com/amnezia-vpn/amneziawg-tools.git
ARG AWG_TOOLS_COMMIT=d09ecc38425082e472368dd2bf8c4c42d10cae03
ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /src
COPY . .
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates git build-essential gcc make pkg-config iproute2 procps file binutils; \
	rm -rf /var/lib/apt/lists/*
RUN set -eux; \
	test -n "${GATEWAY_SOURCE_COMMIT}"; \
	git clone --quiet "${AWG_GO_REPO}" /tmp/amneziawg-go; \
	git -C /tmp/amneziawg-go checkout --quiet "${AWG_GO_COMMIT}"; \
	test "$(git -C /tmp/amneziawg-go rev-parse HEAD)" = "${AWG_GO_COMMIT}"; \
	test -z "$(git -C /tmp/amneziawg-go status --porcelain)"; \
	test "$(git -C /tmp/amneziawg-go remote get-url origin)" = "${AWG_GO_REPO}"; \
	git clone --quiet "${AWG_TOOLS_REPO}" /tmp/amneziawg-tools; \
	git -C /tmp/amneziawg-tools checkout --quiet "${AWG_TOOLS_COMMIT}"; \
	test "$(git -C /tmp/amneziawg-tools rev-parse HEAD)" = "${AWG_TOOLS_COMMIT}"; \
	test -z "$(git -C /tmp/amneziawg-tools status --porcelain)"; \
	test "$(git -C /tmp/amneziawg-tools remote get-url origin)" = "${AWG_TOOLS_REPO}"; \
	mkdir -p /rootfs/etc/awg3 /rootfs/usr/local/bin /rootfs/usr/sbin; \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildCommit=${GATEWAY_SOURCE_COMMIT}" -o /rootfs/gateway ./cmd/gateway; \
	( cd /tmp/amneziawg-go && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rootfs/usr/local/bin/amneziawg-go . ); \
	make -C /tmp/amneziawg-tools/src clean wg; \
	install -D -m 0755 /tmp/amneziawg-tools/src/wg /rootfs/usr/local/bin/awg; \
	gcc -O2 -Wall -Wextra -std=gnu11 -D_GNU_SOURCE -I /tmp/amneziawg-tools/src \
		-o /rootfs/usr/local/bin/awg3-parser-validate \
		/src/build/official_parser_validator.c \
		/tmp/amneziawg-tools/src/config.c \
		/tmp/amneziawg-tools/src/encoding.c \
		/tmp/amneziawg-tools/src/curve25519.c \
		/tmp/amneziawg-tools/src/type.c; \
	install -D -m 0755 /usr/sbin/ip /rootfs/usr/sbin/ip; \
	install -D -m 0755 /usr/sbin/sysctl /rootfs/usr/sbin/sysctl; \
	case "${TARGETARCH}/${TARGETVARIANT}" in \
		amd64/|amd64) expected_class="ELF64"; expected_machine="Advanced Micro Devices X86-64"; expected_flags="";; \
		arm64/|arm64|arm64/v8) expected_class="ELF64"; expected_machine="AArch64"; expected_flags="";; \
		arm/v5) expected_class="ELF32"; expected_machine="ARM"; expected_flags="Version5 EABI";; \
		*) echo "unsupported target platform ${TARGETOS}/${TARGETARCH}/${TARGETVARIANT}" >&2; exit 1;; \
	esac; \
	for bin in /rootfs/gateway /rootfs/usr/local/bin/amneziawg-go /rootfs/usr/local/bin/awg /rootfs/usr/local/bin/awg3-parser-validate /rootfs/usr/sbin/ip /rootfs/usr/sbin/sysctl; do \
		file "$bin"; \
		readelf -h "$bin" > "/tmp/$(basename "$bin").readelf"; \
		grep -q "Class:[[:space:]]*${expected_class}" "/tmp/$(basename "$bin").readelf"; \
		grep -q "Machine:[[:space:]]*${expected_machine}" "/tmp/$(basename "$bin").readelf"; \
		if [ -n "${expected_flags}" ]; then grep -q "Flags:.*${expected_flags}" "/tmp/$(basename "$bin").readelf"; fi; \
	done; \
	toolchain_digest="$( (go version; gcc --version | head -n 1; make --version | head -n 1; git --version; dpkg-query -W -f='${Package}=${Version}\n' iproute2 procps binutils) | sha256sum | awk '{print $1}' )"; \
	build_recipe_digest="$(sha256sum /src/build/Dockerfile /src/build/bake.hcl /src/build/official_parser_validator.c /src/build/verify-buildx.ps1 /src/cmd/artifactgen/main.go /src/internal/artifacts/manifest.go /src/internal/artifacts/verify.go | sha256sum | awk '{print $1}' )"; \
	go run ./cmd/artifactgen \
		--output /rootfs/etc/awg3/runtime-artifacts.json \
		--os "${TARGETOS}" \
		--arch "${TARGETARCH}" \
		--variant "${TARGETVARIANT}" \
		--self-repo "file://awg3routerosgateway" \
		--self-commit "${GATEWAY_SOURCE_COMMIT}" \
		--go-repo "${AWG_GO_REPO}" \
		--go-commit "${AWG_GO_COMMIT}" \
		--tools-repo "${AWG_TOOLS_REPO}" \
		--tools-commit "${AWG_TOOLS_COMMIT}" \
		--pkg-repo "debian:bookworm-slim" \
		--pkg-commit "$(dpkg-query -W -f='${Version}' iproute2) $(dpkg-query -W -f='${Version}' procps) $(dpkg-query -W -f='${Version}' binutils)" \
		--build-recipe "${build_recipe_digest}" \
		--toolchain-digest "${toolchain_digest}" \
		--gateway /gateway \
		--gateway-source /rootfs/gateway \
		--runtime /usr/local/bin/amneziawg-go \
		--runtime-source /rootfs/usr/local/bin/amneziawg-go \
		--tools /usr/local/bin/awg \
		--tools-source /rootfs/usr/local/bin/awg \
		--parser /usr/local/bin/awg3-parser-validate \
		--parser-source /rootfs/usr/local/bin/awg3-parser-validate \
		--ip /usr/sbin/ip \
		--ip-source /rootfs/usr/sbin/ip \
		--sysctl /usr/sbin/sysctl \
		--sysctl-source /rootfs/usr/sbin/sysctl

FROM --platform=$TARGETPLATFORM golang:1.23-bookworm AS test
ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /src
COPY . .
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates git build-essential gcc make pkg-config iproute2 procps; \
	rm -rf /var/lib/apt/lists/*
RUN set -eux; \
	go test ./...; \
	go test -tags integration ./...; \
	go test -tags integration ./internal/awgofficial -run TestOfficialSetconfShowconfDumpRoundTrip -v -count=1

FROM --platform=$TARGETPLATFORM debian:bookworm-slim AS runtime
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates iproute2 procps; \
	rm -rf /var/lib/apt/lists/*
COPY --from=build /rootfs/gateway /gateway
COPY --from=build /rootfs/usr/local/bin/amneziawg-go /usr/local/bin/amneziawg-go
COPY --from=build /rootfs/usr/local/bin/awg /usr/local/bin/awg
COPY --from=build /rootfs/usr/local/bin/awg3-parser-validate /usr/local/bin/awg3-parser-validate
COPY --from=build /rootfs/usr/sbin/ip /usr/sbin/ip
COPY --from=build /rootfs/usr/sbin/sysctl /usr/sbin/sysctl
COPY --from=build /rootfs/etc/awg3/runtime-artifacts.json /etc/awg3/runtime-artifacts.json
ENTRYPOINT ["/gateway"]

FROM runtime AS smoke
COPY build/smoke/ /smoke/
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends file; \
	rm -rf /var/lib/apt/lists/*; \
	file /gateway /usr/local/bin/amneziawg-go /usr/local/bin/awg /usr/local/bin/awg3-parser-validate /usr/sbin/ip /usr/sbin/sysctl; \
	/gateway --mode validate --config /smoke/awg3.json --secrets /smoke/secrets.json
