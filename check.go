package main

import (
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var errCheckFailed = errors.New("skill check failed")

type checkIssue struct {
	Kind     string
	Skill    string
	Location string
	Message  string
	Action   string
}

type checkReport struct {
	Source       string
	Branch       string
	Revision     string
	ConfigPath   string
	Scope        string
	Checked      int
	Ignored      int
	Issues       []checkIssue
	ScanFailures []string
}

// runCheckCommand performs a read-only audit. It deliberately does not call
// gh skill update or create a PR: check is suitable for CI and must not mutate
// a user's skills or remote repositories merely because it found drift.
func runCheckCommand(cfg config, out io.Writer) error {
	report, err := runSkillCheck(cfg, findProjectRoot())
	if err != nil {
		return err
	}
	if err := renderCheckReport(out, report); err != nil {
		return err
	}
	if len(report.Issues) > 0 || len(report.ScanFailures) > 0 {
		return errCheckFailed
	}
	return nil
}

func runSkillCheck(cfg config, projectRoot string) (checkReport, error) {
	skills, ref, trees, blobs, err := loadSkills(cfg)
	if err != nil {
		return checkReport{}, err
	}

	report := checkReport{
		Source:     cfg.Source,
		Branch:     cfg.Ref,
		Revision:   ref,
		ConfigPath: projectCfgPath,
		Scope:      cfg.Scope,
	}

	patterns := uniqueStrings(cfg.CheckIgnoreSkills)
	for _, pattern := range patterns {
		if _, err := path.Match(pattern, ""); err != nil {
			return checkReport{}, fmt.Errorf("invalid check_ignore_skills pattern %q: %v", pattern, err)
		}
	}

	// A project source is authoritative even when it is not present in the
	// user's broader allowlist. The allowlist remains useful for classifying
	// other copies, while belongsToConfiguredSource below makes the actual
	// check source comparison explicit.
	allowed := append([]string(nil), allowedSources()...)
	if !isLocalSource(cfg.Source) {
		allowed = append(allowed, repoSlug(cfg.Source))
	}
	targets := scanInstalled(allInstallTargets(), projectRoot, uniqueStrings(allowed), allowedLocalRoots(cfg.Source))

	sourceLocal := isLocalSource(cfg.Source)
	sourceRoot := ""
	if sourceLocal {
		sourceRoot, _ = localRoot(cfg.Source)
	}
	checker := model{
		cfg:         cfg,
		sourceLocal: sourceLocal,
		sourceRoot:  sourceRoot,
		skills:      skills,
		treeShas:    trees,
		blobShas:    blobs,
		ref:         ref,
	}
	checker.reconcileMergedOutsideCopies(targets)

	expected := make(map[string]string, len(skills))
	for _, skill := range skills {
		key := skill.Dir()
		if sourceLocal {
			key = filepath.Join(sourceRoot, filepath.FromSlash(key))
		}
		if sha := trees[key]; sha != "" {
			expected[key] = sha
		}
	}

	for _, target := range targets {
		if target.Scope != cfg.Scope {
			continue
		}
		if target.Err != "" {
			report.ScanFailures = append(report.ScanFailures, target.Display+": "+target.Err)
			continue
		}
		for _, installed := range target.Skills {
			location := target.Display + "/" + installed.Name
			// Ignore patterns apply to every class of copy: outdated or
			// locally modified managed skills as well as outside skills.
			if ignoredCheckSkill(installed, patterns) {
				report.Ignored++
				continue
			}
			if belongsToConfiguredSource(installed, cfg.Source, sourceRoot) {
				report.Checked++
				checkManagedCopy(&report, checker, installed, location, expected, sourceLocal, sourceRoot)
				continue
			}
			report.Issues = append(report.Issues, checkIssue{
				Kind:     "outside",
				Skill:    displayInstalledSkill(installed),
				Location: location,
				Message:  outsideSkillMessage(installed),
				Action:   "run gh-skill-tui, select this skill, then press p to propose an import PR",
			})
		}
	}
	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Location == report.Issues[j].Location {
			return report.Issues[i].Kind < report.Issues[j].Kind
		}
		return report.Issues[i].Location < report.Issues[j].Location
	})
	sort.Strings(report.ScanFailures)
	return report, nil
}

func checkManagedCopy(report *checkReport, checker model, installed installedSkill, location string, expected map[string]string, sourceLocal bool, sourceRoot string) {
	key := installed.GhPath
	if sourceLocal {
		key = filepath.Clean(installed.LocalPath)
	}
	if key == "" {
		report.Issues = append(report.Issues, checkIssue{
			Kind:     "untracked",
			Skill:    displayInstalledSkill(installed),
			Location: location,
			Message:  "managed source metadata does not identify a skill path",
			Action:   "run gh-skill-tui, select this skill, then press p to propose an import PR",
		})
		return
	}
	if sourceLocal {
		rel, err := filepath.Rel(filepath.Clean(sourceRoot), filepath.Clean(installed.LocalPath))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			report.Issues = append(report.Issues, checkIssue{
				Kind:     "outside",
				Skill:    displayInstalledSkill(installed),
				Location: location,
				Message:  "installed local source is outside the configured repository",
				Action:   "run gh-skill-tui, select this skill, then press p to propose an import PR",
			})
			return
		}
		key = filepath.Join(sourceRoot, rel)
	} else {
		key = normalizeSkillDir(key)
	}
	want, ok := expected[key]
	if !ok {
		report.Issues = append(report.Issues, checkIssue{
			Kind:     "missing-source",
			Skill:    displayInstalledSkill(installed),
			Location: location,
			Message:  "the installed source path is not present in the configured private-skills revision",
			Action:   "run gh-skill-tui, select this skill, then press p to propose an import PR",
		})
		return
	}
	if installed.TreeSha == "" || installed.TreeSha != want {
		report.Issues = append(report.Issues, checkIssue{
			Kind:     "outdated",
			Skill:    displayInstalledSkill(installed),
			Location: location,
			Message:  fmt.Sprintf("installed tree %s does not match source tree %s", firstNonEmpty(installed.TreeSha, "<missing>"), want),
			Action:   updateAction(checker.cfg, normalizeSkillDir(installed.GhPath), sourceLocal),
		})
		return
	}
	// Companion files can be checked without another network request. For
	// remote sources SKILL.md itself is represented by the tree SHA; the TUI's
	// richer lazy content comparison remains available for local-edit review.
	if checker.copyModified(installed) {
		report.Issues = append(report.Issues, checkIssue{
			Kind:     "modified",
			Skill:    displayInstalledSkill(installed),
			Location: location,
			Message:  "installed files differ from the selected source revision",
			Action:   updateAction(checker.cfg, normalizeSkillDir(installed.GhPath), sourceLocal),
		})
	}
}

func belongsToConfiguredSource(inst installedSkill, source, sourceRoot string) bool {
	if isLocalSource(source) {
		if inst.LocalPath == "" || sourceRoot == "" {
			return false
		}
		rel, err := filepath.Rel(filepath.Clean(sourceRoot), filepath.Clean(inst.LocalPath))
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return inst.RepoSlug != "" && repoSlug(inst.RepoSlug) == repoSlug(source)
}

func normalizeSkillDir(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimSuffix(value, "/SKILL.md")
	return strings.TrimSuffix(value, "/")
}

func displayInstalledSkill(inst installedSkill) string {
	if inst.GhPath != "" {
		return normalizeSkillDir(inst.GhPath)
	}
	return inst.Name
}

func outsideSkillMessage(inst installedSkill) string {
	switch inst.Class {
	case classForeign:
		return "skill is installed from another source: " + firstNonEmpty(inst.RepoSlug, "unknown")
	case classManaged:
		return "skill is managed by an allowed source but not by the configured private-skills source"
	default:
		return "skill has no trusted source metadata"
	}
}

func ignoredCheckSkill(inst installedSkill, patterns []string) bool {
	candidates := []string{
		normalizeSkillDir(inst.Name),
		normalizeSkillDir(inst.GhPath),
		path.Base(normalizeSkillDir(inst.Name)),
	}
	for _, pattern := range patterns {
		pattern = normalizeSkillDir(pattern)
		if pattern == "" {
			continue
		}
		for _, candidate := range candidates {
			if candidate == pattern {
				return true
			}
			if matched, err := path.Match(pattern, candidate); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func updateAction(cfg config, skillPath string, sourceLocal bool) string {
	if sourceLocal {
		return "run gh-skill-tui and install/update this skill from the configured local source"
	}
	args := []string{"gh", "skill", "install", cfg.Source, skillPath, "--force", "--scope", cfg.Scope}
	if cfg.Pin != "" {
		args = append(args, "--pin", cfg.Pin)
	} else {
		return "run gh skill update --all, or reinstall with: " + shellJoin(args)
	}
	return "reinstall the pinned revision with: " + shellJoin(args)
}

func renderCheckReport(out io.Writer, report checkReport) error {
	ref := firstNonEmpty(report.Branch, report.Revision, "default branch")
	if report.Branch != "" && report.Revision != "" && report.Branch != report.Revision {
		ref = report.Branch + " @ " + report.Revision
	}
	if report.ConfigPath != "" {
		if _, err := fmt.Fprintf(out, "gh-skill-check: source=%s ref=%s scope=%s config=%s\n", report.Source, ref, report.Scope, homeShorten(report.ConfigPath)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "gh-skill-check: source=%s ref=%s scope=%s\n", report.Source, ref, report.Scope); err != nil {
		return err
	}
	for _, ignored := range report.ScanFailures {
		if _, err := fmt.Fprintf(out, "ERROR scan: %s\n", ignored); err != nil {
			return err
		}
	}
	for _, issue := range report.Issues {
		if _, err := fmt.Fprintf(out, "ERROR %s: %s — %s\n", issue.Location, issue.Kind, issue.Message); err != nil {
			return err
		}
		if issue.Action != "" {
			if _, err := fmt.Fprintf(out, "  fix: %s\n", issue.Action); err != nil {
				return err
			}
		}
	}
	if report.Ignored > 0 {
		if _, err := fmt.Fprintf(out, "OK ignored: %d skill(s) matched project policy\n", report.Ignored); err != nil {
			return err
		}
	}
	if len(report.Issues) == 0 && len(report.ScanFailures) == 0 {
		_, err := fmt.Fprintf(out, "OK: %d managed skill copy/copies are current; no outside skills found\n", report.Checked)
		return err
	}
	_, err := fmt.Fprintf(out, "FAIL: %d issue(s)\n", len(report.Issues)+len(report.ScanFailures))
	return err
}
