package app

import (
	"claude-squad/session"
	"os"
	"sort"
	"strings"
)

// homePrefix is where the directory field starts. Sessions may live anywhere, so
// completion is anchored at the developer's home rather than at wherever the
// coordinator happened to be launched.
const homePrefix = "~/"

// splitDirInput splits a partially typed path into the directory being listed
// and the fragment being completed. A fragment with no slash is completed
// inside the home directory.
func splitDirInput(input string) (dir string, fragment string) {
	if idx := strings.LastIndex(input, "/"); idx >= 0 {
		return input[:idx+1], input[idx+1:]
	}
	return homePrefix, input
}

// completeDirs returns the directory candidates for a partially typed path,
// each keeping the prefix the developer typed (so "~/" stays "~/") and ending
// in a slash so the next segment can be typed straight away. Files are ignored:
// a session works in a directory.
func completeDirs(input string) []string {
	dir, fragment := splitDirInput(input)

	resolved, err := session.ResolvePath(dir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Hidden directories only show up once the developer asks for them.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(fragment, ".") {
			continue
		}
		if !strings.HasPrefix(name, fragment) {
			continue
		}
		out = append(out, dir+name+"/")
	}
	sort.Strings(out)
	return out
}

// commonPrefix returns the longest string all candidates start with.
func commonPrefix(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	prefix := candidates[0]
	for _, c := range candidates[1:] {
		for !strings.HasPrefix(c, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// completePath advances the typed path by one Tab press. It fills in as much as
// every candidate agrees on; when there is nothing left to agree on it cycles
// through the candidates so the developer can pick one without leaving the
// keyboard. next is the index to cycle from.
//
// Returns the new input, the index to use on the following press, and whether
// this press cycled (in which case the caller must keep the candidate list, or
// the cycle would collapse to whatever is inside the chosen directory).
func completePath(input string, candidates []string, next int) (result string, following int, cycled bool) {
	if len(candidates) == 0 {
		return input, 0, false
	}
	if len(candidates) == 1 {
		return candidates[0], 0, false
	}

	// Fill in the shared part first — that is free progress, no guessing.
	if prefix := commonPrefix(candidates); len(prefix) > len(input) {
		return prefix, 0, false
	}

	// Nothing left to agree on: cycle.
	if next < 0 || next >= len(candidates) {
		next = 0
	}
	return candidates[next], (next + 1) % len(candidates), true
}
