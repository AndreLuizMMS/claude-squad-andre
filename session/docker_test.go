package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectComposeFile(t *testing.T) {
	names := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(""), 0644))

			path, ok := DetectComposeFile(dir)
			require.True(t, ok)
			require.Equal(t, filepath.Join(dir, name), path)
		})
	}

	t.Run("none present", func(t *testing.T) {
		dir := t.TempDir()
		_, ok := DetectComposeFile(dir)
		require.False(t, ok)
	})

	t.Run("only in subdirectory does not count", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		require.NoError(t, os.MkdirAll(sub, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sub, "docker-compose.yml"), []byte(""), 0644))

		_, ok := DetectComposeFile(dir)
		require.False(t, ok)
	})

	t.Run("priority order: docker-compose.yml wins over compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(""), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(""), 0644))

		path, ok := DetectComposeFile(dir)
		require.True(t, ok)
		require.Equal(t, filepath.Join(dir, "docker-compose.yml"), path)
	})
}
