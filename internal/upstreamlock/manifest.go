package upstreamlock

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type Manifest struct {
	Sources []Source `json:"sources"`
}

type Source struct {
	Name    string `json:"name"`
	RepoURL string `json:"repo_url"`
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
	Purpose string `json:"purpose"`
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
	for _, src := range manifest.Sources {
		if src.Name == "" || src.RepoURL == "" || src.Ref == "" || src.Commit == "" {
			return Manifest{}, errors.New("manifest source requires name, repo_url, ref and commit")
		}
	}
	return manifest, nil
}

func (m Manifest) Source(name string) (Source, bool) {
	for _, src := range m.Sources {
		if src.Name == name {
			return src, true
		}
	}
	return Source{}, false
}
