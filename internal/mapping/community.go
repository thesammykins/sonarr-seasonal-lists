package mapping

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultMappingURL = "https://raw.githubusercontent.com/shinkro/community-mapping/main/tvdb-mal.yaml"

type tvdbMappingFile struct {
	AnimeMap []TvdbEntry `yaml:"AnimeMap"`
}

type TvdbEntry struct {
	MALID  int `yaml:"malid"`
	TVDBID int `yaml:"tvdbid"`
}

type CommunityMapping struct {
	data map[int]int
}

// LoadCommunityMapping reads a YAML TVDB-to-MAL mapping file. If the file
// does not exist, it downloads the latest community mapping from GitHub.
func LoadCommunityMapping(path string) (*CommunityMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read community mapping: %w", err)
		}
		slog.Info("downloading community mapping", "url", defaultMappingURL)
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(defaultMappingURL)
		if err != nil {
			return nil, fmt.Errorf("download community mapping: %w", err)
		}
		defer resp.Body.Close()
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read community mapping response: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			slog.Warn("could not create mapping directory", "path", filepath.Dir(path), "error", err)
		} else if err := os.WriteFile(path, data, 0o600); err != nil {
			slog.Warn("could not cache mapping file", "path", path, "error", err)
		}
	}

	var mf tvdbMappingFile
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parse community mapping: %w", err)
	}

	cm := &CommunityMapping{data: make(map[int]int, len(mf.AnimeMap))}
	for _, e := range mf.AnimeMap {
		if e.MALID > 0 && e.TVDBID > 0 {
			cm.data[e.MALID] = e.TVDBID
		}
	}
	slog.Info("loaded community mapping", "entries", len(cm.data), "path", path)
	return cm, nil
}

func (m *CommunityMapping) Lookup(malID int) (int, bool) {
	tvdbID, ok := m.data[malID]
	return tvdbID, ok
}
