package main

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// localCopy is one installed copy outside the configured source, tied to the
// scan target that holds it.
type localCopy struct {
	Scope   string   // "user" | "project"
	Agents  []string // agent shorts sharing the destination
	Display string   // scanTarget.Display, e.g. ".claude/skills"
	Inst    installedSkill
}

type outsideReason int

const (
	reasonNoTracking outsideReason = iota
	reasonExternalSource
	reasonSourcePathMissing
)

func (r outsideReason) String() string {
	switch r {
	case reasonExternalSource:
		return "external source"
	case reasonSourcePathMissing:
		return "source path missing"
	default:
		return "no tracking"
	}
}

// localOnlySkill groups copies of one installed skill that are outside the
// configured source. The historical type name is kept while the surrounding
// implementation is migrated, but the UI calls these entries "outside".
type localOnlySkill struct {
	Key    string // stable selection identity; includes provenance when known
	Name   string // installed relative name, preserving namespaces
	Reason outsideReason
	Copies []localCopy
}

// buildLocalOnly collects installed skills outside the configured source,
// deduped by installed path and provenance across scan targets (both scopes):
// external-source copies, no-tracking/manual copies, and configured-source
// copies whose tracked path no longer exists upstream.
// sourceKeys holds the source skills under the same key convention as
// installedSkill.Key().
func buildLocalOnly(targets []scanTarget, skills []skill, sourceKeys map[string]bool, belongsToSource ...func(installedSkill) bool) []localOnlySkill {
	_ = skills // kept in the signature for callers while the model is migrated
	belongs := func(inst installedSkill) bool { return inst.Class == classManaged }
	if len(belongsToSource) > 0 && belongsToSource[0] != nil {
		belongs = belongsToSource[0]
	}
	index := make(map[string]int)
	var out []localOnlySkill
	for _, t := range targets {
		for _, inst := range t.Skills {
			reason := reasonNoTracking
			key := "untracked|" + inst.Name
			switch inst.Class {
			case classUntracked:
				// Manual/self-authored/unknown-origin copy: outside candidate.
			case classManaged:
				if !belongs(inst) {
					reason = reasonExternalSource
					origin := firstNonEmpty(inst.RepoSlug, inst.LocalPath, "unknown")
					key = "external|" + origin + "|" + firstNonEmpty(inst.GhPath, inst.Name)
					break
				}
				if inst.Key() == "" || sourceKeys[inst.Key()] {
					continue // has a real counterpart in the source
				}
				reason = reasonSourcePathMissing
				key = "missing|" + inst.Key()
			case classForeign:
				reason = reasonExternalSource
				key = "external|" + inst.RepoSlug + "|" + firstNonEmpty(inst.GhPath, inst.Name)
			}
			cp := localCopy{Scope: t.Scope, Agents: t.Agents, Display: t.Display, Inst: inst}
			if i, ok := index[key]; ok {
				out[i].Copies = append(out[i].Copies, cp)
				continue
			}
			index[key] = len(out)
			out = append(out, localOnlySkill{Key: key, Name: inst.Name, Reason: reason, Copies: []localCopy{cp}})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Key < out[j].Key
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// copiesFor returns the entry's copies installed at the given scope.
func (lo localOnlySkill) copiesFor(scope string) []localCopy {
	var out []localCopy
	for _, c := range lo.Copies {
		if c.Scope == scope {
			out = append(out, c)
		}
	}
	return out
}

// filterLocalOnly returns indices of outside entries visible under the
// tree filter, search query, and current scope: an entry shows only when it
// has a copy at that scope (the u key switches). Local entries show under
// "(all)" and the virtual outside node, never under real source dirs.
func filterLocalOnly(entries []localOnlySkill, dir, query, scope string) []int {
	if dir != "" && dir != localOnlyDir {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []int
	for i, lo := range entries {
		if len(lo.copiesFor(scope)) == 0 {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(lo.Name), query) {
			continue
		}
		out = append(out, i)
	}
	return out
}

// candidateParentDirs lists the unique parent directories of the source
// skills ("" = repo root), as destination candidates for a new skill.
func candidateParentDirs(skills []skill) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range skills {
		dir := s.Dir()
		parent := ""
		if i := strings.LastIndex(dir, "/"); i >= 0 {
			parent = dir[:i]
		}
		if !seen[parent] {
			seen[parent] = true
			out = append(out, parent)
		}
	}
	sort.Strings(out)
	return out
}

func validSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		ok := r == '.' || r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// cleanDestDirInput normalizes a hand-typed destination dir to a repo-root
// relative path ("" = repo root). Rooting the Clean forbids escaping the
// repo via "..".
func cleanDestDirInput(input string) (string, error) {
	dir := strings.Trim(path.Clean("/"+strings.TrimSpace(input)), "/")
	if dir == "." {
		dir = ""
	}
	for _, part := range strings.Split(dir, "/") {
		if part == ".." {
			return "", fmt.Errorf("invalid destination dir: %s", input)
		}
	}
	return dir, nil
}

// autoDestDir picks a destination in the source for an outside copy
// without asking, in order of confidence:
//  1. the copy's own tracked path (github-path metadata), when free
//  2. the namespace it is installed under (e.g. sakuramoti/foo -> a source
//     parent dir ending in /sakuramoti)
//  3. the configured new_skill_dir
//  4. a source parent dir named "share"
//  5. the sole parent dir, when the source has exactly one
//
// P (capital) remains the manual picker for anything this gets wrong.
func (m model) autoDestDir(cp localCopy) (string, error) {
	name := path.Base(cp.Inst.Name)
	if !validSkillName(name) {
		return "", fmt.Errorf("%s: invalid skill name for the source", name)
	}
	free := func(dest string) bool { return dest != "" && !m.sourceHasSkillDir(dest) }

	if gp := cp.Inst.GhPath; gp != "" && validSkillName(path.Base(gp)) && free(gp) {
		return gp, nil
	}
	parents := candidateParentDirs(m.skills)
	if i := strings.LastIndex(cp.Inst.Name, "/"); i > 0 {
		ns := cp.Inst.Name[:i]
		for _, par := range parents {
			if par == ns || strings.HasSuffix(par, "/"+ns) {
				if dest := par + "/" + name; free(dest) {
					return dest, nil
				}
			}
		}
	}
	if d := strings.Trim(strings.TrimSpace(fileCfg.NewSkillDir), "/"); d != "" {
		if dest := path.Join(d, name); free(dest) {
			return dest, nil
		}
		return "", fmt.Errorf("%s: already exists under %s (P picks another dir)", name, d)
	}
	for _, par := range parents {
		if path.Base(par) == "share" {
			if dest := path.Join(par, name); free(dest) {
				return dest, nil
			}
			return "", fmt.Errorf("%s: already exists under %s (P picks another dir)", name, par)
		}
	}
	if len(parents) == 1 {
		if dest := path.Join(parents[0], name); free(dest) {
			return dest, nil
		}
	}
	return "", fmt.Errorf("%s: no obvious destination (set new_skill_dir in config, or use P)", name)
}

// addPick is the modal state for PR-ing an outside skill into the source:
// stage 0 picks a destination parent dir (or direct input), stage 1 edits
// the skill name; enter on stage 1 builds an add-PR plan and opens the
// existing PR confirm screen.
type addPick struct {
	Local     localOnlySkill
	Copy      localCopy // PR source, chosen by the panel-2 cursor
	Dirs      []string  // candidate parent dirs; "" = repo root
	Stage     int       // 0 = dir list, 1 = name edit
	Cursor    int       // stage-0 cursor over Dirs + trailing "(direct input)" row
	Direct    bool      // stage 0 switched to direct text input
	DirInput  string
	Dir       string // chosen parent dir (valid once Stage == 1)
	NameInput string
}

func newAddPick(lo localOnlySkill, cp localCopy, skills []skill) *addPick {
	return &addPick{Local: lo, Copy: cp, Dirs: candidateParentDirs(skills)}
}
