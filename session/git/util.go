package git

import (
	"os/exec"
)

// IsGitRepo checks if the given path is within a git repository. Sessions do not
// require one — this only decides whether a directory has changes to show.
func IsGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	return cmd.Run() == nil
}
