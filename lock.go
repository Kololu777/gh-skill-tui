package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The lock file records, per skills root, the state an install operation left
// on disk: the raw blob SHA of every installed file (as gh wrote it, injected
// metadata included) plus the source tree SHA the install came from. That
// separates the two questions check has to answer — "did the user edit this
// copy?" (files vs lock, plain byte comparison, no frontmatter normalization)
// and "did the source move?" (lock tree SHA vs the checked revision's tree
// SHA) — which a direct copy-vs-source comparison cannot tell apart.
//
// Copies without a usable lock entry (pre-lock installs, installs done by gh
// directly) keep the content-authoritative comparison as a fallback.
const lockFileName = ".gh-skill-lock.json"

type lockEntry struct {
	Source  string            `json:"source,omitempty"` // repo slug; empty for local sources
	Path    string            `json:"path"`             // source key: repo-relative dir, or absolute dir for local sources
	Ref     string            `json:"ref,omitempty"`
	TreeSha string            `json:"tree_sha"`
	Files   map[string]string `json:"files"` // rel path -> raw git blob sha as installed on disk
}

type lockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"` // installed rel name -> entry
}

type lockStatus int

const (
	// lockAbsent: no usable entry; fall back to comparing content against
	// the source revision directly.
	lockAbsent lockStatus = iota
	// lockCurrent: files untouched since install and the source has not
	// moved. No content comparison (or SKILL.md fetch) is needed.
	lockCurrent
	// lockOutdated: files untouched since install but the source moved.
	// Updating cannot lose local work.
	lockOutdated
	// lockDirty: files differ from the install-time snapshot (local edits,
	// or an update done outside the TUI leaving the lock stale). Content
	// comparison against the source decides what it means.
	lockDirty
)

// lockStatusFor classifies an installed copy against its lock entry. key is
// the copy's source key under the check's convention (normalized github-path,
// or the absolute source dir for local sources); currentTree is the source's
// tree SHA for that key at the checked revision.
func lockStatusFor(entry lockEntry, ok bool, inst installedSkill, key, currentTree string) lockStatus {
	if !ok || entry.Path != key || entry.TreeSha == "" || len(entry.Files) == 0 || currentTree == "" {
		return lockAbsent
	}
	if !lockFilesMatch(entry, inst) {
		return lockDirty
	}
	if entry.TreeSha != currentTree {
		return lockOutdated
	}
	return lockCurrent
}

// lockFilesMatch reports whether a copy is byte-identical to its lock
// snapshot. FileShas hashes SKILL.md with injected metadata stripped, so the
// raw content is re-hashed here to match what the lock recorded from disk.
func lockFilesMatch(entry lockEntry, inst installedSkill) bool {
	if len(entry.Files) != len(inst.FileShas) {
		return false
	}
	for rel, sha := range entry.Files {
		got, ok := inst.FileShas[rel]
		if !ok {
			return false
		}
		if rel == "SKILL.md" {
			got = gitBlobSha([]byte(inst.SkillMD))
		}
		if got != sha {
			return false
		}
	}
	return true
}

func readLockFile(root string) map[string]lockEntry {
	data, err := os.ReadFile(filepath.Join(root, lockFileName))
	if err != nil {
		return nil
	}
	var lf lockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil
	}
	return lf.Skills
}

func writeLockFile(root string, skills map[string]lockEntry) error {
	path := filepath.Join(root, lockFileName)
	if len(skills) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := json.MarshalIndent(lockFile{Version: 1, Skills: skills}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func writeLockEntry(root, name string, entry lockEntry) error {
	skills := readLockFile(root)
	if skills == nil {
		skills = make(map[string]lockEntry)
	}
	skills[name] = entry
	return writeLockFile(root, skills)
}

// removeLockEntry drops a deleted copy from its root's lock file.
func removeLockEntry(root, name string) error {
	skills := readLockFile(root)
	if _, ok := skills[name]; !ok {
		return nil
	}
	delete(skills, name)
	return writeLockFile(root, skills)
}

// updateLockAfterInstall snapshots the copy a successful plan entry produced.
// It runs after injectLocalTracking so the hashed SKILL.md already carries
// every injected key, and rescans the destination rather than trusting the
// plan: gh decides the final installed layout.
func updateLockAfterInstall(entry planEntry) error {
	if entry.DestAbs == "" {
		return nil // unknown destination (e.g. unrecognized --agent value)
	}
	skills, errMsg := readInstalledSkills(entry.DestAbs, nil, nil)
	if errMsg != "" {
		return fmt.Errorf("rescan %s: %s", entry.DestAbs, errMsg)
	}
	wantDir := skill{Path: entry.Skill}.Dir()
	for _, inst := range skills {
		if entry.SourceSkillAbs != "" {
			if filepath.Clean(inst.LocalPath) != filepath.Clean(entry.SourceSkillAbs) {
				continue
			}
		} else if normalizeSkillDir(inst.GhPath) != wantDir {
			continue
		}
		return writeLockEntry(entry.DestAbs, inst.Name, lockEntryFor(inst, entry.TreeSha))
	}
	return fmt.Errorf("no installed copy of %s found under %s", entry.Skill, entry.DestAbs)
}

func lockEntryFor(inst installedSkill, fallbackTree string) lockEntry {
	files := make(map[string]string, len(inst.FileShas))
	for rel, sha := range inst.FileShas {
		files[rel] = sha
	}
	if inst.SkillMD != "" {
		files["SKILL.md"] = gitBlobSha([]byte(inst.SkillMD))
	}
	entry := lockEntry{
		Ref:     inst.Ref,
		TreeSha: firstNonEmpty(inst.TreeSha, fallbackTree),
		Files:   files,
	}
	if inst.LocalPath != "" {
		entry.Path = filepath.Clean(inst.LocalPath)
	} else {
		entry.Source = inst.RepoSlug
		entry.Path = normalizeSkillDir(inst.GhPath)
	}
	return entry
}
