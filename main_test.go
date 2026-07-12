package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testSkills() []skill {
	return []skill{
		{Path: "skills/share/memoc-create/SKILL.md"},
		{Path: "skills/share/memoc-write/SKILL.md"},
		{Path: "skills/tools/gpu-setup/SKILL.md"},
		{Path: "top/SKILL.md"},
	}
}

func loadedModel(t *testing.T, cfg config) model {
	t.Helper()
	t.Setenv("GH_SKILL_DEFAULT_AGENTS", "codex")
	m := newModel(cfg)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(model)
	next, _ = m.Update(skillsLoadedMsg{Skills: testSkills(), Ref: "main"})
	return next.(model)
}

func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func TestParseArgsExtractsAgentFlags(t *testing.T) {
	cfg, err := parseArgs([]string{"--agent", "claude-code", "--scope", "user", "--force", "--pin", "v1", "Owner/Repo"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if cfg.AgentArg != "claude-code" || cfg.Scope != "user" || !cfg.Force {
		t.Fatalf("flags not extracted: %+v", cfg)
	}
	if cfg.Source != "Owner/Repo" {
		t.Fatalf("source = %q", cfg.Source)
	}
	if strings.Join(cfg.InstallArgs, " ") != "--pin v1" {
		t.Fatalf("passthrough = %v", cfg.InstallArgs)
	}

	if _, err := parseArgs([]string{"--scope", "global"}); err == nil {
		t.Fatal("invalid scope should error")
	}
}

func TestArgsForProgram(t *testing.T) {
	joined := func(args []string) string { return strings.Join(args, "\x00") }

	if got := argsForProgram("/nix/store/example/bin/gh-skill-check", []string{"--source", "Owner/Repo"}); joined(got) != "check\x00--source\x00Owner/Repo" {
		t.Fatalf("standalone checker args = %q", joined(got))
	}
	if got := argsForProgram("gh-skill-check", []string{"check", "--source", "Owner/Repo"}); joined(got) != "check\x00--source\x00Owner/Repo" {
		t.Fatalf("explicit check args = %q", joined(got))
	}
	original := []string{"--source", "Owner/Repo"}
	if got := argsForProgram("gh-skill-tui", original); joined(got) != joined(original) {
		t.Fatalf("TUI args changed = %q", joined(got))
	}
}

func TestFileConfigDefaultsAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	content := `
source = "Me/my-skills"
scope = "user"
default_agents = ["claude-code", "kimi"]
allowed_sources = ["me/my-skills", "me/other"]
diff_command = "delta --color-only"

[[agents]]
name = "myagent"
user_dir = "~/.myagent/skills"
project_dir = ".myagent/skills"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadFileConfig(cfgPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fileCfg = fileConfig{} })
	t.Setenv("GH_SKILL_DEFAULT_AGENTS", "")
	t.Setenv("GH_SKILL_ALLOWED_SOURCES", "")
	t.Setenv("GH_SKILL_DEFAULT_SOURCE", "")
	t.Setenv("GH_SKILL_DEFAULT_SCOPE", "")

	cfg, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != "Me/my-skills" || cfg.Scope != "user" {
		t.Fatalf("cfg = %+v", cfg)
	}
	marked := defaultSelectedAgents()
	if !marked["claude-code"] || !marked["kimi"] || marked["codex"] {
		t.Fatalf("marked = %v", marked)
	}
	installTargets := allInstallTargets()
	last := installTargets[len(installTargets)-1]
	if last.Name != "myagent" || last.Short != "myagent" || last.UserDir != "~/.myagent/skills" {
		t.Fatalf("custom agent = %+v", last)
	}
	allowed := allowedSources()
	if len(allowed) != 2 || allowed[0] != "me/my-skills" {
		t.Fatalf("allowed = %v", allowed)
	}
	if fileCfg.DiffCommand != "delta --color-only" {
		t.Fatalf("diff_command = %q", fileCfg.DiffCommand)
	}

	// environment variables override the file
	t.Setenv("GH_SKILL_DEFAULT_AGENTS", "codex")
	if m2 := defaultSelectedAgents(); !m2["codex"] || m2["kimi"] {
		t.Fatalf("env override failed: %v", m2)
	}
	t.Setenv("GH_SKILL_DEFAULT_SOURCE", "Env/repo")
	if cfg2, _ := parseArgs(nil); cfg2.Source != "Env/repo" {
		t.Fatalf("env source override failed: %q", cfg2.Source)
	}

	// missing file is fine, malformed file is a hard error
	if err := loadFileConfig(filepath.Join(dir, "nope.toml")); err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("source = [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadFileConfig(cfgPath); err == nil {
		t.Fatal("malformed toml must error")
	}
	if err := os.WriteFile(cfgPath, []byte("[[agents]]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadFileConfig(cfgPath); err == nil {
		t.Fatal("agent without dirs must error")
	}
	fileCfg = fileConfig{}
}

func TestLegacyProvidersConfigAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[[providers]]
name = "legacy-agent"
agent = "legacy-agent"
user_dir = "~/.legacy/skills"
project_dir = ".legacy/skills"

[[providers]]
name = "codex"
agent = "codex"
user_dir = "~/.legacy-codex/skills"
project_dir = ".legacy-codex/skills"

[[agents]]
name = "codex"
agent = "codex"
user_dir = "~/.canonical-codex/skills"
project_dir = ".canonical-codex/skills"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadFileConfig(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fileCfg = fileConfig{} })
	targets := allInstallTargets()
	byName := make(map[string]installTarget, len(targets))
	for _, target := range targets {
		byName[target.Name] = target
	}
	if got := byName["legacy-agent"]; got.UserDir != "~/.legacy/skills" {
		t.Fatalf("legacy [[providers]] was not loaded: %+v", got)
	}
	if got := byName["codex"]; got.UserDir != "~/.canonical-codex/skills" {
		t.Fatalf("canonical [[agents]] must override legacy alias: %+v", got)
	}
}

func TestRenderDiffExternalTools(t *testing.T) {
	// mirror diffTexts output: diff-so-fancy hard-requires the diff --git header
	diff := "diff --git a/SKILL.md b/SKILL.md\n--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1,2 +1,3 @@\n context\n+## my local fix\n"
	// signature must be tool-specific: the builtin fallback also emits
	// ANSI, so a generic check cannot detect a silently failing tool
	for tool, signatures := range map[string][]string{
		"delta --color-only --paging=never":                 {"\x1b[38;2;", "\x1b[38;5;"},
		"diff-so-fancy":                                     {"\x1b[32m## my local fix"},
		"bat -l diff --color=always --plain --paging=never": {"\x1b[38;2;", "\x1b[38;5;"},
	} {
		bin := strings.Fields(tool)[0]
		if _, err := commandPath(bin); err != nil {
			continue
		}
		fileCfg.DiffCommand = tool
		lines := renderDiff(diff, 60)
		fileCfg.DiffCommand = ""
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "my local fix") {
			t.Fatalf("%s: diff content missing:\n%s", bin, joined)
		}
		matched := false
		for _, signature := range signatures {
			matched = matched || strings.Contains(joined, signature)
		}
		if !matched {
			t.Fatalf("%s: tool signature missing (fell back to builtin?):\n%q", bin, joined)
		}
	}
}

func TestConfigPathFromArgs(t *testing.T) {
	if got := configPathFromArgs([]string{"--config", "/x/c.toml"}); got != "/x/c.toml" {
		t.Fatalf("got %q", got)
	}
	if got := configPathFromArgs([]string{"--config=/y/c.toml"}); got != "/y/c.toml" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildDirEntries(t *testing.T) {
	entries := buildDirEntries(testSkills())
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	want := []string{"", "skills", "skills/share", "skills/tools"}
	if strings.Join(paths, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs = %v, want %v", paths, want)
	}
	if entries[2].Depth != 2 {
		t.Fatalf("depth of skills/share = %d", entries[2].Depth)
	}
}

func TestFilterSkills(t *testing.T) {
	skills := testSkills()
	if got := filterSkills(skills, "skills/share", ""); len(got) != 2 {
		t.Fatalf("dir filter = %v", got)
	}
	if got := filterSkills(skills, "", "gpu"); len(got) != 1 || skills[got[0]].Path != "skills/tools/gpu-setup/SKILL.md" {
		t.Fatalf("query filter = %v", got)
	}
	if got := filterSkills(skills, "skills/share", "gpu"); len(got) != 0 {
		t.Fatalf("combined filter = %v", got)
	}
}

func TestParseSkillMetadata(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"description: something",
		"metadata:",
		"    github-path: skills/share/memoc-create",
		"    github-ref: refs/heads/main",
		"    github-repo: https://github.com/Kololu777/private-skills",
		"name: memoc-create",
		"---",
		"# body",
		"github-repo: https://github.com/evil/repo",
	}, "\n")
	meta := parseSkillMetadata(content)
	if meta.Repo != "https://github.com/Kololu777/private-skills" {
		t.Fatalf("repo = %q", meta.Repo)
	}
	if meta.Path != "skills/share/memoc-create" {
		t.Fatalf("path = %q", meta.Path)
	}
	if meta.Ref != "refs/heads/main" {
		t.Fatalf("ref = %q", meta.Ref)
	}
	if parseSkillMetadata("no frontmatter").Repo != "" {
		t.Fatal("missing frontmatter should yield empty meta")
	}
}

func TestClassify(t *testing.T) {
	allowed := []string{"kololu777/private-skills"}
	cases := []struct {
		repo string
		want skillClass
	}{
		{"https://github.com/Kololu777/private-skills", classManaged},
		{"https://github.com/Kololu777/private-skills.git", classManaged},
		{"https://github.com/other/repo", classForeign},
		{"", classUntracked},
	}
	for _, c := range cases {
		if got := classify(skillMeta{Repo: c.repo}, allowed, nil); got != c.want {
			t.Fatalf("classify(%q) = %v, want %v", c.repo, got, c.want)
		}
	}
}

func TestBuildPlanDedupsSharedDestination(t *testing.T) {
	cfg := config{Source: "Owner/Repo"}
	marked := map[string]bool{"codex": true, "github-copilot": true}
	entries, skipped := buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), marked, "project", false, "/proj", "", nil, nil, nil)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
	if entries[0].Agent != "codex" {
		t.Fatalf("kept agent = %q", entries[0].Agent)
	}
	args := strings.Join(entries[0].Args, " ")
	if !strings.Contains(args, "--agent codex") || !strings.Contains(args, "--scope project") {
		t.Fatalf("args = %q", args)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "github-copilot") {
		t.Fatalf("skipped = %v", skipped)
	}

	// A force override on the skipped alias still applies to the one physical
	// destination executed through the canonical agent target.
	entries, _ = buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), marked, "project", false, "/proj", "", nil, nil, map[string]bool{"github-copilot": true})
	if len(entries) != 1 || !containsStr(entries[0].Args, "--force") {
		t.Fatalf("shared-destination force was lost: %+v", entries)
	}
}

func TestBuildPlanCustomDirAgent(t *testing.T) {
	cfg := config{Source: "Owner/Repo"}
	marked := map[string]bool{"opencode": true}
	entries, _ := buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), marked, "user", true, "/proj", "", nil, nil, nil)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
	args := strings.Join(entries[0].Args, " ")
	if !strings.Contains(args, "--dir "+expandHome("~/.config/opencode/skills")) {
		t.Fatalf("args = %q", args)
	}
	if strings.Contains(args, "--scope") {
		t.Fatalf("--dir install must not pass --scope: %q", args)
	}
	if !strings.Contains(args, "--force") {
		t.Fatalf("force flag missing: %q", args)
	}
	// dummy agent suppresses gh's interactive "Select target agent(s)" prompt
	if !strings.Contains(args, "--agent github-copilot") {
		t.Fatalf("--dir install must pass a dummy --agent: %q", args)
	}
}

func TestBuildPlanKimiUsesAgentsDir(t *testing.T) {
	cfg := config{Source: "Owner/Repo"}
	marked := map[string]bool{"kimi": true}
	entries, _ := buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), marked, "user", false, "/proj", "", nil, nil, nil)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
	args := strings.Join(entries[0].Args, " ")
	if !strings.Contains(args, "--dir "+expandHome("~/.agents/skills")) {
		t.Fatalf("args = %q", args)
	}

	// project scope: kimi shares .agents/skills with codex -> dedup
	marked = map[string]bool{"kimi": true, "codex": true}
	entries, skipped := buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), marked, "project", false, "/proj", "", nil, nil, nil)
	if len(entries) != 1 || entries[0].Agent != "codex" {
		t.Fatalf("entries = %v", entries)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "kimi") {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestBuildPlanAgentArgOverride(t *testing.T) {
	cfg := config{Source: "Owner/Repo", AgentArg: "gemini"}
	entries, skipped := buildPlan(cfg, "gh", []string{"skills/a/SKILL.md"}, allInstallTargets(), map[string]bool{}, "user", false, "/proj", "", nil, nil, nil)
	if len(entries) != 1 || len(skipped) != 0 {
		t.Fatalf("entries = %v skipped = %v", entries, skipped)
	}
	if !strings.Contains(strings.Join(entries[0].Args, " "), "--agent gemini") {
		t.Fatalf("args = %v", entries[0].Args)
	}
}

func TestToggleTreeMarksSubtree(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m = press(t, m, "0", "j", " ") // focus tree, move to "skills", mark subtree
	if len(m.selectedPaths()) != 3 {
		t.Fatalf("selected = %v", m.selectedPaths())
	}
	if m.selected["top/SKILL.md"] {
		t.Fatal("top skill should not be marked")
	}
	m = press(t, m, " ")
	if len(m.selectedPaths()) != 0 {
		t.Fatalf("unmark failed: %v", m.selectedPaths())
	}
}

func TestTreeCursorFiltersSkillsPanel(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m = press(t, m, "0", "j", "j") // move to "skills/share"
	if m.dirFilter != "skills/share" {
		t.Fatalf("dirFilter = %q", m.dirFilter)
	}
	if len(m.visibleSkills) != 2 {
		t.Fatalf("visible = %v", m.visibleSkills)
	}
}

func TestAgentToggleAndConfirmPlan(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "user"})
	if !m.agentSelected["codex"] || m.agentSelected["claude-code"] {
		t.Fatalf("default marks = %v", m.agentSelected)
	}
	m = press(t, m, "2", " ") // focus agents, toggle claude-code (cursor 0)
	if !m.agentSelected["claude-code"] {
		t.Fatalf("claude-code not marked: %v", m.agentSelected)
	}
	m = press(t, m, "1", " ", "i") // select first skill, open install plan
	if !m.confirmMode {
		t.Fatalf("confirm mode not entered: status=%q", m.status)
	}
	if len(m.plan) != 2 { // claude-code + codex, distinct user dirs
		t.Fatalf("plan = %v", m.plan)
	}
	m = press(t, m, "esc")
	if m.confirmMode || m.accepted {
		t.Fatal("esc should cancel confirm")
	}
	m = press(t, m, "i", "enter")
	if !m.running || m.accepted {
		t.Fatal("enter should hand the validated plan to the shared runner")
	}
}

func TestPreviewFocusScrollsAndReturns(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m = press(t, m, "4")
	if m.focus != focusMain || m.detail != focusSkills {
		t.Fatalf("focus = %v detail = %v", m.focus, m.detail)
	}
	m = press(t, m, "j", "j")
	if m.mainScroll != 2 {
		t.Fatalf("mainScroll = %d", m.mainScroll)
	}
	m = press(t, m, "q")
	if m.cancelled {
		t.Fatal("q in preview focus must not quit")
	}
	if m.focus != focusSkills {
		t.Fatalf("focus after q = %v", m.focus)
	}
}

func TestSkillIndicatorAggregatesSelectedAgents(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{"claude-code": true, "codex": true, "kimi": true}
	m.lookup = buildInstallIndex([]scanTarget{
		{Scope: "project", Agents: []string{"claude"}, Skills: []installedSkill{
			{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
		}},
		{Scope: "project", Agents: []string{"kimi"}, Skills: []installedSkill{
			{Name: "memoc-create", Class: classUntracked},
		}},
	})
	target := skill{Path: "skills/share/memoc-create/SKILL.md"}
	if got := m.skillIndicator(target); got != "O" { // outside collision wins health; coverage carries 1/3
		t.Fatalf("indicator = %q, want O", got)
	}
	if installed, total := m.skillCoverage(target); installed != 1 || total != 2 {
		t.Fatalf("coverage = %d/%d, want 1/2 after shared-destination dedup", installed, total)
	}
	m.agentSelected = map[string]bool{"claude-code": true}
	if got := m.skillIndicator(target); got != "✓" {
		t.Fatalf("indicator = %q, want ✓", got)
	}
	m.agentSelected = map[string]bool{"kimi": true}
	if got := m.skillIndicator(target); got != "O" {
		t.Fatalf("indicator = %q, want O", got)
	}
	m.scope = "user"
	m.agentSelected = map[string]bool{"claude-code": true}
	if got := m.skillIndicator(target); got != " " {
		t.Fatalf("indicator@user = %q, want blank", got)
	}
}

func TestAgentRowsShowActionAndInstallState(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{"claude-code": true, "codex": true}
	m.lookup = buildInstallIndex([]scanTarget{
		{Scope: "project", Agents: []string{"claude"}, Skills: []installedSkill{
			{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
		}},
	})

	// cursor on skills/share/memoc-create (first visible skill), nothing marked.
	// claude's copy is current -> marked but nothing to do -> [x], no label
	rows := m.agentRows(40)
	claude, codex, opencode := rows[0].plain, rows[1].plain, rows[2].plain
	if !strings.HasPrefix(claude, "[x]✓ claude-code") || strings.Contains(claude, "update") {
		t.Fatalf("claude row = %q", claude)
	}
	if !strings.HasPrefix(codex, "[x]  codex") || !strings.Contains(codex, "new") {
		t.Fatalf("codex row = %q", codex)
	}
	if !strings.HasPrefix(opencode, "[ ]") || strings.Contains(opencode, "new") || strings.Contains(opencode, "update") {
		t.Fatalf("unselected agent must have no action: %q", opencode)
	}

	// force turns the up-to-date copy into a reinstall
	m.force = true
	if rows := m.agentRows(40); !strings.HasPrefix(rows[0].plain, "[x]✓ claude-code reinstall") {
		t.Fatalf("forced claude row = %q", rows[0].plain)
	}
	m.force = false

	// two skills marked: claude installs only the missing one
	m.selected["skills/share/memoc-create/SKILL.md"] = true
	m.selected["skills/share/memoc-write/SKILL.md"] = true
	rows = m.agentRows(40)
	if !strings.Contains(rows[0].plain, "new 1/2") {
		t.Fatalf("claude row = %q", rows[0].plain)
	}
	if !strings.Contains(rows[1].plain, "new 2/2") {
		t.Fatalf("codex row = %q", rows[1].plain)
	}
}

func TestUpToDateAgentCanBeSelectedAndPlanIsFiltered(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{}
	m.targets = []scanTarget{{Scope: "project", Dir: "/x", Agents: []string{"claude"}, Skills: []installedSkill{
		{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
	}}}
	m.lookup = buildInstallIndex(m.targets)

	// Agent brackets are selection only, even when no work is pending.
	m = press(t, m, "2", " ")
	if !m.agentSelected["claude-code"] {
		t.Fatalf("agent must be selected: %v (status %q)", m.agentSelected, m.status)
	}

	// f adds an explicit agent override while [x] remains selection-only.
	m = press(t, m, "f")
	if !m.agentForce["claude-code"] || !m.agentSelected["claude-code"] {
		t.Fatalf("force mark failed: force=%v marked=%v", m.agentForce, m.agentSelected)
	}
	if rows := m.agentRows(40); !strings.HasPrefix(rows[0].plain, "[x]✓ claude-code reinstall") {
		t.Fatalf("claude row = %q", rows[0].plain)
	}

	// plan includes the current copy only because of the force mark
	m = press(t, m, "1", " ", "i")
	if !m.confirmMode || len(m.plan) != 1 {
		t.Fatalf("plan = %+v (status %q)", m.plan, m.status)
	}
	if !strings.Contains(strings.Join(m.plan[0].Args, " "), "--force") {
		t.Fatalf("forced install must pass --force: %v", m.plan[0].Args)
	}
	m = press(t, m, "esc")

	// without the force mark the same enter has nothing to do
	m = press(t, m, "2", "f", "1", "i")
	if m.confirmMode || len(m.plan) != 0 {
		t.Fatalf("expected no actions: plan=%d status=%q", len(m.plan), m.status)
	}
}

func TestDeletePlanUsesNeutralSelection(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{"claude-code": true, "codex": true}
	m.targets = []scanTarget{{Scope: "project", Dir: "/x", Agents: []string{"claude"}, Skills: []installedSkill{
		{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
	}}}
	m.lookup = buildInstallIndex(m.targets)

	// Space selects; d resolves a separate Delete plan without rewriting marks.
	m = press(t, m, " ", "d")
	target := skill{Path: "skills/share/memoc-create/SKILL.md"}
	if !m.selected[target.Path] {
		t.Fatal("space must select the cursor skill")
	}
	if !m.confirmMode || m.planKind != opDelete || len(m.deletePlan) != 1 || len(m.planBlocks) != 0 {
		t.Fatalf("delete plan: kind=%v plan=%+v blocks=%v status=%q", m.planKind, m.deletePlan, m.planBlocks, m.status)
	}
	if rows := m.skillRows(40); !strings.HasPrefix(rows[0].plain, "[x]") {
		t.Fatalf("skill row = %q", rows[0].plain)
	}
	m = press(t, m, "esc")

	// A second managed copy produces two explicit delete entries.
	m.targets = append(m.targets, scanTarget{Scope: "project", Dir: "/y", Agents: []string{"codex"}, Skills: []installedSkill{
		{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
	}})
	m.lookup = buildInstallIndex(m.targets)
	m = press(t, m, "d")
	if !m.confirmMode || len(m.deletePlan) != 2 || len(m.planBlocks) != 0 {
		t.Fatalf("confirm: mode=%v delete=%d blocks=%v status=%q", m.confirmMode, len(m.deletePlan), m.planBlocks, m.status)
	}
}

func TestAgentSelectionFeedsDeletePlan(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{}
	m.targets = []scanTarget{{Scope: "project", Dir: "/tmp/x", Display: ".claude/skills", Agents: []string{"claude"}, Skills: []installedSkill{
		{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
	}}}
	m.lookup = buildInstallIndex(m.targets)

	// Installed does not imply selected; the state remains a separate column.
	if rows := m.agentRows(40); !strings.HasPrefix(rows[0].plain, "[ ]✓ claude-code") {
		t.Fatalf("claude row = %q", rows[0].plain)
	}

	m = press(t, m, "2", " ") // select claude agent
	if !m.agentSelected["claude-code"] {
		t.Fatalf("agent selection = %v (status %q)", m.agentSelected, m.status)
	}
	if rows := m.agentRows(40); !strings.HasPrefix(rows[0].plain, "[x]✓ claude-code") {
		t.Fatalf("claude row = %q", rows[0].plain)
	}

	// Select the skill, then d opens Delete—not Install—and only claude resolves.
	m = press(t, m, "1", " ", "d")
	if !m.confirmMode || len(m.plan) != 0 || len(m.deletePlan) != 1 {
		t.Fatalf("confirm: mode=%v plan=%d delete=%d status=%q", m.confirmMode, len(m.plan), len(m.deletePlan), m.status)
	}
}

func TestClassifyLocalAllowedRoot(t *testing.T) {
	roots := []string{"/home/ko/src/private-skills"}
	if got := classify(skillMeta{LocalPath: "/home/ko/src/private-skills/skills/a"}, nil, roots); got != classManaged {
		t.Fatalf("allowed local root = %v, want managed", got)
	}
	if got := classify(skillMeta{LocalPath: "/home/ko/other/skills/a"}, nil, roots); got != classUntracked {
		t.Fatalf("other local path = %v, want untracked", got)
	}
}

func TestBadgeStateOutdated(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{"claude-code": true}
	m.targets = []scanTarget{{Scope: "project", Dir: "/x", Agents: []string{"claude"}, Skills: []installedSkill{
		{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create", TreeSha: "old-sha"},
	}}}
	m.lookup = buildInstallIndex(m.targets)
	m.treeShas = map[string]string{"skills/share/memoc-create": "new-sha"}

	target := skill{Path: "skills/share/memoc-create/SKILL.md"}
	if got := m.badgeState(target, m.installTargets[0]); got != badgeOutdated {
		t.Fatalf("badgeState = %v, want outdated", got)
	}
	if got := m.skillIndicator(target); got != "↓" {
		t.Fatalf("indicator = %q, want ↓", got)
	}
	// agent state char follows suit
	if _, _, state, _ := m.agentAction(m.installTargets[0], []skill{target}); state != "↓" {
		t.Fatalf("state = %q, want ↓", state)
	}
	m.selected[target.Path] = true
	updatePlan := press(t, m, "i")
	if len(updatePlan.plan) != 1 || updatePlan.plan[0].Action != "update" ||
		!containsStr(updatePlan.plan[0].Args, "--force") {
		t.Fatalf("outdated update must be non-interactive: %+v", updatePlan.plan)
	}

	// same sha -> up to date
	m.treeShas["skills/share/memoc-create"] = "old-sha"
	if got := m.badgeState(target, m.installTargets[0]); got != badgeManaged {
		t.Fatalf("badgeState = %v, want managed", got)
	}
	// unknown current sha -> no false positive
	m.treeShas = nil
	if got := m.badgeState(target, m.installTargets[0]); got != badgeManaged {
		t.Fatalf("badgeState = %v, want managed when current sha unknown", got)
	}
}

func TestInjectLocalTracking(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memoc-create")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		"---",
		"description: x",
		"metadata:",
		"    local-path: /src/skills/share/memoc-create",
		"name: memoc-create",
		"---",
		"# body",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := injectLocalTracking(root, "/src/skills/share/memoc-create", "sha-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	meta := parseSkillMetadata(string(got))
	if meta.TuiTreeSha != "sha-1" || meta.LocalPath == "" {
		t.Fatalf("meta after inject = %+v", meta)
	}

	// re-inject replaces, not duplicates
	if err := injectLocalTracking(root, "/src/skills/share/memoc-create", "sha-2"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if strings.Count(string(got), "tui-tree-sha") != 1 {
		t.Fatalf("duplicated tui-tree-sha:\n%s", got)
	}
	if parseSkillMetadata(string(got)).TuiTreeSha != "sha-2" {
		t.Fatalf("sha not replaced:\n%s", got)
	}

	if err := injectLocalTracking(root, "/no/such/source", "sha"); err == nil {
		t.Fatal("expected error for unmatched source")
	}
}

func TestLocalSourceRequiresGit(t *testing.T) {
	if _, err := commandPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "a", "SKILL.md"), []byte("---\nname: a\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadSkills(config{Source: dir}); err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("expected git-repository error, got %v", err)
	}
}

func TestLocalSourceListsTrackedOnly(t *testing.T) {
	if _, err := commandPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	mustGit := func(args ...string) {
		if out, err := runGit(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	mustGit("init", "-q")
	mustGit("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	write := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/tracked/SKILL.md")
	write("skills/untracked/SKILL.md")
	mustGit("add", "skills/tracked")
	mustGit("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "add tracked")

	skills, ref, shas, _, err := loadSkills(config{Source: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Path != "skills/tracked/SKILL.md" {
		t.Fatalf("skills = %+v (untracked must be invisible)", skills)
	}
	if !strings.HasPrefix(ref, "local@") {
		t.Fatalf("ref = %q", ref)
	}
	key := filepath.Join(dir, "skills", "tracked")
	if shas[key] == "" {
		t.Fatalf("tree sha missing for %s: %v", key, shas)
	}
}

func TestNormalizeSkillMD(t *testing.T) {
	content := strings.Join([]string{
		"---",
		"description: d",
		"metadata:",
		"    github-path: skills/a",
		"    github-pinned: 0a0f7b0",
		"    github-repo: https://github.com/o/r",
		"    tui-tree-sha: abc",
		"    local-path: /x",
		"name: a",
		"---",
		"# body",
	}, "\n")
	want := strings.Join([]string{"---", "description: d", "name: a", "---", "# body"}, "\n")
	if got := normalizeSkillMD(content); got != want {
		t.Fatalf("normalized:\n%s\nwant:\n%s", got, want)
	}

	// user's own metadata keys survive, metadata: stays
	content2 := strings.Join([]string{
		"---",
		"metadata:",
		"    custom-key: keep",
		"    github-repo: https://github.com/o/r",
		"---",
		"body",
	}, "\n")
	got2 := normalizeSkillMD(content2)
	if !strings.Contains(got2, "custom-key: keep") || !strings.Contains(got2, "metadata:") {
		t.Fatalf("user metadata lost:\n%s", got2)
	}
	if strings.Contains(got2, "github-repo") {
		t.Fatalf("injected key kept:\n%s", got2)
	}
}

func TestGitBlobSha(t *testing.T) {
	// well-known git object id of "test\n"
	if got := gitBlobSha([]byte("test\n")); got != "9daeafb9864cf43055ae93beb0afd6c7d144bfa4" {
		t.Fatalf("blob sha = %s", got)
	}
}

func editedCopyModel(t *testing.T, srcContent, copyContent string) (model, string) {
	t.Helper()
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	copyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte(copyContent), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := installedSkill{
		Name: "share/memoc-create", Dir: copyDir, Class: classManaged,
		GhPath: "skills/share/memoc-create", SkillMD: copyContent,
		FileShas: hashInstalledFiles(copyDir),
	}
	m.targets = []scanTarget{{Scope: "project", Dir: "/x", Agents: []string{"claude"}, Skills: []installedSkill{inst}}}
	m.lookup = buildInstallIndex(m.targets)
	m.blobShas = map[string]string{"skills/share/memoc-create/SKILL.md": gitBlobSha([]byte(srcContent))}
	m.agentSelected = map[string]bool{"claude-code": true}
	m.skills[0].Content = srcContent
	return m, copyDir
}

func TestModifiedDetection(t *testing.T) {
	src := "---\ndescription: d\n---\n# ORIGINAL\n"
	edited := "---\ndescription: d\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# EDITED\n"
	m, _ := editedCopyModel(t, src, edited)

	target := skill{Path: "skills/share/memoc-create/SKILL.md"}
	if got := m.badgeState(target, m.installTargets[0]); got != badgeModified {
		t.Fatalf("badgeState = %v, want modified", got)
	}
	if got := m.skillIndicator(target); got != "m" {
		t.Fatalf("indicator = %q, want m", got)
	}

	// identical after normalization -> managed, not modified
	clean := "---\ndescription: d\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# ORIGINAL\n"
	m2, _ := editedCopyModel(t, src, clean)
	if got := m2.badgeState(target, m2.installTargets[0]); got != badgeManaged {
		t.Fatalf("badgeState = %v, want managed (metadata must not count as edit)", got)
	}
}

func TestModifiedInstallRequiresExplicitOverwrite(t *testing.T) {
	src := "---\ndescription: d\n---\n# ORIGINAL\n"
	edited := "---\ndescription: d\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# EDITED\n"
	m, _ := editedCopyModel(t, src, edited)
	m.selected["skills/share/memoc-create/SKILL.md"] = true

	m = press(t, m, "i")
	if !m.confirmMode || len(m.plan) != 1 || m.plan[0].Action != "overwrite" {
		t.Fatalf("blocked overwrite plan = %+v", m.plan)
	}
	if len(m.planBlocks) != 1 || containsStr(m.plan[0].Args, "--force") {
		t.Fatalf("overwrite must be blocked before force: blocks=%v args=%v", m.planBlocks, m.plan[0].Args)
	}

	m = press(t, m, "esc")
	m.agentForce["claude-code"] = true
	m = press(t, m, "i")
	if len(m.planBlocks) != 0 || len(m.plan) != 1 || m.plan[0].Action != "overwrite" ||
		!containsStr(m.plan[0].Args, "--force") {
		t.Fatalf("explicit overwrite plan = %+v blocks=%v", m.plan, m.planBlocks)
	}
	if len(m.planWarns) != 1 || !strings.Contains(m.planWarns[0], "local edits will be overwritten") {
		t.Fatalf("warnings = %v", m.planWarns)
	}
}

func TestGhFrontmatterReserializationIsNotAnEdit(t *testing.T) {
	// gh re-serializes frontmatter on install: alphabetical key order and
	// its own indentation. That must not count as a local edit.
	src := "---\nname: demo\ndescription: demo skill\n---\n# Body\n"
	installed := "---\ndescription: demo skill\nmetadata:\n    local-path: /x\n    tui-tree-sha: abc\nname: demo\n---\n# Body\n"
	if !skillMDEquivalent(src, installed) {
		t.Fatal("reordered frontmatter must be equivalent")
	}
	m, _ := editedCopyModel(t, src, installed)
	target := skill{Path: "skills/share/memoc-create/SKILL.md"}
	if got := m.badgeState(target, m.installTargets[0]); got != badgeManaged {
		t.Fatalf("badgeState = %v, want managed", got)
	}

	// a real body edit is still detected through the reordering
	edited := "---\ndescription: demo skill\nname: demo\n---\n# Body CHANGED\n"
	if skillMDEquivalent(src, edited) {
		t.Fatal("body edit must not be equivalent")
	}
	// and a frontmatter value edit too
	descEdited := "---\ndescription: better description\nname: demo\n---\n# Body\n"
	if skillMDEquivalent(src, descEdited) {
		t.Fatal("frontmatter value edit must not be equivalent")
	}
}

func TestGhStripsLeadingBodyBlankLine(t *testing.T) {
	// real-world case: gh drops the blank line between frontmatter and body
	src := "---\nname: guide-dpt-model\ndescription: DPTの手引き\n---\n\n# DPTモデルの実装について\n\n- 本文\n"
	installed := "---\ndescription: DPTの手引き\nmetadata:\n    github-path: skills/sakuramoti/guide_dpt_model\nname: guide-dpt-model\n---\n# DPTモデルの実装について\n\n- 本文\n"
	if !skillMDEquivalent(src, installed) {
		t.Fatal("stripped leading blank line must not count as an edit")
	}

	// edited body: reconstruct restores the source's leading blank line
	edited := strings.Replace(installed, "- 本文", "- 本文を直した", 1)
	got := reconstructSkillMD(src, edited)
	if !strings.Contains(got, "---\n\n# DPTモデルの実装について") {
		t.Fatalf("leading blank line not restored:\n%q", got)
	}
	if !strings.Contains(got, "- 本文を直した") {
		t.Fatalf("edit lost:\n%q", got)
	}
	// unedited copy reconstructs to the source verbatim
	if reconstructSkillMD(src, installed) != src {
		t.Fatal("equivalent copy must reconstruct to source verbatim")
	}
}

func TestReconstructSkillMDKeepsSourceFrontmatter(t *testing.T) {
	src := "---\nname: demo\ndescription: demo skill\n---\n# Body\n"
	installed := "---\ndescription: demo skill\nmetadata:\n    local-path: /x\nname: demo\n---\n# Body EDITED\n"
	want := "---\nname: demo\ndescription: demo skill\n---\n# Body EDITED\n"
	if got := reconstructSkillMD(src, installed); got != want {
		t.Fatalf("reconstructed:\n%q\nwant:\n%q", got, want)
	}
	// frontmatter edited -> fall back to normalized copy order
	installed2 := "---\ndescription: NEW DESC\nname: demo\n---\n# Body\n"
	got2 := reconstructSkillMD(src, installed2)
	if !strings.Contains(got2, "NEW DESC") {
		t.Fatalf("frontmatter edit lost: %q", got2)
	}
}

func TestBuildPRPlanFromEditedCopy(t *testing.T) {
	src := "---\ndescription: d\n---\n# ORIGINAL\n"
	edited := "---\ndescription: d\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# EDITED\n"
	m, copyDir := editedCopyModel(t, src, edited)

	plan, errMsg := m.buildPRPlan()
	if plan == nil {
		t.Fatalf("plan nil: %s", errMsg)
	}
	if !plan.Remote || plan.Repo != "Owner/Repo" || plan.CopyDir != copyDir {
		t.Fatalf("plan = %+v", plan)
	}
	wantContent := "---\ndescription: d\n---\n# EDITED\n"
	if got := plan.Files["skills/share/memoc-create/SKILL.md"]; got != wantContent {
		t.Fatalf("file content = %q", got)
	}
	if len(plan.Changed) != 1 || plan.Changed[0] != "M SKILL.md" {
		t.Fatalf("changed = %v", plan.Changed)
	}
	if !strings.HasPrefix(plan.Branch, "gh-skill-tui/memoc-create-") {
		t.Fatalf("branch = %q", plan.Branch)
	}
	if plan.Outdated {
		t.Fatal("plan must not be outdated")
	}
	if _, err := commandPath("git"); err == nil {
		if !strings.Contains(plan.Diff, "+# EDITED") || !strings.Contains(plan.Diff, "-# ORIGINAL") {
			t.Fatalf("diff = %q", plan.Diff)
		}
	}

	// clean copies produce no plan
	m2, _ := editedCopyModel(t, src, "---\ndescription: d\n---\n# ORIGINAL\n")
	if plan2, _ := m2.buildPRPlan(); plan2 != nil {
		t.Fatalf("unexpected plan for clean copy: %+v", plan2)
	}
}

func TestPKeyOpensPRConfirm(t *testing.T) {
	src := "---\ndescription: d\n---\n# ORIGINAL\n"
	edited := "---\ndescription: d\n---\n# EDITED\n"
	m, _ := editedCopyModel(t, src, edited)
	m = press(t, m, "1", " ", "p")
	if !m.confirmMode || m.prPlan == nil {
		t.Fatalf("P did not open confirm: mode=%v plan=%v status=%q", m.confirmMode, m.prPlan, m.status)
	}
	m = press(t, m, "esc")
	if m.confirmMode || m.prPlan != nil {
		t.Fatal("esc must cancel PR confirm")
	}
	// dry-run: enter reports without executing
	dry := m
	dry.cfg.DryRun = true
	dry = press(t, dry, "p", "enter")
	if dry.running || dry.result == nil || dry.result.Summary != "dry-run" {
		t.Fatalf("dry-run enter: running=%v result=%+v status=%q", dry.running, dry.result, dry.status)
	}
	// real accept runs the PR inside the TUI (no quit)
	m = press(t, m, "p", "enter")
	if !m.running || m.confirmMode || m.prPlan != nil || m.accepted {
		t.Fatalf("enter must start the PR in the background: running=%v accepted=%v", m.running, m.accepted)
	}
	// p also proposes edited copies (no outside context here)
	m2, _ := editedCopyModel(t, src, edited)
	m2 = press(t, m2, " ", "p")
	if !m2.confirmMode || m2.prPlan == nil {
		t.Fatalf("p did not open confirm for an edited copy: %q", m2.status)
	}
}

func TestRunPRLocalCreatesBranchWithoutTouchingWorktree(t *testing.T) {
	if _, err := commandPath("git"); err != nil {
		t.Skip("git not available")
	}
	src := t.TempDir()
	mustGit := func(dir string, args ...string) string {
		out, err := runGit(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	mustGit(src, "init", "-q")
	if err := os.MkdirAll(filepath.Join(src, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "demo", "SKILL.md"), []byte("orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(src, "add", "-A")
	mustGit(src, "config", "user.email", "t@t")
	mustGit(src, "config", "user.name", "t")
	mustGit(src, "commit", "-qm", "v1")
	bare := t.TempDir()
	mustGit(bare, "init", "-q", "--bare")
	mustGit(src, "remote", "add", "origin", bare)

	plan := prPlan{
		Remote:     false,
		SourceRoot: src,
		Branch:     "gh-skill-tui/demo-test",
		Title:      "skill: demo (edited)",
		Body:       "body",
		Files:      map[string]string{"skills/demo/SKILL.md": "edited content\n"},
	}
	if _, err := runPRLocal(plan); err != nil {
		t.Fatal(err)
	}

	if got := mustGit(src, "show", plan.Branch+":skills/demo/SKILL.md"); got != "edited content" {
		t.Fatalf("branch content = %q", got)
	}
	if got := mustGit(src, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree dirty: %q", got)
	}
	if disk, _ := os.ReadFile(filepath.Join(src, "skills", "demo", "SKILL.md")); string(disk) != "orig\n" {
		t.Fatalf("worktree file changed: %q", disk)
	}
	if got := mustGit(bare, "rev-parse", "refs/heads/"+plan.Branch); got == "" {
		t.Fatal("branch not pushed to origin")
	}
}

func TestDeletePlanRemovesOnlyTrackedInstalls(t *testing.T) {
	t.Setenv("GH_SKILL_ALLOWED_SOURCES", "kololu777/private-skills")
	root := t.TempDir()
	write := func(rel, repo string) {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: x\n---\n"
		if repo != "" {
			content = strings.Join([]string{
				"---",
				"metadata:",
				"    github-path: skills/" + rel,
				"    github-repo: " + repo,
				"---",
			}, "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("share/memoc-create", "https://github.com/Kololu777/private-skills")
	write("share/memoc-write", "") // untracked; must never be deleted

	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.agentSelected = map[string]bool{"claude-code": true}
	skills, errMsg := readInstalledSkills(root, allowedSources(), nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	m.targets = []scanTarget{{Scope: "project", Dir: root, Display: ".claude/skills", Agents: []string{"claude"}, Skills: skills}}
	m.lookup = buildInstallIndex(m.targets)

	// cursor on skills/share/memoc-create
	plan := buildDeletePlan(m.contextSkills(), m.targets, m.installTargets, m.agentSelected, m.scope, m.sourceRoot)
	if len(plan) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	want := filepath.Join(root, "share/memoc-create")
	if plan[0].Dir != want {
		t.Fatalf("plan dir = %q, want %q", plan[0].Dir, want)
	}
	if errs := deleteInstalled(plan); len(errs) != 0 {
		t.Fatalf("delete errors: %v", errs)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("managed skill dir was not removed")
	}
	if _, err := os.Stat(filepath.Join(root, "share/memoc-write")); err != nil {
		t.Fatal("untracked skill dir must remain")
	}

	// wrong scope -> empty plan
	if p := buildDeletePlan(m.contextSkills(), m.targets, m.installTargets, m.agentSelected, "user", m.sourceRoot); len(p) != 0 {
		t.Fatalf("user-scope plan = %+v", p)
	}
}

func TestSearchAcceptsQRune(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m = press(t, m, "s", "q")
	if m.query != "q" {
		t.Fatalf("query = %q", m.query)
	}
}

func TestSearchSpaceCommitsSelectionAndRestoresOtherSkills(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m = press(t, m, "s", "gpu", " ")
	if m.searchMode || m.query != "" {
		t.Fatalf("search state remained active: mode=%v query=%q", m.searchMode, m.query)
	}
	if len(m.visibleSkills) != len(m.skills) {
		t.Fatalf("other skills remain hidden: visible=%v skills=%v", m.visibleSkills, m.skills)
	}
	if !m.selected["skills/tools/gpu-setup/SKILL.md"] {
		t.Fatalf("search match was not selected: %v", m.selected)
	}
}

func TestViewUsesStableViewportSize(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})

	markdown := strings.Join([]string{
		"---",
		"name: memoc-create",
		"---",
		"# Heading",
		"",
		"- bullet " + strings.Repeat("long ", 40),
		"```sh",
		"echo " + strings.Repeat("code ", 40),
		"```",
		"> quote",
	}, "\n")
	next, _ := m.Update(skillContentMsg{Path: "skills/share/memoc-create/SKILL.md", Content: markdown})
	withContent := next.(model)

	next, _ = m.Update(installedScannedMsg{Targets: []scanTarget{{
		Scope: "user", Dir: "/x", Display: "~/.claude/skills", Agents: []string{"claude"},
		Skills: []installedSkill{
			{Name: "good", Class: classManaged, RepoSlug: "kololu777/private-skills", Ref: "main"},
			{Name: "evil-" + strings.Repeat("x", 120), Class: classForeign, RepoSlug: "evil/repo"},
			{Name: "manual", Class: classUntracked},
		},
	}}})
	withScan := next.(model)

	states := []model{
		m,
		withContent,
		press(t, withScan, "3"),
		press(t, m, "0"),
		press(t, m, "2"),
		press(t, m, "1", " ", "i"), // install plan popup
		press(t, m, "s", "gpu"),
	}
	local := localOnlyModel(t)
	states = append(states,
		press(t, local, "1", "G"), // cursor on the outside row
	)
	for i, s := range states {
		view := s.View()
		lines := strings.Split(view, "\n")
		if len(lines) != 30 {
			t.Fatalf("state %d: %d lines, want 30", i, len(lines))
		}
		for j, line := range lines {
			if w := lipgloss.Width(line); w != 100 {
				t.Fatalf("state %d line %d: width %d, want 100", i, j, w)
			}
		}
	}
}

// localOnlyModel loads a model whose installed scan holds untracked skills
// missing from the source: my-skill in two targets (user + project), plus a
// same-named untracked memoc-create that must be excluded.
func localOnlyModel(t *testing.T) model {
	t.Helper()
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	userDir, projDir := t.TempDir(), t.TempDir()
	write := func(dir, rel, content string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(userDir, "my-skill/SKILL.md", "---\nname: my-skill\n---\n# USER BODY\n")
	write(userDir, "my-skill/helper.py", "print('hi')\n")
	write(userDir, "user-only-skill/SKILL.md", "---\nname: user-only-skill\n---\n# USER ONLY\n")
	write(projDir, "my-skill/SKILL.md", "---\nname: my-skill\n---\n# PROJECT BODY\n")
	write(projDir, "memoc-create/SKILL.md", "---\nname: memoc-create\n---\n") // same base name as a source skill
	userSkills, errMsg := readInstalledSkills(userDir, nil, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	projSkills, errMsg := readInstalledSkills(projDir, nil, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	next, _ := m.Update(installedScannedMsg{Targets: []scanTarget{
		{Scope: "user", Dir: userDir, Display: "~/.claude/skills", Agents: []string{"claude"}, Skills: userSkills},
		{Scope: "project", Dir: projDir, Display: ".agents/skills", Agents: []string{"codex", "kimi"}, Skills: projSkills},
	}})
	return next.(model)
}

func TestReadInstalledSkillsFollowsSymlinksAndSkipsDotDirs(t *testing.T) {
	root := t.TempDir()
	store := t.TempDir() // stands in for the nix store
	write := func(base, rel, content string) {
		t.Helper()
		full := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// home-manager style: the skill dir is a symlink into the store
	write(store, "linked-skill/SKILL.md", "---\nname: linked-skill\n---\n# Linked\n")
	write(store, "linked-skill/helper.sh", "echo hi\n")
	// cache junk must never count as content nor ship in a PR
	write(store, "linked-skill/__pycache__/helper.cpython-312.pyc", "junk")
	if err := os.Symlink(filepath.Join(store, "linked-skill"), filepath.Join(root, "linked-skill")); err != nil {
		t.Fatal(err)
	}
	// vendor space: codex ships built-ins under a dot-directory
	write(root, ".system/imagegen/SKILL.md", "---\nname: imagegen\n---\n")
	write(root, "plain/SKILL.md", "---\nname: plain\n---\n")

	skills, errMsg := readInstalledSkills(root, nil, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
	}
	if strings.Join(names, "|") != "linked-skill|plain" {
		t.Fatalf("names = %v (symlink must be followed, .system skipped)", names)
	}
	// the symlinked copy's files must be hashed through the link, and cache
	// junk (__pycache__) must be excluded
	linked := skills[0]
	if len(linked.FileShas) != 2 {
		t.Fatalf("FileShas = %v", linked.FileShas)
	}

	// and buildAddPRPlan must walk through the symlink too, minus the junk
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	cp := localCopy{Scope: "user", Agents: []string{"claude"}, Inst: linked}
	plan, planErr := m.buildAddPRPlan(cp, "skills/linked-skill")
	if plan == nil {
		t.Fatal(planErr)
	}
	if len(plan.Files) != 2 || plan.Files["skills/linked-skill/helper.sh"] != "echo hi\n" {
		t.Fatalf("plan files = %v", plan.Files)
	}
}

func TestBuildOutsideGroupsEveryNonSourceCopy(t *testing.T) {
	targets := []scanTarget{
		{Scope: "user", Display: "~/.claude/skills", Agents: []string{"claude"}, Skills: []installedSkill{
			{Name: "my-skill", Class: classUntracked},
			{Name: "foreign-skill", Class: classForeign, RepoSlug: "evil/repo"},
			// tracked to the configured source and present there: not outside
			{Name: "share/memoc-create", Class: classManaged, GhPath: "skills/share/memoc-create"},
		}},
		{Scope: "project", Display: ".agents/skills", Agents: []string{"codex", "kimi"}, Skills: []installedSkill{
			{Name: "my-skill", Class: classUntracked},
			{Name: "memoc-create", Class: classUntracked}, // same base name as a source skill
			// allowed-source metadata but the path is absent upstream, e.g. a
			// skill created locally by copying a sibling's frontmatter
			{Name: "sakuramoti/port_model_parity", Class: classManaged, GhPath: "skills/sakuramoti/port_model_parity"},
		}},
	}
	sourceKeys := make(map[string]bool)
	for _, s := range testSkills() {
		sourceKeys[s.Dir()] = true
	}
	got := buildLocalOnly(targets, testSkills(), sourceKeys)
	if len(got) != 4 {
		t.Fatalf("outside = %+v", got)
	}
	byName := make(map[string]localOnlySkill)
	for _, entry := range got {
		byName[entry.Name] = entry
	}
	if entry := byName["foreign-skill"]; entry.Reason != reasonExternalSource {
		t.Fatalf("foreign entry = %+v", entry)
	}
	if entry := byName["memoc-create"]; entry.Reason != reasonNoTracking {
		t.Fatalf("same-name no-track copy must remain visible outside: %+v", entry)
	}
	if entry := byName["my-skill"]; len(entry.Copies) != 2 || entry.Copies[0].Scope != "user" || entry.Copies[1].Scope != "project" {
		t.Fatalf("copies = %+v", entry.Copies)
	}
	if entry := byName["sakuramoti/port_model_parity"]; entry.Reason != reasonSourcePathMissing || len(entry.Copies) != 1 {
		t.Fatalf("source-missing entry = %+v", entry)
	}
}

func TestAllowedButDifferentRepositoryIsStillOutside(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	targets := []scanTarget{{
		Scope: "project", Dir: t.TempDir(), Agents: []string{"codex"},
		Skills: []installedSkill{{
			Name: "share/memoc-create", Class: classManaged,
			RepoSlug: "Other/Allowed", GhPath: "skills/share/memoc-create",
		}},
	}}
	next, _ := m.Update(installedScannedMsg{Targets: targets})
	m = next.(model)
	if len(m.localOnly) != 1 || m.localOnly[0].Reason != reasonExternalSource {
		t.Fatalf("outside entries = %+v", m.localOnly)
	}
	target := m.skills[0]
	if got := m.badgeState(target, m.installTargets[1]); got != badgeForeign {
		t.Fatalf("badge = %v, want outside collision", got)
	}

	m.selected[target.Path] = true
	m = press(t, m, "i")
	if len(m.planBlocks) != 0 || len(m.plan) != 1 || m.plan[0].Action != "adopt" ||
		!containsStr(m.plan[0].Args, "--force") {
		t.Fatalf("adopt plan = %+v blocks=%v", m.plan, m.planBlocks)
	}
	m = press(t, m, "esc", "d")
	if len(m.deletePlan) != 0 || len(m.planBlocks) != 1 {
		t.Fatalf("different-repo copy must be protected: delete=%v blocks=%v", m.deletePlan, m.planBlocks)
	}
}

func TestLocalOnlyRowsInSkillsPanel(t *testing.T) {
	m := localOnlyModel(t)
	// scope:project includes my-skill and the no-track same-name collision.
	if m.skillsListLen() != 6 || len(m.visibleLocal) != 2 {
		t.Fatalf("list len = %d, visibleLocal = %v", m.skillsListLen(), m.visibleLocal)
	}
	rows := m.skillRows(40)
	last := rows[len(rows)-1].plain
	if !strings.HasPrefix(last, "[ ]O") || !strings.Contains(last, "my-skill") || strings.Contains(last, "×") {
		t.Fatalf("local row = %q (one project copy, no ×N suffix)", last)
	}
	if m.dirs[len(m.dirs)-1].Path != localOnlyDir {
		t.Fatalf("tree missing local-only node: %+v", m.dirs)
	}
	// u switches scope and with it the outside set
	mu := press(t, m, "u")
	if mu.scope != "user" || len(mu.visibleLocal) != 2 {
		t.Fatalf("scope=%q visibleLocal=%v (want my-skill + user-only-skill)", mu.scope, mu.visibleLocal)
	}
	// selecting the virtual node filters panel 1 to the local rows only
	m2 := press(t, m, "0", "G")
	if m2.dirFilter != localOnlyDir {
		t.Fatalf("dirFilter = %q", m2.dirFilter)
	}
	if len(m2.visibleSkills) != 0 || len(m2.visibleLocal) != 2 {
		t.Fatalf("filtered: skills=%v local=%v", m2.visibleSkills, m2.visibleLocal)
	}
	// search matches both populations
	m3 := press(t, m, "s", "my")
	if len(m3.visibleSkills) != 0 || len(m3.visibleLocal) != 1 {
		t.Fatalf("search my: skills=%v local=%v", m3.visibleSkills, m3.visibleLocal)
	}
	m4 := press(t, m, "s", "memoc")
	if len(m4.visibleSkills) != 2 || len(m4.visibleLocal) != 1 {
		t.Fatalf("search memoc: skills=%v local=%v", m4.visibleSkills, m4.visibleLocal)
	}
}

func TestOutsideTreeAggregateShowsNonePartialAndAll(t *testing.T) {
	m := localOnlyModel(t)
	last := func(current model) string {
		rows := current.treeRows(50)
		return rows[len(rows)-1].plain
	}
	if got := last(m); !strings.HasPrefix(got, "[ ] (outside source)") {
		t.Fatalf("none = %q", got)
	}
	m.localMarked["untracked|my-skill"] = true
	if got := last(m); !strings.HasPrefix(got, "[~] (outside source)") {
		t.Fatalf("partial = %q", got)
	}
	m = press(t, m, "0", "G", " ")
	if got := last(m); !strings.HasPrefix(got, "[x] (outside source)") {
		t.Fatalf("all = %q", got)
	}
	m = press(t, m, " ")
	if got := last(m); !strings.HasPrefix(got, "[ ] (outside source)") {
		t.Fatalf("cleared = %q", got)
	}
}

func TestOutsideSelectionAndBlockedOperations(t *testing.T) {
	m := localOnlyModel(t)
	m = press(t, m, "1", "G") // cursor onto the local row
	if _, ok := m.currentLocalOnly(); !ok {
		t.Fatalf("cursor not on local row: %d", m.cursors[focusSkills])
	}
	if _, ok := m.currentSkill(); ok {
		t.Fatal("currentSkill must not see the local row")
	}
	// Space is neutral selection; the outside key includes provenance.
	m = press(t, m, " ")
	if len(m.selected) != 0 || !m.localMarked["untracked|my-skill"] {
		t.Fatalf("space: selected=%v localMarked=%v", m.selected, m.localMarked)
	}
	if !strings.Contains(m.skillRows(40)[5].plain, "[x]O") {
		t.Fatalf("selected row = %q", m.skillRows(40)[5].plain)
	}
	// i and d both show the outside blocker and execute nothing.
	m = press(t, m, "i")
	if !m.confirmMode || m.planKind != opInstall || len(m.planBlocks) != 1 {
		t.Fatalf("install block = kind:%v blocks:%v status:%q", m.planKind, m.planBlocks, m.status)
	}
	m = press(t, m, "enter")
	if !m.confirmMode || m.running {
		t.Fatal("enter must remain disabled on a blocked plan")
	}
	m = press(t, m, "esc", "d")
	if !m.confirmMode || m.planKind != opDelete || len(m.planBlocks) != 1 {
		t.Fatalf("delete block = kind:%v blocks:%v", m.planKind, m.planBlocks)
	}
}

func TestMixedSelectionBlocksTheWholeInstallPlan(t *testing.T) {
	m := localOnlyModel(t)
	m.selected["skills/tools/gpu-setup/SKILL.md"] = true
	m.localMarked["untracked|my-skill"] = true

	m = press(t, m, "i")
	if !m.confirmMode || m.planKind != opInstall {
		t.Fatalf("install plan not opened: kind=%v status=%q", m.planKind, m.status)
	}
	if len(m.plan) != 1 || m.plan[0].Skill != "skills/tools/gpu-setup/SKILL.md" {
		t.Fatalf("ready work = %+v", m.plan)
	}
	if len(m.planBlocks) != 1 || !strings.Contains(m.planBlocks[0], "my-skill") {
		t.Fatalf("blockers = %v", m.planBlocks)
	}
	before := append([]planEntry(nil), m.plan...)
	m = press(t, m, "enter")
	if !m.confirmMode || m.running || len(m.plan) != len(before) {
		t.Fatalf("blocked enter changed plan: confirm=%v running=%v plan=%v", m.confirmMode, m.running, m.plan)
	}
	if !strings.Contains(m.status, "blocked") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestMixedSelectionBlocksTheWholeDeletePlan(t *testing.T) {
	m := localOnlyModel(t)
	root := t.TempDir()
	m.targets = append(m.targets, scanTarget{
		Scope: "project", Dir: root, Agents: []string{"codex"},
		Skills: []installedSkill{{
			Name: "tools/gpu-setup", Class: classManaged,
			GhPath: "skills/tools/gpu-setup",
		}},
	})
	m.lookup = buildInstallIndex(m.targets, m.copyFromConfiguredSource)
	m.rebuildLocalOnly()
	m.selected["skills/tools/gpu-setup/SKILL.md"] = true
	m.localMarked["untracked|my-skill"] = true

	m = press(t, m, "d")
	if !m.confirmMode || len(m.deletePlan) != 1 || len(m.planBlocks) != 1 {
		t.Fatalf("mixed delete = ready:%v blocks:%v", m.deletePlan, m.planBlocks)
	}
	m = press(t, m, "enter")
	if !m.confirmMode || m.running {
		t.Fatal("blocked Delete must not execute its Ready subset")
	}
}

func TestSourceCollisionBuildsExplicitAdoptPlan(t *testing.T) {
	m := localOnlyModel(t)
	// A no-track copy named memoc-create exists at the selected codex
	// destination. Selecting the source counterpart means ADOPT, not a
	// silent ordinary install.
	m.selected["skills/share/memoc-create/SKILL.md"] = true
	m = press(t, m, "i")
	if !m.confirmMode || len(m.planBlocks) != 0 || len(m.plan) != 1 {
		t.Fatalf("adopt plan = %+v blocks=%v status=%q", m.plan, m.planBlocks, m.status)
	}
	entry := m.plan[0]
	if entry.Action != "adopt" || !containsStr(entry.Args, "--force") {
		t.Fatalf("entry = %+v", entry)
	}
	if len(m.planWarns) != 1 || !strings.Contains(m.planWarns[0], "outside copy will be replaced") {
		t.Fatalf("warnings = %v", m.planWarns)
	}
}

func TestDeletePlanMatchesManagedLocalSourceMetadata(t *testing.T) {
	root := t.TempDir()
	s := skill{Path: "skills/share/memoc-create/SKILL.md"}
	copyRoot := t.TempDir()
	installTargets := []installTarget{{Name: "codex", Short: "codex"}}
	targets := []scanTarget{{
		Scope: "project", Dir: copyRoot, Agents: []string{"codex"},
		Skills: []installedSkill{{
			Name:      "share/memoc-create",
			Class:     classManaged,
			LocalPath: filepath.Join(root, "skills/share/memoc-create"),
		}},
	}}
	plan := buildDeletePlan([]skill{s}, targets, installTargets, map[string]bool{"codex": true}, "project", root)
	if len(plan) != 1 || plan[0].Skill != s.Dir() {
		t.Fatalf("local-source delete plan = %+v", plan)
	}
}

func TestLocalOnlyCopiesPanel(t *testing.T) {
	m := localOnlyModel(t)
	m = press(t, m, "1", "G") // cursor on my-skill (scope:project)
	// panel 2 is an inventory; O is outside brackets and never means selected.
	if got := m.listLen(focusAgents); got != len(m.installTargets) {
		t.Fatalf("agents list len = %d, want %d", got, len(m.installTargets))
	}
	rows := m.agentRows(60)
	// agents: claude-code, codex, opencode, kimi, ... — the project copy
	// lives in the codex+kimi shared dir; claude's copy is user scope
	if !strings.HasPrefix(rows[0].plain, "    claude-code") ||
		!strings.HasPrefix(rows[1].plain, "  O codex copy") ||
		!strings.HasPrefix(rows[3].plain, "  O kimi copy") {
		t.Fatalf("rows = %q %q %q", rows[0].plain, rows[1].plain, rows[3].plain)
	}
	// cursor agent (claude-code) has no project copy -> falls back to the
	// first project copy
	if _, cp, ok := m.currentLocalCopy(); !ok || cp.Scope != "project" {
		t.Fatalf("default copy = %+v (ok=%v)", cp, ok)
	}
	// at user scope the claude-code cursor picks the claude copy
	mu := press(t, m, "u")
	if _, cp, ok := mu.currentLocalCopy(); !ok || cp.Scope != "user" || !containsStr(cp.Agents, "claude") {
		t.Fatalf("user-scope copy = %+v (ok=%v)", cp, ok)
	}
	// agent marks are inert in outside mode
	m = press(t, m, "2", " ", "f", "d")
	if len(m.agentSelected) != 1 || !m.agentSelected["codex"] || len(m.agentForce) != 0 {
		t.Fatalf("marks changed: marked=%v force=%v", m.agentSelected, m.agentForce)
	}
}

func TestOutsidePRNeverFallsBackAcrossMultipleCopies(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	makeTarget := func(agent, body string) scanTarget {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, "same-name")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: same-name\n---\n# " + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return scanTarget{
			Scope: "project", Dir: root, Agents: []string{agent},
			Skills: []installedSkill{{
				Name: "same-name", Dir: dir, Class: classUntracked,
				SkillMD: content, FileShas: hashInstalledFiles(dir),
			}},
		}
	}
	m.targets = []scanTarget{makeTarget("claude", "CLAUDE"), makeTarget("codex", "CODEX")}
	m.lookup = buildInstallIndex(m.targets)
	m.rebuildLocalOnly()
	m.localMarked["untracked|same-name"] = true

	// opencode owns neither copy. With two physical candidates this must
	// block instead of silently using the first one.
	m.cursors[focusAgents] = 2
	m = press(t, m, "p")
	if !m.confirmMode || m.prPlan != nil || len(m.planBlocks) != 1 ||
		!strings.Contains(m.planBlocks[0], "multiple copies") {
		t.Fatalf("ambiguous outside plan = %+v blocks=%v", m.prPlan, m.planBlocks)
	}

	m = press(t, m, "esc")
	m.cursors[focusAgents] = 0 // claude owns one explicit copy
	m = press(t, m, "p")
	if !m.confirmMode || m.prPlan == nil || len(m.planBlocks) != 0 {
		t.Fatalf("explicit outside plan = %+v blocks=%v", m.prPlan, m.planBlocks)
	}
	if got := m.prPlan.Files["skills/same-name/SKILL.md"]; !strings.Contains(got, "CLAUDE") {
		t.Fatalf("wrong copy selected: %q", got)
	}
}

func TestLowercasePBatchesMarkedLocalSkills(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	projDir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(projDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("my-skill/SKILL.md", "---\nname: my-skill\n---\n# A\n")
	write("share/new_tool/SKILL.md", "---\nname: new-tool\n---\n# B\n") // namespace matches skills/share
	skills, errMsg := readInstalledSkills(projDir, nil, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	next, _ := m.Update(installedScannedMsg{Targets: []scanTarget{
		{Scope: "project", Dir: projDir, Display: ".claude/skills", Agents: []string{"claude"}, Skills: skills},
	}})
	m = next.(model)

	// tree node space selects all visible outside entries
	m = press(t, m, "0", "G", " ")
	if len(m.localMarked) != 2 {
		t.Fatalf("localMarked = %v", m.localMarked)
	}
	m = press(t, m, "p")
	if !m.confirmMode || m.prPlan == nil {
		t.Fatalf("p did not build a batch plan: %q", m.status)
	}
	p := m.prPlan
	if strings.Join(p.DestDirs, "|") != "skills/my-skill|skills/share/new_tool" {
		t.Fatalf("dests = %v", p.DestDirs)
	}
	if !strings.HasPrefix(p.Branch, "gh-skill-tui/add-2-skills-") {
		t.Fatalf("branch = %q", p.Branch)
	}
	if _, ok := p.Files["skills/my-skill/SKILL.md"]; !ok {
		t.Fatalf("files = %v", p.Files)
	}
	if _, ok := p.Files["skills/share/new_tool/SKILL.md"]; !ok {
		t.Fatalf("files = %v", p.Files)
	}
	m = press(t, m, "enter")
	if !m.running {
		t.Fatal("enter must start the batch PR")
	}
}

func TestPRPlanCombinesEditedAndOutsideSelections(t *testing.T) {
	src := "---\ndescription: demo\n---\n# ORIGINAL\n"
	edited := "---\ndescription: demo\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# EDITED\n"
	m, _ := editedCopyModel(t, src, edited)

	outsideRoot := t.TempDir()
	outsideDir := filepath.Join(outsideRoot, "new-local")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideContent := "---\nname: new-local\n---\n# NEW\n"
	if err := os.WriteFile(filepath.Join(outsideDir, "SKILL.md"), []byte(outsideContent), 0o644); err != nil {
		t.Fatal(err)
	}
	m.targets = append(m.targets, scanTarget{
		Scope: "project", Dir: outsideRoot, Agents: []string{"claude"},
		Skills: []installedSkill{{
			Name: "new-local", Dir: outsideDir, Class: classUntracked,
			SkillMD: outsideContent, FileShas: hashInstalledFiles(outsideDir),
		}},
	})
	m.lookup = buildInstallIndex(m.targets)
	m.rebuildLocalOnly()
	m.selected["skills/share/memoc-create/SKILL.md"] = true
	m.localMarked["untracked|new-local"] = true

	m = press(t, m, "p")
	if !m.confirmMode || m.planKind != opPR || len(m.planBlocks) != 0 || m.prPlan == nil {
		t.Fatalf("combined PR = %+v blocks=%v status=%q", m.prPlan, m.planBlocks, m.status)
	}
	p := m.prPlan
	if strings.Join(p.SourceDirs, "|") != "skills/share/memoc-create" ||
		strings.Join(p.DestDirs, "|") != "skills/new-local" {
		t.Fatalf("source/dest dirs = %v / %v", p.SourceDirs, p.DestDirs)
	}
	if _, ok := p.Files["skills/share/memoc-create/SKILL.md"]; !ok {
		t.Fatalf("edited file missing: %v", p.Files)
	}
	if got := p.Files["skills/new-local/SKILL.md"]; !strings.Contains(got, "# NEW") {
		t.Fatalf("outside file = %q", got)
	}
	changed := strings.Join(p.Changed, "|")
	if !strings.Contains(changed, "M skills/share/memoc-create/SKILL.md") ||
		!strings.Contains(changed, "A skills/new-local/SKILL.md") {
		t.Fatalf("changed = %v", p.Changed)
	}
	if !strings.HasPrefix(p.Branch, "gh-skill-tui/propose-2-skills-") {
		t.Fatalf("branch = %q", p.Branch)
	}
}

func TestMixedPRSelectionBlocksTheWholeProposal(t *testing.T) {
	src := "---\ndescription: demo\n---\n# ORIGINAL\n"
	edited := "---\ndescription: demo\nmetadata:\n    github-path: skills/share/memoc-create\n---\n# EDITED\n"
	m, _ := editedCopyModel(t, src, edited)
	m.selected["skills/share/memoc-create/SKILL.md"] = true
	m.selected["skills/share/memoc-write/SKILL.md"] = true // no edited copy

	m = press(t, m, "p")
	if !m.confirmMode || m.prPlan == nil || len(m.planBlocks) != 1 {
		t.Fatalf("mixed PR = ready:%+v blocks:%v", m.prPlan, m.planBlocks)
	}
	m = press(t, m, "enter")
	if !m.confirmMode || m.running {
		t.Fatal("blocked PR must not execute its Ready subset")
	}
}

func TestPlanBlockersGroupByReason(t *testing.T) {
	blocks := []string{
		"skills/astral/ruff: no locally edited copy in project scope",
		"skills/astral/ty: no locally edited copy in project scope",
		"skills/share/demo: outside source is protected from Delete",
		"no agents selected",
	}
	groups := groupPlanBlocks(blocks)
	if len(groups) != 3 {
		t.Fatalf("groups = %+v", groups)
	}
	if groups[0].Reason != "no locally edited copy in project scope" ||
		strings.Join(groups[0].Targets, "|") != "skills/astral/ruff|skills/astral/ty" {
		t.Fatalf("first group = %+v", groups[0])
	}

	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.planKind = opPR
	m.planBlocks = blocks
	m.confirmMode = true
	_, lines := m.confirmContent(90)
	rendered := strings.Join(lines, "\n")
	if strings.Count(rendered, "no locally edited copy in project scope") != 1 ||
		!strings.Contains(rendered, "(2 skills)") ||
		!strings.Contains(rendered, "skills/astral/ruff") ||
		!strings.Contains(rendered, "skills/astral/ty") {
		t.Fatalf("grouped render:\n%s", rendered)
	}
}

func TestOperationResultKeepsFailedSelectionAndClearsSuccessfulSelection(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	m.selected[m.skills[0].Path] = true
	m.localMarked["outside"] = true
	next, _ := m.Update(operationDoneMsg{Result: executionResult{
		Kind: opInstall, Summary: "install failed", Failed: []string{"boom"},
	}})
	failed := next.(model)
	if failed.result == nil || failed.focus != focusMain || !failed.scanning {
		t.Fatalf("failed result state = result:%+v focus:%v scanning:%v", failed.result, failed.focus, failed.scanning)
	}
	if !failed.selected[m.skills[0].Path] || !failed.localMarked["outside"] {
		t.Fatalf("failed selections were cleared: source=%v outside=%v", failed.selected, failed.localMarked)
	}

	m.result = nil
	next, _ = m.Update(operationDoneMsg{Result: executionResult{
		Kind: opDelete, Summary: "delete: 1 succeeded", Succeeded: []string{"one"},
	}})
	succeeded := next.(model)
	if succeeded.result == nil || len(succeeded.selected) != 0 || len(succeeded.localMarked) != 0 {
		t.Fatalf("successful result state = result:%+v source=%v outside=%v", succeeded.result, succeeded.selected, succeeded.localMarked)
	}
}

func TestPlanExecRetainsCommandFailureTail(t *testing.T) {
	ex := &planExec{
		kind: opInstall,
		installs: []planEntry{{
			Skill: "skills/demo/SKILL.md", Agent: "codex", Dest: ".agents/skills",
			Args: []string{"/bin/sh", "-c", "echo useful-diagnosis >&2; exit 7"},
		}},
		stdout: &strings.Builder{},
		stderr: &strings.Builder{},
	}
	if err := ex.Run(); err != nil {
		t.Fatal(err)
	}
	if len(ex.result.Failed) != 1 || !strings.Contains(ex.result.Failed[0], "useful-diagnosis") {
		t.Fatalf("failure detail = %v", ex.result.Failed)
	}
}

func TestAutoDestDir(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	dest := func(inst installedSkill) (string, error) {
		return m.autoDestDir(localCopy{Scope: "project", Agents: []string{"claude"}, Inst: inst})
	}
	// 1. a namespaced installed path is mirrored below skills/
	if d, err := dest(installedSkill{Name: "sakuramoti/port_model", GhPath: "skills/sakuramoti/port_model"}); err != nil || d != "skills/sakuramoti/port_model" {
		t.Fatalf("tree mirror: %q %v", d, err)
	}
	// 2. another nested path gets the same relative layout
	if d, err := dest(installedSkill{Name: "share/new_tool"}); err != nil || d != "skills/share/new_tool" {
		t.Fatalf("nested tree mirror: %q %v", d, err)
	}
	// 3. plain names are mirrored under the source's skills/ root
	if d, err := dest(installedSkill{Name: "plain-skill"}); err != nil || d != "skills/plain-skill" {
		t.Fatalf("tree mirror: %q %v", d, err)
	}
	// collisions are an error, not a silent overwrite
	if _, err := dest(installedSkill{Name: "share/memoc-create"}); err == nil {
		t.Fatal("collision must error")
	}
	if _, err := dest(installedSkill{Name: "../escape"}); err == nil {
		t.Fatal("path traversal must error")
	}
}

func TestBuildAddPRPlanNormalizesMetadata(t *testing.T) {
	m := loadedModel(t, config{Source: "Owner/Repo", Scope: "project"})
	dir := t.TempDir()
	content := "---\nname: my-skill\nmetadata:\n    local-path: /x\n    tui-tree-sha: abc\n---\n# BODY\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cp := localCopy{Scope: "user", Agents: []string{"claude"}, Inst: installedSkill{Name: "my-skill", Dir: dir, SkillMD: content}}
	plan, errMsg := m.buildAddPRPlan(cp, "skills/my-skill")
	if plan == nil {
		t.Fatal(errMsg)
	}
	want := "---\nname: my-skill\n---\n# BODY\n"
	if got := plan.Files["skills/my-skill/SKILL.md"]; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	// a metadata-free copy ships byte-identical
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	plan2, errMsg := m.buildAddPRPlan(cp, "skills/my-skill")
	if plan2 == nil {
		t.Fatal(errMsg)
	}
	if got := plan2.Files["skills/my-skill/SKILL.md"]; got != want {
		t.Fatalf("clean copy changed: %q", got)
	}
}

func TestRunPRLocalAddsNewSkillPath(t *testing.T) {
	if _, err := commandPath("git"); err != nil {
		t.Skip("git not available")
	}
	src := t.TempDir()
	mustGit := func(dir string, args ...string) string {
		out, err := runGit(dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	mustGit(src, "init", "-q")
	if err := os.MkdirAll(filepath.Join(src, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "skills", "demo", "SKILL.md"), []byte("orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(src, "add", "-A")
	mustGit(src, "config", "user.email", "t@t")
	mustGit(src, "config", "user.name", "t")
	mustGit(src, "commit", "-qm", "v1")
	bare := t.TempDir()
	mustGit(bare, "init", "-q", "--bare")
	mustGit(src, "remote", "add", "origin", bare)

	plan := prPlan{
		Remote:     false,
		SourceRoot: src,
		Branch:     "gh-skill-tui/add-newbie-test",
		DestDirs:   []string{"skills/newbie"},
		Title:      "skill: add newbie",
		Body:       "body",
		Files: map[string]string{
			"skills/newbie/SKILL.md":  "new skill\n",
			"skills/newbie/helper.sh": "echo hi\n",
		},
	}
	if _, err := runPRLocal(plan); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(src, "show", plan.Branch+":skills/newbie/SKILL.md"); got != "new skill" {
		t.Fatalf("branch content = %q", got)
	}
	if got := mustGit(src, "show", plan.Branch+":skills/demo/SKILL.md"); got != "orig" {
		t.Fatalf("existing content = %q", got)
	}
	if got := mustGit(src, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree dirty: %q", got)
	}
	if _, err := os.Stat(filepath.Join(src, "skills", "newbie")); !os.IsNotExist(err) {
		t.Fatal("worktree must not gain the new dir")
	}
	if got := mustGit(bare, "rev-parse", "refs/heads/"+plan.Branch); got == "" {
		t.Fatal("branch not pushed to origin")
	}
}

func makeCheckSource(t *testing.T, body string) string {
	t.Helper()
	if _, err := commandPath("git"); err != nil {
		t.Skip("git not available")
	}
	src := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		if out, err := runGit(src, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	path := filepath.Join(src, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: demo\n---\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("init", "-q")
	gitRun("config", "user.email", "test@example.com")
	gitRun("config", "user.name", "test")
	gitRun("add", "skills/demo/SKILL.md")
	gitRun("commit", "-qm", "demo")
	return src
}

func writeLocalInstalledSkill(t *testing.T, projectRoot, sourceRoot, body, treeSHA string) {
	t.Helper()
	dir := filepath.Join(projectRoot, ".agents", "skills", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: demo\nmetadata:\n    local-path: " + filepath.Join(sourceRoot, "skills", "demo") + "\n    tui-tree-sha: " + treeSHA + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillCheckFindsOutdatedCopies(t *testing.T) {
	fileCfg = fileConfig{}
	projectCfg = fileConfig{}
	projectCfgPath = ""
	t.Cleanup(func() {
		fileCfg = fileConfig{}
		projectCfg = fileConfig{}
		projectCfgPath = ""
	})
	source := makeCheckSource(t, "old")
	project := t.TempDir()
	skills, _, trees, _, err := loadSkills(config{Source: source})
	if err != nil || len(skills) != 1 {
		t.Fatalf("load source: skills=%v err=%v", skills, err)
	}
	oldTree := trees[filepath.Join(source, "skills", "demo")]
	writeLocalInstalledSkill(t, project, source, "old", oldTree)

	good, err := runSkillCheck(config{Source: source, Scope: "project"}, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(good.Issues) != 0 || good.Checked != 1 {
		t.Fatalf("current check = %+v", good)
	}

	path := filepath.Join(source, "skills", "demo", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: demo\n---\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(source, "add", "skills/demo/SKILL.md"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := runGit(source, "commit", "-qm", "update"); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}

	bad, err := runSkillCheck(config{Source: source, Scope: "project"}, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad.Issues) != 1 || bad.Issues[0].Kind != "outdated" {
		t.Fatalf("outdated check = %+v", bad)
	}
}

func TestSkillCheckIgnoreOutsideSkill(t *testing.T) {
	fileCfg = fileConfig{}
	projectCfg = fileConfig{}
	projectCfgPath = ""
	t.Cleanup(func() {
		fileCfg = fileConfig{}
		projectCfg = fileConfig{}
		projectCfgPath = ""
	})
	source := makeCheckSource(t, "body")
	project := t.TempDir()
	dir := filepath.Join(project, ".agents", "skills", "local-only")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: local-only\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bad, err := runSkillCheck(config{Source: source, Scope: "project"}, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad.Issues) != 1 || bad.Issues[0].Kind != "outside" {
		t.Fatalf("outside check = %+v", bad)
	}
	good, err := runSkillCheck(config{Source: source, Scope: "project", CheckIgnoreSkills: []string{"local-only"}}, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(good.Issues) != 0 || good.Ignored != 1 {
		t.Fatalf("ignored check = %+v", good)
	}
}

func TestSkillCheckIgnoreOutdatedManagedSkill(t *testing.T) {
	fileCfg = fileConfig{}
	projectCfg = fileConfig{}
	projectCfgPath = ""
	t.Cleanup(func() {
		fileCfg = fileConfig{}
		projectCfg = fileConfig{}
		projectCfgPath = ""
	})
	source := makeCheckSource(t, "old")
	project := t.TempDir()
	_, _, trees, _, err := loadSkills(config{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	oldTree := trees[filepath.Join(source, "skills", "demo")]
	writeLocalInstalledSkill(t, project, source, "old", oldTree)

	path := filepath.Join(source, "skills", "demo", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: demo\n---\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit(source, "add", "skills/demo/SKILL.md"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := runGit(source, "commit", "-qm", "update"); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}

	report, err := runSkillCheck(config{Source: source, Scope: "project", CheckIgnoreSkills: []string{"demo"}}, project)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 0 || report.Ignored != 1 || report.Checked != 0 {
		t.Fatalf("ignored managed check = %+v", report)
	}
}

func TestParseArgsVersionFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--version"}); !errors.Is(err, errVersion) {
		t.Fatalf("err = %v", err)
	}
	// gh-skill-check --version arrives as ["check", "--version"]
	if _, err := parseArgs([]string{"check", "--version"}); !errors.Is(err, errVersion) {
		t.Fatalf("check err = %v", err)
	}
}

func TestParseArgsRequiresSource(t *testing.T) {
	fileCfg = fileConfig{}
	projectCfg = fileConfig{}
	projectCfgPath = ""
	t.Cleanup(func() {
		fileCfg = fileConfig{}
		projectCfg = fileConfig{}
		projectCfgPath = ""
	})
	t.Setenv("GH_SKILL_DEFAULT_SOURCE", "")
	if _, err := parseArgs(nil); err == nil {
		t.Fatal("expected error when no source is configured")
	}
	cfg, err := parseArgs([]string{"Owner/Repo"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != "Owner/Repo" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestApplyPRTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "template.md")
	if err := os.WriteFile(path, []byte("## Summary\n{{title}}\n\n## Details\n{{body}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := prPlan{Title: "skill: demo", Body: "- source skill: `skills/demo`"}
	if err := applyPRTemplate(config{PRTemplate: "template.md"}, root, &plan); err != nil {
		t.Fatal(err)
	}
	want := "## Summary\nskill: demo\n\n## Details\n- source skill: `skills/demo`\n"
	if plan.Body != want {
		t.Fatalf("templated body = %q", plan.Body)
	}

	if err := os.WriteFile(path, []byte("checklist\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appended := prPlan{Title: "t", Body: "details"}
	if err := applyPRTemplate(config{PRTemplate: path}, root, &appended); err != nil {
		t.Fatal(err)
	}
	if appended.Body != "checklist\n\ndetails" {
		t.Fatalf("appended body = %q", appended.Body)
	}

	missing := prPlan{Title: "t", Body: "details"}
	if err := applyPRTemplate(config{PRTemplate: filepath.Join(root, "absent.md")}, root, &missing); err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestProjectConfigOverridesGlobalAndPins(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "src")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gh-skill-tui.toml"), []byte("source = \"project/private-skills\"\nbranch = \"release\"\nhash = \"deadbeef\"\ncheck-ignore-skill = [\"local-only\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
		fileCfg = fileConfig{}
		projectCfg = fileConfig{}
		projectCfgPath = ""
	})
	fileCfg = fileConfig{Source: "global/private-skills", Ref: "main", Pin: "global-pin"}
	if err := loadProjectConfig(root); err != nil {
		t.Fatal(err)
	}
	cfg, err := parseArgs([]string{"check"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Command != "check" || cfg.Source != "project/private-skills" || cfg.Ref != "release" || cfg.Pin != "deadbeef" {
		t.Fatalf("project config = %+v", cfg)
	}
	if len(cfg.CheckIgnoreSkills) != 1 || cfg.CheckIgnoreSkills[0] != "local-only" {
		t.Fatalf("ignore config = %v", cfg.CheckIgnoreSkills)
	}
	cli, err := parseArgs([]string{"check", "--source", "cli/private-skills", "--ref", "cli", "--pin", "cli-pin"})
	if err != nil {
		t.Fatal(err)
	}
	if cli.Source != "cli/private-skills" || cli.Ref != "cli" || cli.Pin != "cli-pin" {
		t.Fatalf("CLI precedence = %+v", cli)
	}
}

func TestBuildPlanAddsConfiguredPin(t *testing.T) {
	cfg := config{Source: "Owner/Repo", Pin: "deadbeef"}
	entries, _ := buildPlan(cfg, "gh", []string{"skills/demo/SKILL.md"}, allInstallTargets(), map[string]bool{"codex": true}, "project", false, "/proj", "", nil, nil, nil)
	if len(entries) != 1 || !containsStr(entries[0].Args, "--pin") || !containsStr(entries[0].Args, "deadbeef") {
		t.Fatalf("configured pin missing: %+v", entries)
	}
}
