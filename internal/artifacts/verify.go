package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"debug/elf"
)

type Verification struct {
	ManifestPath string
	Paths        []string
}

func VerifyAll(manifestPath string, paths ...string) error {
	manifest, err := Load(manifestPath)
	if err != nil {
		return err
	}
	return VerifyManifest(manifest, paths...)
}

func VerifyManifest(manifest Manifest, paths ...string) error {
	for _, path := range paths {
		artifact, ok := manifest.ArtifactByPath(path)
		if !ok {
			return fmt.Errorf("artifact manifest entry missing for %s", path)
		}
		if err := verifyArtifact(artifact); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

func verifyArtifact(artifact Artifact) error {
	info, err := os.Lstat(artifact.Path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink not allowed")
	}
	if !info.Mode().IsRegular() {
		return errors.New("regular file required")
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return errors.New("executable required")
	}
	if runtime.GOOS != artifact.OS || runtime.GOARCH != artifact.Arch {
		return fmt.Errorf("platform mismatch: runtime=%s/%s manifest=%s/%s", runtime.GOOS, runtime.GOARCH, artifact.OS, artifact.Arch)
	}
	sum, err := FileSHA256(artifact.Path)
	if err != nil {
		return err
	}
	if sum != artifact.SHA256 {
		return errors.New("sha256 mismatch")
	}
	isELF, err := isELFArtifact(artifact.Path)
	if err != nil {
		return err
	}
	if isELF {
		f, err := elf.Open(artifact.Path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := verifyELF(f, artifact); err != nil {
			return err
		}
	}
	return nil
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyELF(f *elf.File, artifact Artifact) error {
	switch artifact.Arch {
	case "amd64":
		if f.FileHeader.Machine != elf.EM_X86_64 {
			return errors.New("wrong ELF architecture")
		}
	case "arm64":
		if f.FileHeader.Machine != elf.EM_AARCH64 {
			return errors.New("wrong ELF architecture")
		}
	case "arm":
		if f.FileHeader.Machine != elf.EM_ARM {
			return errors.New("wrong ELF architecture")
		}
	default:
		return fmt.Errorf("unsupported arch %q", artifact.Arch)
	}
	return nil
}

func isELFArtifact(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var magic [4]byte
	n, err := io.ReadFull(f, magic[:])
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	return n == len(magic) && bytes.Equal(magic[:], []byte{0x7f, 'E', 'L', 'F'}), nil
}
