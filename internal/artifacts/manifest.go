package artifacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type Manifest struct {
	Artifacts []Artifact `json:"artifacts"`
}

type Artifact struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Variant         string `json:"variant,omitempty"`
	SourceRepo      string `json:"source_repo"`
	SourceCommit    string `json:"source_commit"`
	BuildRecipe     string `json:"build_recipe"`
	ToolchainDigest string `json:"toolchain_digest"`
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var manifest Manifest
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Manifest{}, errors.New("trailing json data not allowed")
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "" || artifact.Path == "" || artifact.SHA256 == "" || artifact.OS == "" || artifact.Arch == "" {
			return Manifest{}, errors.New("artifact manifest entry requires name, path, sha256, os and arch")
		}
	}
	return manifest, nil
}

func (m Manifest) ArtifactByPath(path string) (Artifact, bool) {
	for _, artifact := range m.Artifacts {
		if artifact.Path == path {
			return artifact, true
		}
	}
	return Artifact{}, false
}
