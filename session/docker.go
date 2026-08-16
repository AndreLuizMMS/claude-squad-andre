package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// composeFileNames are the docker compose manifest names looked for first
// in each search directory, in the same priority order docker compose
// itself uses.
var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

// composeSearchDirs are checked in order for a compose file: the session
// root itself, then a "docker/" subfolder — the layout DOXAR keeps its
// compose file in. Only this one extra level, not a general recursive
// search.
var composeSearchDirs = []string{".", "docker"}

// DetectComposeFile reports whether a docker compose manifest can be found
// for a session's working directory. Within each of composeSearchDirs, the
// canonical composeFileNames win first; failing that, any file whose name
// contains "compose" and ends in .yml/.yaml also counts — Cortz and the
// Norven projects suffix an environment onto the name instead
// ("docker-compose.dev.yml", "docker-compose-prod.yml"), which the
// canonical list alone would never match.
func DetectComposeFile(path string) (string, bool) {
	for _, dir := range composeSearchDirs {
		base := filepath.Join(path, dir)
		for _, name := range composeFileNames {
			full := filepath.Join(base, name)
			if info, err := os.Stat(full); err == nil && !info.IsDir() {
				return full, true
			}
		}
		if found, ok := bestComposeMatch(base); ok {
			return found, true
		}
	}
	return "", false
}

// bestComposeMatch scans dir (one level, non-recursive) for any file whose
// name contains "compose" and ends in .yml/.yaml. When more than one such
// file exists, the "dev" variant wins — this panel is for watching what's
// running locally, not staging or prod — otherwise the alphabetically
// first, so the pick is at least deterministic.
func bestComposeMatch(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.Contains(name, "compose") {
			continue
		}
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		matches = append(matches, e.Name())
	}
	if len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	for _, m := range matches {
		if strings.Contains(strings.ToLower(m), "dev") {
			return filepath.Join(dir, m), true
		}
	}
	return filepath.Join(dir, matches[0]), true
}
