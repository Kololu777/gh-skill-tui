package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type skillClass int

const (
	classManaged skillClass = iota
	classForeign
	classUntracked
)

func (c skillClass) mark() string {
	switch c {
	case classManaged:
		return "✓"
	case classForeign:
		return "!"
	default:
		return "?"
	}
}

type installedSkill struct {
	Name      string // directory relative to the skills root, e.g. "share/memoc-create"
	Dir       string // absolute directory of this installed copy
	Class     skillClass
	RepoSlug  string
	GhPath    string
	LocalPath string // source dir for --from-local installs
	TreeSha   string // tree sha of the source dir at install time
	Ref       string
	SkillMD   string // raw content of this copy's SKILL.md
	// FileShas holds per-file git blob SHAs of this copy (SKILL.md is
	// hashed with the injected metadata stripped), keyed by path relative
	// to the copy dir. Compared against the source to detect local edits.
	FileShas map[string]string
}

// Key identifies the source skill this install came from: the repo-relative
// dir for GitHub installs, the absolute source dir for local installs.
func (s installedSkill) Key() string {
	if s.GhPath != "" {
		return s.GhPath
	}
	return s.LocalPath
}

type scanTarget struct {
	Scope   string
	Dir     string // absolute
	Display string
	Agents  []string
	Skills  []installedSkill
	Err     string
}

type skillMeta struct {
	Repo       string
	Path       string
	Ref        string
	GhTreeSha  string
	LocalPath  string
	TuiTreeSha string
}

// parseSkillMetadata extracts the source-tracking keys `gh skill install`
// injects into SKILL.md frontmatter (nested under `metadata:`, so matching
// is by trimmed key prefix rather than by indentation).
func parseSkillMetadata(content string) skillMeta {
	var meta skillMeta
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return meta
	}
	for _, line := range lines[1:] {
		t := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if t == "---" {
			break
		}
		for key, dst := range map[string]*string{
			"github-repo:":     &meta.Repo,
			"github-path:":     &meta.Path,
			"github-ref:":      &meta.Ref,
			"github-tree-sha:": &meta.GhTreeSha,
			"local-path:":      &meta.LocalPath,
			"tui-tree-sha:":    &meta.TuiTreeSha,
		} {
			if strings.HasPrefix(t, key) {
				*dst = strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, key)), `"'`)
			}
		}
	}
	return meta
}

// repoSlug normalizes "https://github.com/Owner/Repo" or "Owner/Repo"
// to a lowercase "owner/repo".
func repoSlug(repo string) string {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	parts := []string{}
	for _, p := range strings.Split(repo, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 2 {
		return strings.ToLower(repo)
	}
	return strings.ToLower(parts[len(parts)-2] + "/" + parts[len(parts)-1])
}

// classify decides supply-chain status. GitHub installs are matched against
// allowed OWNER/REPO slugs; --from-local installs (e.g. GitLab clones) are
// managed when their source dir lies under an allowed local root.
func classify(meta skillMeta, allowed []string, allowedRoots []string) skillClass {
	if meta.Repo != "" {
		slug := repoSlug(meta.Repo)
		for _, a := range allowed {
			if slug == a {
				return classManaged
			}
		}
		return classForeign
	}
	if meta.LocalPath != "" {
		for _, root := range allowedRoots {
			if root == "" {
				continue
			}
			if meta.LocalPath == root || strings.HasPrefix(meta.LocalPath, strings.TrimSuffix(root, "/")+"/") {
				return classManaged
			}
		}
	}
	return classUntracked
}

// allowedLocalRoots are local clone roots (e.g. of GitLab repositories)
// whose skills count as managed: GH_SKILL_ALLOWED_LOCAL_ROOTS (or the
// config file's allowed_local_roots) plus the current session's local
// source, if any.
func allowedLocalRoots(source string) []string {
	active := activeFileConfig()
	raw := os.Getenv("GH_SKILL_ALLOWED_LOCAL_ROOTS")
	candidates := strings.Split(raw, ",")
	if raw == "" {
		candidates = active.AllowedLocalRoots
	}
	var roots []string
	for _, r := range candidates {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if abs, err := localRoot(r); err == nil {
			roots = append(roots, abs)
		}
	}
	if isLocalSource(source) {
		if abs, err := localRoot(source); err == nil {
			roots = append(roots, abs)
		}
	}
	return roots
}

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

// scanInstalled inspects every unique agent skills destination (per scope)
// and classifies what is installed there against the allowed sources.
func scanInstalled(installTargets []installTarget, projectRoot string, allowed []string, allowedRoots []string) []scanTarget {
	var targets []scanTarget
	index := make(map[string]int)
	for _, scope := range []string{"user", "project"} {
		for _, p := range installTargets {
			dir := p.dirFor(scope, projectRoot)
			key := scope + "|" + dir
			if i, ok := index[key]; ok {
				targets[i].Agents = append(targets[i].Agents, p.Short)
				continue
			}
			index[key] = len(targets)
			targets = append(targets, scanTarget{
				Scope:   scope,
				Dir:     dir,
				Display: p.displayDirFor(scope),
				Agents:  []string{p.Short},
			})
		}
	}
	for i := range targets {
		targets[i].Skills, targets[i].Err = readInstalledSkills(targets[i].Dir, allowed, allowedRoots)
	}
	return targets
}

// entryIsDir reports whether a directory entry is a directory, following
// symlinks: home-manager installs skills as symlinks into the nix store,
// and those must count as installed copies.
func entryIsDir(root string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(root, e.Name()))
	return err == nil && info.IsDir()
}

// readInstalledSkills scans <root>/*/SKILL.md and <root>/*/*/SKILL.md;
// the second level exists because gh preserves the author namespace
// (e.g. share/memoc-create) when installing from namespaced repos.
// Dot-directories are vendor/internal space (e.g. codex ships its built-in
// skills under ~/.codex/skills/.system) and are not scanned.
func readInstalledSkills(root string, allowed []string, allowedRoots []string) ([]installedSkill, string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ""
		}
		return nil, err.Error()
	}
	var skills []installedSkill
	for _, e1 := range entries {
		if !entryIsDir(root, e1) || strings.HasPrefix(e1.Name(), ".") {
			continue
		}
		if s, ok := readInstalledSkill(root, e1.Name(), allowed, allowedRoots); ok {
			skills = append(skills, s)
			continue
		}
		nestedRoot := filepath.Join(root, e1.Name())
		nested, err := os.ReadDir(nestedRoot)
		if err != nil {
			continue
		}
		for _, e2 := range nested {
			if !entryIsDir(nestedRoot, e2) || strings.HasPrefix(e2.Name(), ".") {
				continue
			}
			if s, ok := readInstalledSkill(root, e1.Name()+"/"+e2.Name(), allowed, allowedRoots); ok {
				skills = append(skills, s)
			}
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, ""
}

func readInstalledSkill(root, rel string, allowed []string, allowedRoots []string) (installedSkill, bool) {
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel), "SKILL.md"))
	if err != nil {
		return installedSkill{}, false
	}
	meta := parseSkillMetadata(string(content))
	treeSha := meta.GhTreeSha
	if meta.TuiTreeSha != "" {
		treeSha = meta.TuiTreeSha
	}
	dir := filepath.Join(root, filepath.FromSlash(rel))
	return installedSkill{
		Name:      rel,
		Dir:       dir,
		Class:     classify(meta, allowed, allowedRoots),
		RepoSlug:  repoSlug(meta.Repo),
		GhPath:    meta.Path,
		LocalPath: meta.LocalPath,
		TreeSha:   treeSha,
		Ref:       strings.TrimPrefix(meta.Ref, "refs/heads/"),
		SkillMD:   string(content),
		FileShas:  hashInstalledFiles(dir),
	}, true
}

// injectedMetaKeys are the machine-written frontmatter keys that must be
// ignored when comparing an installed copy against its source (and stripped
// when sending edits back upstream).
var injectedMetaKeys = []string{
	"github-path:", "github-ref:", "github-repo:", "github-tree-sha:",
	"local-path:", "tui-tree-sha:",
}

// normalizeSkillMD removes the injected metadata keys from SKILL.md
// frontmatter, dropping the `metadata:` mapping entirely if it ends up
// empty, so the result matches the pristine source file.
func normalizeSkillMD(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(strings.TrimRight(lines[i], "\r")) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return content
	}
	injected := func(line string) bool {
		t := strings.TrimSpace(line)
		for _, key := range injectedMetaKeys {
			if strings.HasPrefix(t, key) {
				return true
			}
		}
		return false
	}
	var front []string
	for i := 1; i < end; i++ {
		if !injected(lines[i]) {
			front = append(front, lines[i])
		}
	}
	// drop a `metadata:` line whose children were all injected
	var cleaned []string
	for i := 0; i < len(front); i++ {
		if strings.TrimSpace(front[i]) == "metadata:" {
			hasChild := i+1 < len(front) &&
				strings.TrimLeft(front[i+1], " \t") != front[i+1] &&
				strings.TrimSpace(front[i+1]) != ""
			if !hasChild {
				continue
			}
		}
		cleaned = append(cleaned, front[i])
	}
	out := append([]string{lines[0]}, cleaned...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// splitFrontmatter splits SKILL.md into frontmatter lines (between the ---
// markers) and the remaining body lines.
func splitFrontmatter(content string) (fm []string, body []string, ok bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, lines, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(strings.TrimRight(lines[i], "\r")) == "---" {
			return lines[1:i], lines[i+1:], true
		}
	}
	return nil, lines, false
}

// canonicalSkillMD renders SKILL.md in a form independent of frontmatter
// key order and indentation. Needed because `gh skill install` re-serializes
// frontmatter (alphabetical keys, 4-space nesting), so installed copies are
// never byte-identical to the source even when semantically unchanged.
func canonicalSkillMD(content string) string {
	fm, body, ok := splitFrontmatter(content)
	if !ok {
		return strings.TrimRight(content, "\n")
	}
	type entry struct {
		top      string
		children []string
	}
	var entries []entry
	for _, line := range fm {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if trimmed == "" {
			continue
		}
		indented := strings.TrimLeft(line, " \t") != line
		if indented && len(entries) > 0 {
			entries[len(entries)-1].children = append(entries[len(entries)-1].children, trimmed)
		} else {
			entries = append(entries, entry{top: trimmed})
		}
	}
	var rendered []string
	for _, e := range entries {
		sort.Strings(e.children)
		rendered = append(rendered, e.top+"|"+strings.Join(e.children, "|"))
	}
	sort.Strings(rendered)
	// gh strips blank lines between the frontmatter and the body on
	// install, so leading blanks must not count either
	return strings.Join(rendered, "\n") + "\n===\n" + strings.Trim(strings.Join(body, "\n"), "\n")
}

// skillMDEquivalent reports whether an installed SKILL.md matches the source
// once injected metadata and gh's frontmatter re-serialization are ignored.
func skillMDEquivalent(source, installed string) bool {
	return canonicalSkillMD(source) == canonicalSkillMD(normalizeSkillMD(installed))
}

// reconstructSkillMD produces the content to commit upstream from an edited
// copy: when only the body changed, the source frontmatter (and its blank
// line before the body, which gh strips) is kept verbatim so the PR diff
// carries no re-serialization noise.
func reconstructSkillMD(source, installed string) string {
	if skillMDEquivalent(source, installed) {
		return source
	}
	norm := normalizeSkillMD(installed)
	srcFM, srcBody, okS := splitFrontmatter(source)
	normFM, normBody, okC := splitFrontmatter(norm)
	if !okS || !okC {
		return norm
	}
	fmOnly := func(fm []string) string {
		full := "---\n" + strings.Join(fm, "\n") + "\n---\n"
		return canonicalSkillMD(full)
	}
	if fmOnly(srcFM) != fmOnly(normFM) {
		return norm
	}
	// re-add the source's leading blank lines that gh stripped
	srcLead := 0
	for srcLead < len(srcBody) && strings.TrimSpace(srcBody[srcLead]) == "" {
		srcLead++
	}
	for len(normBody) > 0 && strings.TrimSpace(normBody[0]) == "" {
		normBody = normBody[1:]
	}
	out := append([]string{"---"}, srcFM...)
	out = append(out, "---")
	out = append(out, srcBody[:srcLead]...)
	out = append(out, normBody...)
	return strings.Join(out, "\n")
}

// gitBlobSha computes the git object id of file content (sha1 over
// "blob <len>\0<content>"), matching what git and the GitHub tree API report.
func gitBlobSha(content []byte) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d\x00", len(content))
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// resolveDirSymlink resolves a (possibly symlinked) directory to its real
// path so filepath.WalkDir can descend into it; WalkDir does not follow a
// symlinked root. Home-manager-managed skill dirs are such symlinks.
func resolveDirSymlink(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// junkRel reports copy-relative paths that never count as skill content:
// python bytecode caches, node_modules, and OS litter. Applied to both the
// installed copy and the source listing so edit detection stays symmetric
// and PR plans never ship cache files.
func junkRel(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "__pycache__" || part == "node_modules" || part == ".DS_Store" {
			return true
		}
	}
	return false
}

// hashInstalledFiles hashes every file in an installed copy, normalizing
// SKILL.md so injected metadata does not count as a local edit.
func hashInstalledFiles(dir string) map[string]string {
	dir = resolveDirSymlink(dir)
	shas := make(map[string]string)
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || junkRel(rel) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if d.Name() == "SKILL.md" {
			content = []byte(normalizeSkillMD(string(content)))
		}
		shas[filepath.ToSlash(rel)] = gitBlobSha(content)
		return nil
	})
	return shas
}

// injectLocalTracking records the source tree SHA into a --from-local
// install so staleness can be judged later. gh only writes local-path for
// local installs, so the TUI adds its own key next to it.
func injectLocalTracking(destRoot, sourceSkillAbs, treeSha string) error {
	skills, errMsg := readInstalledSkills(destRoot, nil, nil)
	if errMsg != "" {
		return fmt.Errorf("%s", errMsg)
	}
	for _, s := range skills {
		if s.LocalPath != sourceSkillAbs {
			continue
		}
		file := filepath.Join(destRoot, filepath.FromSlash(s.Name), "SKILL.md")
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		lines := strings.Split(string(content), "\n")
		var out []string
		injected := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "tui-tree-sha:") {
				continue // replace any previous record
			}
			out = append(out, line)
			if !injected && strings.HasPrefix(trimmed, "local-path:") {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				out = append(out, indent+"tui-tree-sha: "+treeSha)
				injected = true
			}
		}
		if !injected {
			return fmt.Errorf("%s: no local-path metadata to attach tui-tree-sha to", file)
		}
		return os.WriteFile(file, []byte(strings.Join(out, "\n")), 0o644)
	}
	return fmt.Errorf("no install matching %s under %s", sourceSkillAbs, destRoot)
}

// deleteEntry is one installed skill directory scheduled for removal.
type deleteEntry struct {
	Skill  string // repo skill dir, e.g. skills/share/memoc-create
	Agents string // agent shorts sharing the destination
	Root   string // skills root the entry lives under
	Dir    string // absolute directory to remove
}

// buildDeletePlan collects the installed copies of the context skills in the
// selected agents' destinations at the current scope. Only tracked installs
// whose metadata path matches the repo skill exactly are deleted — untracked
// or foreign same-named directories are never touched.
func buildDeletePlan(ctx []skill, targets []scanTarget, installTargets []installTarget, marked map[string]bool, scope, sourceRoot string, belongsToSource ...func(installedSkill) bool) []deleteEntry {
	trusted := func(inst installedSkill) bool { return inst.Class == classManaged }
	if len(belongsToSource) > 0 && belongsToSource[0] != nil {
		trusted = belongsToSource[0]
	}
	shorts := make(map[string]bool)
	for _, p := range installTargets {
		if marked[p.Name] {
			shorts[p.Short] = true
		}
	}
	var entries []deleteEntry
	for _, t := range targets {
		if t.Scope != scope {
			continue
		}
		var matched []string
		for _, short := range t.Agents {
			if shorts[short] {
				matched = append(matched, short)
			}
		}
		if len(matched) == 0 {
			continue
		}
		for _, s := range ctx {
			for _, inst := range t.Skills {
				matchesSource := inst.GhPath == s.Dir()
				if inst.LocalPath != "" && sourceRoot != "" {
					want := filepath.Join(sourceRoot, filepath.FromSlash(s.Dir()))
					matchesSource = filepath.Clean(inst.LocalPath) == filepath.Clean(want)
				}
				if !trusted(inst) || !matchesSource {
					continue
				}
				entries = append(entries, deleteEntry{
					Skill:  s.Dir(),
					Agents: strings.Join(matched, "+"),
					Root:   t.Dir,
					Dir:    filepath.Join(t.Dir, filepath.FromSlash(inst.Name)),
				})
			}
		}
	}
	return entries
}

// deleteInstalled removes the planned directories, refusing anything that
// escapes its skills root.
func deleteInstalled(entries []deleteEntry) []string {
	var errs []string
	for _, e := range entries {
		rel, err := filepath.Rel(e.Root, e.Dir)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			errs = append(errs, e.Dir+": refusing to delete outside skills root")
			continue
		}
		if err := os.RemoveAll(e.Dir); err != nil {
			errs = append(errs, err.Error())
		}
	}
	return errs
}

// installIndex answers "is this source skill already installed, and where?":
// byKey maps tracked installs (key: github-path or local-path) to the full
// installed record per "agent-short|scope"; byName maps same-named
// untracked/foreign copies.
type installIndex struct {
	byKey  map[string]map[string]installedSkill // skill key -> agent|scope -> installed copy
	byName map[string]map[string]skillClass     // base name -> agent|scope -> class
}

func buildInstallIndex(targets []scanTarget, belongsToSource ...func(installedSkill) bool) installIndex {
	trusted := func(inst installedSkill) bool { return inst.Class == classManaged }
	if len(belongsToSource) > 0 && belongsToSource[0] != nil {
		trusted = belongsToSource[0]
	}
	idx := installIndex{
		byKey:  make(map[string]map[string]installedSkill),
		byName: make(map[string]map[string]skillClass),
	}
	for _, t := range targets {
		for _, s := range t.Skills {
			if trusted(s) && s.Key() != "" {
				if idx.byKey[s.Key()] == nil {
					idx.byKey[s.Key()] = make(map[string]installedSkill)
				}
				for _, short := range t.Agents {
					idx.byKey[s.Key()][short+"|"+t.Scope] = s
				}
				continue
			}
			parts := strings.Split(s.Name, "/")
			base := parts[len(parts)-1]
			if idx.byName[base] == nil {
				idx.byName[base] = make(map[string]skillClass)
			}
			class := s.Class
			if class == classManaged {
				// It may be policy-allowed yet belong to another repository.
				// For this configured source it is still an outside collision.
				class = classForeign
			}
			for _, short := range t.Agents {
				key := short + "|" + t.Scope
				// foreign beats untracked when both exist under the same name
				if cur, ok := idx.byName[base][key]; !ok || cur != classForeign {
					idx.byName[base][key] = class
				}
			}
		}
	}
	return idx
}
