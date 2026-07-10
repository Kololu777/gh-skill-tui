package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type skill struct {
	Path    string // repo path of SKILL.md, e.g. skills/share/memoc-create/SKILL.md
	Content string
	Loading bool
	LoadErr string
}

func (s skill) Dir() string {
	return strings.TrimSuffix(strings.TrimSuffix(s.Path, "SKILL.md"), "/")
}

type skillsLoadedMsg struct {
	Skills []skill
	Ref    string
	// TreeShas maps a skill key (remote: repo-relative dir, local: absolute
	// dir) to the current git tree SHA of that directory, for staleness
	// detection against installed copies.
	TreeShas map[string]string
	// BlobShas maps every source file (same key convention) to its git
	// blob SHA, for detecting locally edited installed copies.
	BlobShas map[string]string
	Err      error
}

type skillContentMsg struct {
	Path    string
	Content string
	Err     error
}

type installedScannedMsg struct {
	Targets []scanTarget
}

func loadSkillsCmd(cfg config) tea.Cmd {
	return func() tea.Msg {
		skills, ref, trees, blobs, err := loadSkills(cfg)
		return skillsLoadedMsg{Skills: skills, Ref: ref, TreeShas: trees, BlobShas: blobs, Err: err}
	}
}

func loadSkillContentCmd(cfg config, ref, path string) tea.Cmd {
	return func() tea.Msg {
		content, err := readSkillContent(cfg, ref, path)
		return skillContentMsg{Path: path, Content: content, Err: err}
	}
}

func scanInstalledCmd(targets []installTarget, projectRoot string, allowed []string, allowedRoots []string) tea.Cmd {
	return func() tea.Msg {
		return installedScannedMsg{Targets: scanInstalled(targets, projectRoot, allowed, allowedRoots)}
	}
}

func loadSkills(cfg config) ([]skill, string, map[string]string, map[string]string, error) {
	if isLocalSource(cfg.Source) {
		return loadLocalSkills(cfg.Source)
	}

	ref := cfg.Ref
	var err error
	if ref == "" {
		ref, err = defaultRef(cfg.Source)
		if err != nil {
			return nil, "", nil, nil, err
		}
	}

	out, err := runGh(
		"api",
		fmt.Sprintf("repos/%s/git/trees/%s?recursive=1", cfg.Source, url.PathEscape(ref)),
		"--jq",
		`.tree[] | "\(.type) \(.sha) \(.path)"`,
	)
	if err != nil {
		return nil, "", nil, nil, err
	}

	var skills []skill
	trees := make(map[string]string)
	blobs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
		if len(fields) != 3 {
			continue
		}
		switch fields[0] {
		case "blob":
			blobs[fields[2]] = fields[1]
			if fields[2] == "SKILL.md" || strings.HasSuffix(fields[2], "/SKILL.md") {
				skills = append(skills, skill{Path: fields[2]})
			}
		case "tree":
			trees[fields[2]] = fields[1]
		}
	}
	shas := make(map[string]string)
	for _, s := range skills {
		if sha, ok := trees[s.Dir()]; ok {
			shas[s.Dir()] = sha
		}
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Path < skills[j].Path
	})
	return skills, ref, shas, blobs, nil
}

// loadLocalSkills lists skills from a local git clone (e.g. of a GitLab
// repository). Like nix flakes, only git-tracked files are considered, and
// a source directory outside a git work tree is an error.
func loadLocalSkills(source string) ([]skill, string, map[string]string, map[string]string, error) {
	root, err := localRoot(source)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if _, err := runGit(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, "", nil, nil, fmt.Errorf("local source %s is not inside a git repository (only git-tracked skills are usable, like nix flakes): %v", root, err)
	}

	tracked, err := runGit(root, "ls-files")
	if err != nil {
		return nil, "", nil, nil, err
	}
	var skills []skill
	for _, rel := range strings.Split(strings.TrimSpace(tracked), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || (rel != "SKILL.md" && !strings.HasSuffix(rel, "/SKILL.md")) {
			continue
		}
		item := skill{Path: rel}
		if content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			item.Content = string(content)
		}
		skills = append(skills, item)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Path < skills[j].Path
	})

	ref := ""
	if out, err := runGit(root, "rev-parse", "--short", "HEAD"); err == nil {
		ref = "local@" + strings.TrimSpace(out)
	}
	trees, blobs := localGitShas(root, skills)
	return skills, ref, trees, blobs, nil
}

// localGitShas maps each skill's absolute directory to its git tree SHA at
// HEAD, and every tracked file to its blob SHA. Staleness is judged against
// the clone's HEAD by design: whether the clone itself is behind its origin
// is the user's responsibility.
func localGitShas(root string, skills []skill) (map[string]string, map[string]string) {
	prefixOut, err := runGit(root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, nil
	}
	prefix := strings.TrimSpace(prefixOut)
	out, err := runGit(root, "ls-tree", "-r", "-t", "--full-tree", "HEAD")
	if err != nil {
		return nil, nil // e.g. empty repository without commits
	}
	trees := make(map[string]string)
	blobs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// format: <mode> <type> <sha>\t<path>
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(line[:tab])
		if len(fields) != 3 {
			continue
		}
		path := line[tab+1:]
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(path, prefix)
		switch fields[1] {
		case "tree":
			trees[rel] = fields[2]
		case "blob":
			blobs[filepath.Join(root, filepath.FromSlash(rel))] = fields[2]
		}
	}
	shas := make(map[string]string)
	for _, s := range skills {
		if sha, ok := trees[s.Dir()]; ok {
			shas[filepath.Join(root, filepath.FromSlash(s.Dir()))] = sha
		}
	}
	return shas, blobs
}

func readSkillContent(cfg config, ref, path string) (string, error) {
	if isLocalSource(cfg.Source) {
		root, err := localRoot(cfg.Source)
		if err != nil {
			return "", err
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		return string(content), nil
	}

	if ref == "" {
		ref = cfg.Ref
	}
	if ref == "" {
		var err error
		ref, err = defaultRef(cfg.Source)
		if err != nil {
			return "", err
		}
	}
	return runGh(
		"api",
		"-H",
		"Accept: application/vnd.github.raw+json",
		fmt.Sprintf("repos/%s/contents/%s?ref=%s", cfg.Source, escapePath(path), url.QueryEscape(ref)),
	)
}

func defaultRef(source string) (string, error) {
	out, err := runGh("api", fmt.Sprintf("repos/%s", source), "--jq", ".default_branch")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runGh(args ...string) (string, error) {
	gh, err := commandPath("gh")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(gh, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func runGit(dir string, args ...string) (string, error) {
	return runGitFull(dir, nil, "", args...)
}

// runGitFull runs git with optional extra environment (e.g. GIT_INDEX_FILE
// for plumbing on a temporary index) and optional stdin.
func runGitFull(dir string, extraEnv []string, stdin string, args ...string) (string, error) {
	git, err := commandPath("git")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// runGhInput runs gh with a request body on stdin (for --input -).
func runGhInput(stdin string, args ...string) (string, error) {
	gh, err := commandPath("gh")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(gh, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func isLocalSource(source string) bool {
	root, err := localRoot(source)
	if err != nil {
		return false
	}
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

func localRoot(source string) (string, error) {
	source = expandHome(source)
	return filepath.Abs(source)
}
