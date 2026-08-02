//go:build integration

package awgofficial

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"awg3routerosgateway/internal/awg3profile"
	"awg3routerosgateway/internal/config"
	"awg3routerosgateway/internal/upstreamlock"
)

func TestOfficialSetconfShowconfDumpRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL integration harness is only wired for windows host runs")
	}

	toolsRoot := ensureOfficialTools(t)
	harness := buildOfficialHarness(t, toolsRoot)

	pair := officialPair()
	text, err := awg3profile.RenderSetconf(pair)
	if err != nil {
		t.Fatalf("render setconf failed: %v", err)
	}
	confPath := writeTempFile(t, "awg3-official.conf", []byte(text))
	out := runHarness(t, harness, confPath)

	showconf, dump := splitHarnessOutput(t, out)
	assertShowconfMatches(t, showconf, pair)
	assertDumpMatches(t, dump, pair)
}

func ensureOfficialTools(t *testing.T) string {
	t.Helper()

	manifest := loadUpstreamManifest(t)
	toolsSrc, ok := manifest.Source("amneziawg-tools")
	if !ok {
		t.Fatalf("upstream manifest missing amneziawg-tools")
	}

	const root = "/tmp/awg3-official-tools"
	if ok, err := verifyOfficialCheckout(root, toolsSrc); err == nil && ok {
		return root + "/src"
	}
	script := strings.Join([]string{
		"set -euo pipefail",
		fmt.Sprintf("rm -rf %s", root),
		fmt.Sprintf("git clone --depth 1 --branch %s %s %s", toolsSrc.Ref, toolsSrc.RepoURL, root),
		fmt.Sprintf("test \"$(git -C %s rev-parse HEAD)\" = %s", root, toolsSrc.Commit),
		fmt.Sprintf("cd %s/src", root),
		"make -j2",
	}, "; ")
	if _, err := runWsl("bash", "-lc", script); err != nil {
		t.Fatalf("build official tools failed: %v", err)
	}
	if ok, err := verifyOfficialCheckout(root, toolsSrc); err != nil || !ok {
		t.Fatalf("official checkout verification failed: %v", err)
	}
	return root + "/src"
}

func buildOfficialHarness(t *testing.T, toolsRoot string) string {
	t.Helper()

	dir := t.TempDir()
	harnessC := filepath.Join(dir, "official_harness.c")
	harnessBin := filepath.Join(dir, "official_harness")

	source := officialHarnessSource()
	if err := os.WriteFile(harnessC, []byte(source), 0o600); err != nil {
		t.Fatalf("write harness failed: %v", err)
	}

	harnessCwsl := wslPath(t, harnessC)
	harnessBinWsl := wslPath(t, harnessBin)
	compile := fmt.Sprintf(
		"set -euo pipefail; gcc -std=c11 -I%s %s %s %s %s %s %s %s %s -o %s",
		shQuote(toolsRoot),
		shQuote(harnessCwsl),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "config.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "curve25519.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "encoding.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "show.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "showconf.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "terminal.o"))),
		shQuote(filepath.ToSlash(filepath.Join(toolsRoot, "type.o"))),
		shQuote(harnessBinWsl),
	)
	t.Logf("compile script:\n%s", compile)
	if out, err := runWsl("bash", "-lc", compile); err != nil {
		t.Fatalf("compile harness failed: %v\n%s", err, string(out))
	}
	return harnessBin
}

func loadUpstreamManifest(t *testing.T) upstreamlock.Manifest {
	t.Helper()
	manifest, err := upstreamlock.Load(filepath.Join("..", "..", "build", "upstream-lock.json"))
	if err != nil {
		t.Fatalf("load upstream manifest failed: %v", err)
	}
	return manifest
}

func verifyOfficialCheckout(root string, src upstreamlock.Source) (bool, error) {
	checks := []string{
		fmt.Sprintf("test -d %s/.git", root),
		fmt.Sprintf("test \"$(git -C %s remote get-url origin)\" = %s", root, shQuote(src.RepoURL)),
		fmt.Sprintf("test \"$(git -C %s rev-parse HEAD)\" = %s", root, src.Commit),
		fmt.Sprintf("test -z \"$(git -C %s status --porcelain)\"", root),
	}
	script := "set -euo pipefail; " + strings.Join(checks, "; ")
	out, err := runWsl("bash", "-lc", script)
	if err != nil {
		return false, fmt.Errorf("%v: %s", err, string(out))
	}
	return true, nil
}

func officialHarnessSource() string {
	return `#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "config.h"
#include "containers.h"
#include "ipc.h"
#include "subcommands.h"

const char *PROG_NAME = "awg";

static struct wgdevice *g_device;

static struct wgallowedip *clone_allowedip(const struct wgallowedip *src) {
	struct wgallowedip *dst = calloc(1, sizeof(*dst));
	if (!dst) {
		perror("calloc");
		exit(9);
	}
	*dst = *src;
	dst->next_allowedip = NULL;
	return dst;
}

static struct wgpeer *clone_peer(const struct wgpeer *src) {
	struct wgpeer *dst = calloc(1, sizeof(*dst));
	struct wgallowedip *tail = NULL;
	if (!dst) {
		perror("calloc");
		exit(9);
	}
	*dst = *src;
	dst->first_allowedip = NULL;
	dst->last_allowedip = NULL;
	dst->next_peer = NULL;
	for (const struct wgallowedip *ip = src->first_allowedip; ip; ip = ip->next_allowedip) {
		struct wgallowedip *copy = clone_allowedip(ip);
		if (!dst->first_allowedip)
			dst->first_allowedip = copy;
		else
			tail->next_allowedip = copy;
		tail = copy;
		dst->last_allowedip = copy;
	}
	return dst;
}

static struct wgdevice *clone_device(const struct wgdevice *src) {
	struct wgdevice *dst = calloc(1, sizeof(*dst));
	struct wgpeer *tail = NULL;
	if (!dst) {
		perror("calloc");
		exit(9);
	}
	*dst = *src;
	dst->first_peer = NULL;
	dst->last_peer = NULL;
	dst->i1 = src->i1 ? strdup(src->i1) : NULL;
	dst->i2 = src->i2 ? strdup(src->i2) : NULL;
	dst->i3 = src->i3 ? strdup(src->i3) : NULL;
	dst->i4 = src->i4 ? strdup(src->i4) : NULL;
	dst->i5 = src->i5 ? strdup(src->i5) : NULL;
	if ((src->i1 && !dst->i1) || (src->i2 && !dst->i2) || (src->i3 && !dst->i3) || (src->i4 && !dst->i4) || (src->i5 && !dst->i5)) {
		perror("strdup");
		exit(9);
	}
	for (const struct wgpeer *peer = src->first_peer; peer; peer = peer->next_peer) {
		struct wgpeer *copy = clone_peer(peer);
		if (!dst->first_peer)
			dst->first_peer = copy;
		else
			tail->next_peer = copy;
		tail = copy;
		dst->last_peer = copy;
	}
	return dst;
}

int ipc_get_device(struct wgdevice **dev, const char *interface) {
	(void)interface;
	*dev = clone_device(g_device);
	return 0;
}

char *ipc_list_devices(void) {
	return NULL;
}

int main(int argc, char **argv) {
	if (argc != 2) {
		fprintf(stderr, "usage: %s <config>\n", argv[0]);
		return 2;
	}

	FILE *f = fopen(argv[1], "r");
	if (!f) {
		perror("fopen");
		return 3;
	}

	struct config_ctx ctx;
	if (!config_read_init(&ctx, false)) {
		fclose(f);
		return 4;
	}

	char *line = NULL;
	size_t cap = 0;
	while (getline(&line, &cap, f) >= 0) {
		if (!config_read_line(&ctx, line)) {
			fprintf(stderr, "configuration parsing error\n");
			free(line);
			fclose(f);
			return 5;
		}
	}
	free(line);
	fclose(f);

	g_device = config_read_finish(&ctx);
	if (!g_device) {
		fprintf(stderr, "invalid configuration\n");
		return 6;
	}

	puts("--SHOWCONF--");
	const char *showconf_argv[] = {"showconf", "awg0"};
	if (showconf_main(2, showconf_argv) != 0) {
		free_wgdevice(g_device);
		return 7;
	}

	puts("--DUMP--");
	const char *show_argv[] = {"show", "awg0", "dump"};
	if (show_main(3, show_argv) != 0) {
		free_wgdevice(g_device);
		return 8;
	}

	free_wgdevice(g_device);
	return 0;
}
`
}

func officialPair() config.Pair {
	return config.Pair{
		Config: config.Config{
			Version:                   "1",
			Generation:                "gen-official-1",
			ListenPort:                51820,
			MTU:                       1380,
			TunnelAddress:             "10.99.99.2/30",
			AllowedIPs:                []string{"10.99.99.0/24", "192.168.50.0/24"},
			Endpoint:                  "213.176.116.165:443",
			Jc:                        4,
			Jmin:                      50,
			Jmax:                      250,
			S1:                        84,
			S2:                        40,
			S3:                        46,
			S4:                        20,
			H1:                        50,
			H2:                        250,
			H3:                        50,
			H4:                        250,
			I1:                        "template-1",
			I2:                        "template-2",
			I3:                        "template-3",
			I4:                        "template-4",
			I5:                        "template-5",
			ContentPaddingAdditionMin: 0,
			ContentPaddingAdditionMax: 32,
			RekeyAfterTimeMin:         110,
			RekeyAfterTimeMax:         130,
			RekeyTimeoutMin:           4,
			RekeyTimeoutMax:           6,
			RejectAfterTimeMin:        175,
			RejectAfterTimeMax:        190,
			KeepaliveTimeoutMin:       9,
			KeepaliveTimeoutMax:       11,
			MaxHandshakeAttemptsMin:   16,
			MaxHandshakeAttemptsMax:   20,
			PersistentKeepaliveMin:    23,
			PersistentKeepaliveMax:    27,
		},
		Secrets: config.Secrets{
			Version:             "1",
			Generation:          "gen-official-1",
			PrivateKey:          testKey(1),
			PeerPublicKey:       testKey(2),
			PresharedKey:        testKey(3),
			HeaderProtectionKey: testKey(4),
			ControlToken:        "token",
		},
	}
}

func splitHarnessOutput(t *testing.T, out []byte) (string, string) {
	t.Helper()

	parts := bytes.SplitN(out, []byte("\n--DUMP--\n"), 2)
	if len(parts) != 2 {
		t.Fatalf("harness output missing dump section:\n%s", string(out))
	}
	showconfParts := bytes.SplitN(parts[0], []byte("\n"), 2)
	if len(showconfParts) != 2 || string(showconfParts[0]) != "--SHOWCONF--" {
		t.Fatalf("harness output missing showconf marker:\n%s", string(out))
	}
	return string(showconfParts[1]), string(parts[1])
}

func assertShowconfMatches(t *testing.T, out string, pair config.Pair) {
	t.Helper()

	got := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		got[key] = value
	}

	expect := map[string]string{
		"PrivateKey":             pair.Secrets.PrivateKey,
		"ListenPort":             fmt.Sprintf("%d", pair.Config.ListenPort),
		"Jc":                     fmt.Sprintf("%d", pair.Config.Jc),
		"Jmin":                   fmt.Sprintf("%d", pair.Config.Jmin),
		"Jmax":                   fmt.Sprintf("%d", pair.Config.Jmax),
		"S1":                     fmt.Sprintf("%d", pair.Config.S1),
		"S2":                     fmt.Sprintf("%d", pair.Config.S2),
		"S3":                     fmt.Sprintf("%d", pair.Config.S3),
		"S4":                     fmt.Sprintf("%d", pair.Config.S4),
		"H1":                     fmt.Sprintf("%d", pair.Config.H1),
		"H2":                     fmt.Sprintf("%d", pair.Config.H2),
		"H3":                     fmt.Sprintf("%d", pair.Config.H3),
		"H4":                     fmt.Sprintf("%d", pair.Config.H4),
		"I1":                     pair.Config.I1,
		"I2":                     pair.Config.I2,
		"I3":                     pair.Config.I3,
		"I4":                     pair.Config.I4,
		"I5":                     pair.Config.I5,
		"HeaderProtectionKey":    pair.Secrets.HeaderProtectionKey,
		"ContentPaddingAddition": fmt.Sprintf("%d-%d", pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax),
		"RekeyAfterTime":         fmt.Sprintf("%d-%d", pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax),
		"RekeyTimeout":           fmt.Sprintf("%d-%d", pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax),
		"RejectAfterTime":        fmt.Sprintf("%d-%d", pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax),
		"KeepaliveTimeout":       fmt.Sprintf("%d-%d", pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax),
		"MaxHandshakeAttempts":   fmt.Sprintf("%d-%d", pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax),
		"PublicKey":              pair.Secrets.PeerPublicKey,
		"PresharedKey":           pair.Secrets.PresharedKey,
		"AllowedIPs":             strings.Join(pair.Config.AllowedIPs, ", "),
		"Endpoint":               pair.Config.Endpoint,
	}
	for key, want := range expect {
		if got[key] != want {
			t.Fatalf("showconf field %q mismatch: got %q want %q\noutput:\n%s", key, got[key], want, out)
		}
	}
	if _, ok := got["MaxHandshakeAttempts"]; !ok {
		t.Fatalf("showconf output missing MaxHandshakeAttempts spelling\n%s", out)
	}
}

func assertDumpMatches(t *testing.T, out string, pair config.Pair) {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		t.Fatalf("empty dump output")
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) < 27 {
		t.Fatalf("unexpected dump field count %d: %v", len(fields), fields)
	}
	if fields[0] != testKey(1) {
		t.Fatalf("dump private key mismatch: %q", fields[0])
	}
	if fields[1] != "(none)" {
		t.Fatalf("dump interface public key should be absent: %q", fields[1])
	}
	if fields[2] != fmt.Sprintf("%d", pair.Config.ListenPort) {
		t.Fatalf("dump listen port mismatch: %q", fields[2])
	}
	if fields[3] != fmt.Sprintf("%d", pair.Config.Jc) {
		t.Fatalf("dump jc mismatch: %q", fields[3])
	}
	if fields[14] != pair.Config.I1 {
		t.Fatalf("dump I1 mismatch: %q", fields[14])
	}
	if fields[18] != pair.Config.I5 {
		t.Fatalf("dump I5 mismatch: %q", fields[18])
	}
	if fields[19] != testKey(4) {
		t.Fatalf("dump header protection key mismatch: %q", fields[19])
	}
	if !strings.Contains(out, "23-27") {
		t.Fatalf("dump output missing keepalive range:\n%s", out)
	}

	if len(lines) < 2 {
		t.Fatalf("expected peer dump line, got only:\n%s", out)
	}
	peerFields := strings.Split(lines[1], "\t")
	if peerFields[0] != testKey(2) {
		t.Fatalf("peer public key mismatch: %q", peerFields[0])
	}
	if peerFields[1] != testKey(3) {
		t.Fatalf("peer preshared key mismatch: %q", peerFields[1])
	}
	if peerFields[2] != pair.Config.Endpoint {
		t.Fatalf("peer endpoint mismatch: %q", peerFields[2])
	}
	if peerFields[3] != strings.Join(pair.Config.AllowedIPs, ",") {
		t.Fatalf("peer allowed ips mismatch: %q", peerFields[3])
	}
}

func testKey(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed + byte(i)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp file failed: %v", err)
	}
	return path
}

func runHarness(t *testing.T, harness, configPath string) []byte {
	t.Helper()

	out, err := runWsl("bash", "-lc", fmt.Sprintf("set -euo pipefail; %s %s", shQuote(wslPath(t, harness)), shQuote(wslPath(t, configPath))))
	if err != nil {
		t.Fatalf("harness execution failed: %v\n%s", err, string(out))
	}
	return out
}

func runWsl(args ...string) ([]byte, error) {
	cmd := exec.Command("wsl", args...)
	return cmd.CombinedOutput()
}

func wslPath(t *testing.T, path string) string {
	t.Helper()
	if strings.HasPrefix(path, "/") {
		return path
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		t.Fatalf("cannot convert non-windows path %q to wsl path", path)
	}
	rest := strings.TrimPrefix(path[len(volume):], `\`)
	rest = strings.ReplaceAll(rest, `\`, `/`)
	return "/mnt/" + strings.ToLower(strings.TrimSuffix(volume, ":")) + "/" + rest
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
