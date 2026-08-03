ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm@sha256:ea341baa9bd5ba6784f6d7161ace70544349a6242d54d34a0fbfd2c4d51c9d58 AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG GATEWAY_SOURCE_COMMIT
ARG AWG_GO_REPO=https://github.com/amnezia-vpn/amneziawg-go.git
ARG AWG_GO_COMMIT=cf9d2dd202821301f7039093b0a1b3d4b574c47c
ARG AWG_TOOLS_REPO=https://github.com/amnezia-vpn/amneziawg-tools.git
ARG AWG_TOOLS_COMMIT=d09ecc38425082e472368dd2bf8c4c42d10cae03
ENV DEBIAN_FRONTEND=noninteractive
ENV GOTOOLCHAIN=local
ENV GOFLAGS=-buildvcs=false
WORKDIR /src
COPY . .
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates git build-essential gcc make pkg-config file binutils iproute2 procps; \
	target_pkg_arch=amd64; \
	goarch=amd64; \
	goarm=; \
	cc=gcc; \
	case "${TARGETARCH}/${TARGETVARIANT}" in \
		amd64/|amd64) ;; \
		arm64/|arm64|arm64/v8) \
			target_pkg_arch=arm64; \
			goarch=arm64; \
			cc=aarch64-linux-gnu-gcc; \
			dpkg --add-architecture arm64; \
			apt-get update; \
			apt-get install -y --no-install-recommends gcc-aarch64-linux-gnu binutils-aarch64-linux-gnu libc6-dev-arm64-cross linux-libc-dev-arm64-cross; \
			;; \
		arm/v5) \
			target_pkg_arch=armel; \
			goarch=arm; \
			goarm=5; \
			cc=arm-linux-gnueabi-gcc; \
			dpkg --add-architecture armel; \
			apt-get update; \
			apt-get install -y --no-install-recommends gcc-arm-linux-gnueabi binutils-arm-linux-gnueabi libc6-dev-armel-cross linux-libc-dev-armel-cross; \
			;; \
		*) echo "unsupported target platform ${TARGETOS}/${TARGETARCH}/${TARGETVARIANT}" >&2; exit 1;; \
	esac; \
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
	if [ -n "${goarm}" ]; then \
		GOOS=linux GOARCH="${goarch}" GOARM="${goarm}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildCommit=${GATEWAY_SOURCE_COMMIT}" -o /rootfs/gateway ./cmd/gateway; \
		( cd /tmp/amneziawg-go && GOOS=linux GOARCH="${goarch}" GOARM="${goarm}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rootfs/usr/local/bin/amneziawg-go . ); \
	else \
		GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.buildCommit=${GATEWAY_SOURCE_COMMIT}" -o /rootfs/gateway ./cmd/gateway; \
		( cd /tmp/amneziawg-go && GOOS=linux GOARCH="${goarch}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /rootfs/usr/local/bin/amneziawg-go . ); \
	fi; \
	make -C /tmp/amneziawg-tools/src clean wg CC="${cc}"; \
	install -D -m 0755 /tmp/amneziawg-tools/src/wg /rootfs/usr/local/bin/awg; \
	"${cc}" -O2 -Wall -Wextra -std=gnu11 -D_GNU_SOURCE -I /tmp/amneziawg-tools/src \
		-o /rootfs/usr/local/bin/awg3-parser-validate \
		/src/build/official_parser_validator.c \
		/tmp/amneziawg-tools/src/config.c \
		/tmp/amneziawg-tools/src/encoding.c \
		/tmp/amneziawg-tools/src/curve25519.c \
		/tmp/amneziawg-tools/src/type.c; \
	if [ "${target_pkg_arch}" = "amd64" ]; then \
		cp /bin/ip /rootfs/usr/sbin/ip; \
		chmod 0755 /rootfs/usr/sbin/ip; \
		install -D -m 0755 /usr/sbin/sysctl /rootfs/usr/sbin/sysctl; \
		ip_version="$(dpkg-query -W -f='${Version}' iproute2)"; \
		procps_version="$(dpkg-query -W -f='${Version}' procps)"; \
	else \
		apt-get download "iproute2:${target_pkg_arch}" "procps:${target_pkg_arch}"; \
		ip_deb="$(ls iproute2_*_${target_pkg_arch}.deb)"; \
		procps_deb="$(ls procps_*_${target_pkg_arch}.deb)"; \
		dpkg-deb -x "${ip_deb}" /tmp/pkgroot-ip; \
		dpkg-deb -x "${procps_deb}" /tmp/pkgroot-procps; \
		ip_src="$(find /tmp/pkgroot-ip -type f -name ip -perm -111 | head -n 1)"; \
		sysctl_src="$(find /tmp/pkgroot-procps -type f -name sysctl -perm -111 | head -n 1)"; \
		test -n "${ip_src}"; \
		test -n "${sysctl_src}"; \
		install -D -m 0755 "${ip_src}" /rootfs/usr/sbin/ip; \
		install -D -m 0755 "${sysctl_src}" /rootfs/usr/sbin/sysctl; \
		ip_version="$(apt-cache madison "iproute2:${target_pkg_arch}" | head -n 1 | awk '{print $3}')"; \
		procps_version="$(apt-cache madison "procps:${target_pkg_arch}" | head -n 1 | awk '{print $3}')"; \
	fi; \
	binutils_version="$(dpkg-query -W -f='${Version}' binutils)"; \
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
	toolchain_digest="$( (go version; "${cc}" --version | head -n 1; make --version | head -n 1; git --version; dpkg-query -W -f='${Package}=${Version}\n' binutils) | sha256sum | awk '{print $1}' )"; \
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
		--sysctl-source /rootfs/usr/sbin/sysctl \
		--pkg-commit "${ip_version} ${procps_version} ${binutils_version}"; \
	go run ./cmd/artifactgen \
		--output /rootfs/etc/awg3/runtime-artifacts-smoke.json \
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
		--build-recipe "${build_recipe_digest}" \
		--toolchain-digest "${toolchain_digest}" \
		--gateway /gateway \
		--gateway-source /rootfs/gateway \
		--runtime /smoke/bin/amneziawg-go \
		--runtime-source /src/build/smoke/bin/amneziawg-go \
		--tools /smoke/bin/awg \
		--tools-source /src/build/smoke/bin/awg \
		--parser /usr/local/bin/awg3-parser-validate \
		--parser-source /rootfs/usr/local/bin/awg3-parser-validate \
		--ip /smoke/bin/ip \
		--ip-source /src/build/smoke/bin/ip \
		--sysctl /smoke/bin/sysctl \
		--sysctl-source /src/build/smoke/bin/sysctl \
		--pkg-commit "${ip_version} ${procps_version} ${binutils_version}"

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm@sha256:ea341baa9bd5ba6784f6d7161ace70544349a6242d54d34a0fbfd2c4d51c9d58 AS test
ENV DEBIAN_FRONTEND=noninteractive
ENV GOTOOLCHAIN=local
ENV GOFLAGS=-buildvcs=false
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

FROM --platform=$TARGETPLATFORM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818 AS runtime
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
	apt-get update; \
	apt-get install -y --no-install-recommends ca-certificates iproute2 procps; \
	rm -f /usr/sbin/ip; \
	cp /bin/ip /usr/sbin/ip; \
	chmod 0755 /usr/sbin/ip; \
	rm -rf /var/lib/apt/lists/*
COPY --from=build /rootfs/gateway /gateway
COPY --from=build /rootfs/usr/local/bin/amneziawg-go /usr/local/bin/amneziawg-go
COPY --from=build /rootfs/usr/local/bin/awg /usr/local/bin/awg
COPY --from=build /rootfs/usr/local/bin/awg3-parser-validate /usr/local/bin/awg3-parser-validate
COPY --from=build /rootfs/etc/awg3/runtime-artifacts.json /etc/awg3/runtime-artifacts.json
COPY build/bootstrap-entrypoint.sh /usr/local/bin/bootstrap-entrypoint
RUN chmod 0755 /usr/local/bin/bootstrap-entrypoint
ENTRYPOINT ["/usr/local/bin/bootstrap-entrypoint"]

FROM runtime AS smoke
COPY build/smoke/ /smoke/
COPY --from=build /rootfs/etc/awg3/runtime-artifacts-smoke.json /etc/awg3/runtime-artifacts-smoke.json
RUN set -eux; \
	chmod 0755 /smoke/bootstrap-boundary.sh; \
	chmod 0600 /smoke/awg3.json /smoke/secrets.json; \
	chmod 0755 /smoke/runtime-smoke.sh /smoke/bin/amneziawg-go /smoke/bin/awg /smoke/bin/ip /smoke/bin/sysctl; \
	apt-get update; \
	apt-get install -y --no-install-recommends curl file; \
	rm -rf /var/lib/apt/lists/*; \
	file /gateway /usr/local/bin/amneziawg-go /usr/local/bin/awg /usr/local/bin/awg3-parser-validate /usr/sbin/ip /usr/sbin/sysctl; \
	/gateway --help >/dev/null; \
	sh -c '/usr/local/bin/amneziawg-go --help >/dev/null 2>&1; rc=$?; if [ "$rc" -ne 0 ] && [ "$rc" -ne 1 ]; then exit "$rc"; fi'; \
	/usr/local/bin/awg --help >/dev/null; \
	/usr/local/bin/awg3-parser-validate /smoke/setconf.conf; \
	/usr/sbin/ip -Version >/dev/null; \
	/usr/sbin/sysctl --version >/dev/null; \
	/smoke/bootstrap-boundary.sh; \
	/smoke/runtime-smoke.sh
