package session

import (
	"os"
	"path/filepath"
)

// composeFileNames are the docker compose manifest names looked for at a
// session's working directory, in the same priority order docker compose
// itself uses.
var composeFileNames = []string{
	"docker-compose.yml",
	"docker-compose.yaml",
	"compose.yml",
	"compose.yaml",
}

// DetectComposeFile reports whether a docker compose manifest sits at the
// root of path. No subdirectories are searched — a session's Docker tab is
// about the project at its own working directory, not whatever a monorepo
// might keep further down.
func DetectComposeFile(path string) (string, bool) {
	for _, name := range composeFileNames {
		full := filepath.Join(path, name)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full, true
		}
	}
	return "", false
}
