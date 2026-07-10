package main

import (
	"sort"
	"strings"
)

// localOnlyDir is the virtual tree node for installed skills outside the
// configured source. The internal name is retained during migration. The NUL
// byte can never prefix a real repo path, so source filters never match it.
const localOnlyDir = "\x00local-only"

// dirEntry is a row in the tree panel. Path "" is the "(all)" root.
// Entries are the proper ancestors of skill directories; the skill
// directories themselves live in the skills panel.
type dirEntry struct {
	Path  string
	Depth int
}

func (d dirEntry) Label() string {
	if d.Path == "" {
		return "(all)"
	}
	if d.Path == localOnlyDir {
		return "(outside source)"
	}
	parts := strings.Split(d.Path, "/")
	return parts[len(parts)-1] + "/"
}

func buildDirEntries(skills []skill) []dirEntry {
	seen := make(map[string]bool)
	var paths []string
	for _, s := range skills {
		dir := s.Dir()
		for {
			i := strings.LastIndex(dir, "/")
			if i < 0 {
				break
			}
			dir = dir[:i]
			if seen[dir] {
				break
			}
			seen[dir] = true
			paths = append(paths, dir)
		}
	}
	sort.Strings(paths)
	entries := []dirEntry{{Path: "", Depth: 0}}
	for _, p := range paths {
		entries = append(entries, dirEntry{Path: p, Depth: strings.Count(p, "/") + 1})
	}
	return entries
}

func underDir(skillPath, dir string) bool {
	if dir == "" {
		return true
	}
	return strings.HasPrefix(skillPath, dir+"/")
}

// filterSkills returns indices of skills under dir whose path matches query.
func filterSkills(skills []skill, dir, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []int
	for i, s := range skills {
		if !underDir(s.Path, dir) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(s.Path), query) {
			continue
		}
		out = append(out, i)
	}
	return out
}
