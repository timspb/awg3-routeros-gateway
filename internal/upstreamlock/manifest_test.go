package upstreamlock

import (
	"path/filepath"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	path := filepath.Join("..", "..", "build", "upstream-lock.json")
	manifest, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	goSrc, ok := manifest.Source("amneziawg-go")
	if !ok {
		t.Fatalf("missing amneziawg-go source")
	}
	if goSrc.Commit != "cf9d2dd202821301f7039093b0a1b3d4b574c47c" {
		t.Fatalf("unexpected go commit: %s", goSrc.Commit)
	}
	toolsSrc, ok := manifest.Source("amneziawg-tools")
	if !ok {
		t.Fatalf("missing amneziawg-tools source")
	}
	if toolsSrc.Commit != "d09ecc38425082e472368dd2bf8c4c42d10cae03" {
		t.Fatalf("unexpected tools commit: %s", toolsSrc.Commit)
	}
}
