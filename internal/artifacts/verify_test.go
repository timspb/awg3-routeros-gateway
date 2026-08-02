package artifacts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyManifestPassesForMatchingArtifact(t *testing.T) {
	path, hash := writeSyntheticELF(t, runtime.GOARCH)
	manifest := Manifest{
		Artifacts: []Artifact{{
			Name:            "gateway",
			Path:            path,
			SHA256:          hash,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			Variant:         "",
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}},
	}
	if err := VerifyManifest(manifest, path); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifyManifestRejectsWrongHash(t *testing.T) {
	path, _ := writeSyntheticELF(t, runtime.GOARCH)
	manifest := Manifest{
		Artifacts: []Artifact{{
			Name:            "gateway",
			Path:            path,
			SHA256:          stringsRepeat("0", 64),
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}},
	}
	if err := VerifyManifest(manifest, path); err == nil {
		t.Fatalf("expected hash mismatch to fail")
	}
}

func TestVerifyManifestRejectsMissingFileDirAndSymlink(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if err := VerifyManifest(Manifest{Artifacts: []Artifact{{
		Name:            "missing",
		Path:            missing,
		SHA256:          stringsRepeat("0", 64),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		SourceRepo:      "repo",
		SourceCommit:    "commit",
		BuildRecipe:     "recipe",
		ToolchainDigest: "toolchain",
	}}}, missing); err == nil {
		t.Fatalf("expected missing file to fail")
	}
	if err := VerifyManifest(Manifest{Artifacts: []Artifact{{
		Name:            "dir",
		Path:            dir,
		SHA256:          stringsRepeat("0", 64),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		SourceRepo:      "repo",
		SourceCommit:    "commit",
		BuildRecipe:     "recipe",
		ToolchainDigest: "toolchain",
	}}}, dir); err == nil {
		t.Fatalf("expected directory to fail")
	}
	target, hash := writeSyntheticELF(t, runtime.GOARCH)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err == nil {
		if err := VerifyManifest(Manifest{Artifacts: []Artifact{{
			Name:            "link",
			Path:            link,
			SHA256:          hash,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}}}, link); err == nil {
			t.Fatalf("expected symlink to fail")
		}
	}
}

func TestVerifyManifestRejectsWrongPlatformAndArchitecture(t *testing.T) {
	path, hash := writeSyntheticELF(t, runtime.GOARCH)
	wrongPlatform := Manifest{
		Artifacts: []Artifact{{
			Name:            "gateway",
			Path:            path,
			SHA256:          hash,
			OS:              "plan9",
			Arch:            runtime.GOARCH,
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}},
	}
	if err := VerifyManifest(wrongPlatform, path); err == nil {
		t.Fatalf("expected wrong platform to fail")
	}
	path2, hash2 := writeSyntheticELFWithMachine(t, runtime.GOARCH, wrongMachineFor(runtime.GOARCH))
	wrongArch := Manifest{
		Artifacts: []Artifact{{
			Name:            "gateway",
			Path:            path2,
			SHA256:          hash2,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}},
	}
	if err := VerifyManifest(wrongArch, path2); err == nil {
		t.Fatalf("expected wrong architecture to fail")
	}
}

func TestVerifyManifestRejectsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit is not meaningful on windows")
	}
	path, hash := writeSyntheticELF(t, runtime.GOARCH)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	manifest := Manifest{
		Artifacts: []Artifact{{
			Name:            "gateway",
			Path:            path,
			SHA256:          hash,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			SourceRepo:      "repo",
			SourceCommit:    "commit",
			BuildRecipe:     "recipe",
			ToolchainDigest: "toolchain",
		}},
	}
	if err := VerifyManifest(manifest, path); err == nil {
		t.Fatalf("expected non-executable artifact to fail")
	}
}

func TestManifestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	data := []byte(`{"artifacts":[{"name":"gateway","path":"/gateway","sha256":"` + stringsRepeat("1", 64) + `","os":"linux","arch":"amd64","source_repo":"repo","source_commit":"commit","build_recipe":"recipe","toolchain_digest":"toolchain"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	manifest, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if _, ok := manifest.ArtifactByPath("/gateway"); !ok {
		t.Fatalf("expected artifact lookup")
	}
}

func writeSyntheticELF(t *testing.T, arch string) (string, string) {
	t.Helper()
	return writeSyntheticELFWithMachine(t, arch, elfMachineForArch(arch))
}

func writeSyntheticELFWithMachine(t *testing.T, arch string, machine uint16) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.bin")
	var data []byte
	switch arch {
	case "amd64", "arm64":
		data = makeELF64(machine)
	default:
		data = makeELF32(machine)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("write fake elf failed: %v", err)
	}
	sum := sha256.Sum256(data)
	return path, hex.EncodeToString(sum[:])
}

func makeELF64(machine uint16) []byte {
	data := make([]byte, 64)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4] = 2
	data[5] = 1
	data[6] = 1
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint64(data[24:], 0)
	binary.LittleEndian.PutUint64(data[32:], 0)
	binary.LittleEndian.PutUint32(data[48:], 0)
	binary.LittleEndian.PutUint16(data[52:], 64)
	binary.LittleEndian.PutUint16(data[54:], 0)
	binary.LittleEndian.PutUint16(data[56:], 0)
	binary.LittleEndian.PutUint16(data[58:], 64)
	binary.LittleEndian.PutUint16(data[60:], 0)
	binary.LittleEndian.PutUint16(data[62:], 0)
	return data
}

func makeELF32(machine uint16) []byte {
	data := make([]byte, 52)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4] = 1
	data[5] = 1
	data[6] = 1
	binary.LittleEndian.PutUint16(data[16:], 2)
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[24:], 0)
	binary.LittleEndian.PutUint32(data[28:], 0)
	binary.LittleEndian.PutUint32(data[36:], 0)
	binary.LittleEndian.PutUint16(data[40:], 52)
	binary.LittleEndian.PutUint16(data[42:], 0)
	binary.LittleEndian.PutUint16(data[44:], 0)
	binary.LittleEndian.PutUint16(data[46:], 52)
	binary.LittleEndian.PutUint16(data[48:], 0)
	binary.LittleEndian.PutUint16(data[50:], 0)
	return data
}

func elfMachineForArch(arch string) uint16 {
	switch arch {
	case "amd64":
		return 62
	case "arm64":
		return 183
	default:
		return 40
	}
}

func wrongMachineFor(arch string) uint16 {
	switch arch {
	case "amd64":
		return 183
	case "arm64":
		return 62
	case "arm":
		return 62
	default:
		return 0
	}
}

func stringsRepeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := ""
	for range make([]struct{}, n) {
		out += s
	}
	return out
}
