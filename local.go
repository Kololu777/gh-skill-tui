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

// reconcileMergedOutsideCopies promotes an outside copy when the configured
// source now contains the exact tree that an import PR would have created for
// it. This is intentionally content-based: an open/unmerged PR is absent from
// the source revision, while an unrelated same-name skill with different
// contents must remain an outside collision.
//
// The promotion is model-local and read-only. The installed SKILL.md is not
// rewritten merely by opening the TUI; on every scan the source snapshot is
// used to reconstruct the effective tracking metadata.
func (m model) reconcileMergedOutsideCopies(targets []scanTarget) {
	if len(m.skills) == 0 || len(m.blobShas) == 0 {
		return
	}
	byDest := make(map[string]skill, len(m.skills))
	for _, s := range m.skills {
		byDest[s.Dir()] = s
	}
	for ti := range targets {
		for si := range targets[ti].Skills {
			inst := &targets[ti].Skills[si]
			if m.copyFromConfiguredSource(*inst) || !validSkillPath(inst.Name) {
				continue
			}
			s, ok := byDest[path.Join("skills", path.Clean(inst.Name))]
			if !ok {
				continue
			}
			key := m.skillKey(s)
			if !sameFileSet(m.sourceFilesFor(key), inst.FileShas) {
				continue
			}

			inst.Class = classManaged
			inst.TreeSha = m.treeShas[key]
			inst.Ref = m.ref
			if m.sourceLocal {
				inst.RepoSlug = ""
				inst.GhPath = ""
				inst.LocalPath = key
			} else {
				inst.RepoSlug = repoSlug(m.cfg.Source)
				inst.GhPath = s.Dir()
				inst.LocalPath = ""
			}
		}
	}
}

func sameFileSet(source, installed map[string]string) bool {
	if len(source) == 0 || len(source) != len(installed) {
		return false
	}
	for rel, sha := range source {
		if installed[rel] != sha {
			return false
		}
	}
	return true
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

func validSkillPath(value string) bool {
	value = path.Clean(strings.TrimSpace(value))
	if value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if !validSkillName(part) {
			return false
		}
	}
	return true
}

// autoDestDir mirrors the installed tree under the Agent Skills source tree.
// An installed "sakuramoti/foo" therefore becomes
// "skills/sakuramoti/foo" in private-skills. Git creates missing parent
// directories when the PR tree is written; empty directories are never
// committed separately.
func (m model) autoDestDir(cp localCopy) (string, error) {
	rel := path.Clean(strings.TrimSpace(cp.Inst.Name))
	if !validSkillPath(rel) {
		return "", fmt.Errorf("%s: invalid skill path for the source", cp.Inst.Name)
	}
	dest := path.Join("skills", rel)
	if m.sourceHasSkillDir(dest) {
		return "", fmt.Errorf("%s: destination already exists in source: %s", cp.Inst.Name, dest)
	}
	return dest, nil
}
