package repomap

import (
	"encoding/gob"
	"os"
	"path/filepath"
)

const storeVersion = 1

const indexDir = ".codient/index"
const repomapFile = "repomap.gob"

func repomapPath(workspace string) string {
	return filepath.Join(workspace, indexDir, repomapFile)
}

type fileEntry struct {
	ModUnixNano int64
	Tags        []Tag
}

type storedRepoMap struct {
	Version int
	Files   map[string]fileEntry // rel path -> entry
}

func loadStore(workspace string) (map[string]fileEntry, error) {
	path := repomapPath(workspace)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var s storedRepoMap
	if err := gob.NewDecoder(f).Decode(&s); err != nil {
		return nil, nil
	}
	if s.Version != storeVersion || s.Files == nil {
		return nil, nil
	}
	return s.Files, nil
}

func saveStore(workspace string, files map[string]fileEntry) error {
	dir := filepath.Join(workspace, indexDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := repomapPath(workspace)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	s := storedRepoMap{Version: storeVersion, Files: files}
	if err := gob.NewEncoder(f).Encode(&s); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
