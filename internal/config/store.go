package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	generationsDirName       = ".awg3-generations"
	currentGenerationName    = "current-generation.json"
	keepCommittedGenerations = 2
)

type generationMarker struct {
	Generation  string    `json:"generation"`
	Sequence    int64     `json:"sequence"`
	CommittedAt time.Time `json:"committed_at"`
}

func LoadPair(configPath, secretsPath string) (Pair, error) {
	baseDir := filepath.Dir(configPath)
	if filepath.Dir(secretsPath) != baseDir {
		return Pair{}, errors.New("config and secrets must live in the same directory")
	}
	if pair, err := loadCommittedPair(baseDir); err == nil {
		return pair, nil
	}
	conf, err := readJSONStrict[Config](configPath)
	if err != nil {
		return Pair{}, err
	}
	sec, err := readJSONStrict[Secrets](secretsPath)
	if err != nil {
		return Pair{}, err
	}
	pair := Pair{Config: conf, Secrets: sec}
	return pair, pair.Validate()
}

func RestoreGeneration(configPath, secretsPath, generation string) (Pair, error) {
	baseDir := filepath.Dir(configPath)
	if filepath.Dir(secretsPath) != baseDir {
		return Pair{}, errors.New("config and secrets must live in the same directory")
	}
	if generation == "" {
		return Pair{}, errors.New("generation is required")
	}
	pair, err := loadGenerationPair(baseDir, generation)
	if err != nil {
		return Pair{}, err
	}
	markerPath := filepath.Join(baseDir, generationsDirName, generation, "committed.json")
	marker, err := readJSONStrict[generationMarker](markerPath)
	if err != nil {
		return Pair{}, err
	}
	if err := writeJSONAtomic(filepath.Join(baseDir, currentGenerationName), marker); err != nil {
		return Pair{}, err
	}
	if err := writeJSONAtomic(configPath, pair.Config); err != nil {
		return Pair{}, err
	}
	if err := writeJSONAtomic(secretsPath, pair.Secrets); err != nil {
		return Pair{}, err
	}
	return pair, nil
}

func SavePair(configPath, secretsPath string, pair Pair) error {
	baseDir := filepath.Dir(configPath)
	if filepath.Dir(secretsPath) != baseDir {
		return errors.New("config and secrets must live in the same directory")
	}
	if err := pair.Validate(); err != nil {
		return err
	}
	marker, err := nextMarker(baseDir)
	if err != nil {
		return err
	}
	pair.Config.Generation = marker.Generation
	pair.Secrets.Generation = marker.Generation

	generationDir := filepath.Join(baseDir, generationsDirName, marker.Generation)
	if err := os.MkdirAll(generationDir, 0o755); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(generationDir, "awg3.json"), pair.Config); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(generationDir, "secrets.json"), pair.Secrets); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(generationDir, "committed.json"), marker); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(baseDir, currentGenerationName), marker); err != nil {
		return err
	}
	if err := writeJSONAtomic(configPath, pair.Config); err != nil {
		return err
	}
	if err := writeJSONAtomic(secretsPath, pair.Secrets); err != nil {
		return err
	}
	if err := pruneGenerations(baseDir, marker.Generation); err != nil {
		return err
	}
	return nil
}

func loadCommittedPair(baseDir string) (Pair, error) {
	markerPath := filepath.Join(baseDir, currentGenerationName)
	marker, err := readJSONStrict[generationMarker](markerPath)
	if err != nil {
		return recoverLatestCommittedPair(baseDir)
	}
	if marker.Generation == "" || marker.Sequence <= 0 {
		return recoverLatestCommittedPair(baseDir)
	}
	pair, err := loadGenerationPair(baseDir, marker.Generation)
	if err != nil {
		return recoverLatestCommittedPair(baseDir)
	}
	return pair, nil
}

func recoverLatestCommittedPair(baseDir string) (Pair, error) {
	gensRoot := filepath.Join(baseDir, generationsDirName)
	entries, err := os.ReadDir(gensRoot)
	if err != nil {
		return Pair{}, err
	}
	var chosen *generationMarker
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		markerPath := filepath.Join(gensRoot, entry.Name(), "committed.json")
		marker, err := readJSONStrict[generationMarker](markerPath)
		if err != nil {
			continue
		}
		if marker.Generation == "" {
			continue
		}
		if chosen == nil || marker.Sequence > chosen.Sequence || (marker.Sequence == chosen.Sequence && marker.Generation > chosen.Generation) {
			copyMarker := marker
			chosen = &copyMarker
		}
	}
	if chosen == nil {
		return Pair{}, errors.New("no recoverable committed generation found")
	}
	pair, err := loadGenerationPair(baseDir, chosen.Generation)
	if err != nil {
		return Pair{}, err
	}
	if err := mirrorCommittedPair(baseDir, *chosen, pair); err != nil {
		return Pair{}, err
	}
	return pair, nil
}

func loadGenerationPair(baseDir, gen string) (Pair, error) {
	configPath := filepath.Join(baseDir, generationsDirName, gen, "awg3.json")
	secretsPath := filepath.Join(baseDir, generationsDirName, gen, "secrets.json")
	conf, err := readJSONStrict[Config](configPath)
	if err != nil {
		return Pair{}, err
	}
	sec, err := readJSONStrict[Secrets](secretsPath)
	if err != nil {
		return Pair{}, err
	}
	pair := Pair{Config: conf, Secrets: sec}
	if err := pair.Validate(); err != nil {
		return Pair{}, err
	}
	if pair.Config.Generation != gen || pair.Secrets.Generation != gen {
		return Pair{}, errors.New("generation mismatch in committed pair")
	}
	return pair, nil
}

func mirrorCommittedPair(baseDir string, marker generationMarker, pair Pair) error {
	if err := writeJSONAtomic(filepath.Join(baseDir, currentGenerationName), marker); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(baseDir, "awg3.json"), pair.Config); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(baseDir, "secrets.json"), pair.Secrets); err != nil {
		return err
	}
	return nil
}

func nextMarker(baseDir string) (generationMarker, error) {
	var seq int64 = 1
	if marker, err := readJSONStrict[generationMarker](filepath.Join(baseDir, currentGenerationName)); err == nil && marker.Sequence > 0 {
		seq = marker.Sequence + 1
	} else if marker, err := latestGenerationMarker(baseDir); err == nil && marker.Sequence > 0 {
		seq = marker.Sequence + 1
	}
	gen := fmt.Sprintf("gen-%016d", seq)
	return generationMarker{
		Generation:  gen,
		Sequence:    seq,
		CommittedAt: time.Now().UTC(),
	}, nil
}

func latestGenerationMarker(baseDir string) (generationMarker, error) {
	gensRoot := filepath.Join(baseDir, generationsDirName)
	entries, err := os.ReadDir(gensRoot)
	if err != nil {
		return generationMarker{}, err
	}
	var chosen *generationMarker
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker, err := readJSONStrict[generationMarker](filepath.Join(gensRoot, entry.Name(), "committed.json"))
		if err != nil {
			continue
		}
		if marker.Generation == "" {
			continue
		}
		if chosen == nil || marker.Sequence > chosen.Sequence {
			copyMarker := marker
			chosen = &copyMarker
		}
	}
	if chosen == nil {
		return generationMarker{}, errors.New("no committed generation metadata found")
	}
	return *chosen, nil
}

func pruneGenerations(baseDir, keepGeneration string) error {
	gensRoot := filepath.Join(baseDir, generationsDirName)
	entries, err := os.ReadDir(gensRoot)
	if err != nil {
		return nil
	}
	type item struct {
		gen string
		seq int64
	}
	var items []item
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		marker, err := readJSONStrict[generationMarker](filepath.Join(gensRoot, entry.Name(), "committed.json"))
		if err != nil || marker.Generation == "" {
			continue
		}
		items = append(items, item{gen: marker.Generation, seq: marker.Sequence})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].seq == items[j].seq {
			return items[i].gen > items[j].gen
		}
		return items[i].seq > items[j].seq
	})
	keep := make(map[string]struct{}, keepCommittedGenerations)
	for i, item := range items {
		if i < keepCommittedGenerations || item.gen == keepGeneration {
			keep[item.gen] = struct{}{}
		}
	}
	for _, item := range items {
		if _, ok := keep[item.gen]; ok {
			continue
		}
		_ = os.RemoveAll(filepath.Join(gensRoot, item.gen))
	}
	return nil
}

func readJSONStrict[T any](path string) (T, error) {
	var zero T
	if err := validateSecureFile(path); err != nil {
		return zero, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return zero, errors.New("trailing json data not allowed")
	}
	return out, nil
}

func writeJSONAtomic[T any](path string, value T) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".awg3-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write([]byte{'\n'}); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}

func fingerprint(domain, value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("awg3/" + domain + "/v1:" + value))
	return hex.EncodeToString(sum[:16])
}
