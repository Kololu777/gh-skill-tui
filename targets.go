package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// installTarget maps one user-facing agent name to a physical destination.
// GHAgent is passed to `gh skill install --agent`; targets without a native
// gh agent value use --dir instead (for example OpenCode and Kimi).
type installTarget struct {
	Name       string
	Short      string
	GHAgent    string
	UserDir    string
	ProjectDir string
}

func allInstallTargets() []installTarget {
	return mergeConfigAgents(builtinInstallTargets())
}

func builtinInstallTargets() []installTarget {
	return []installTarget{
		{Name: "claude-code", Short: "claude", GHAgent: "claude-code", UserDir: "~/.claude/skills", ProjectDir: ".claude/skills"},
		{Name: "codex", Short: "codex", GHAgent: "codex", UserDir: "~/.codex/skills", ProjectDir: ".agents/skills"},
		{Name: "opencode", Short: "opencode", GHAgent: "", UserDir: "~/.config/opencode/skills", ProjectDir: ".opencode/skills"},
		// kimi-code scans ~/.agents/skills (user) and .agents/skills (project)
		// as its generic skill roots, plus ~/.kimi-code/skills as brand dir.
		{Name: "kimi", Short: "kimi", GHAgent: "", UserDir: "~/.agents/skills", ProjectDir: ".agents/skills"},
		{Name: "github-copilot", Short: "copilot", GHAgent: "github-copilot", UserDir: "~/.copilot/skills", ProjectDir: ".agents/skills"},
		{Name: "cursor", Short: "cursor", GHAgent: "cursor", UserDir: "~/.cursor/skills", ProjectDir: ".agents/skills"},
		{Name: "gemini", Short: "gemini", GHAgent: "gemini", UserDir: "~/.gemini/skills", ProjectDir: ".agents/skills"},
		{Name: "antigravity", Short: "antig", GHAgent: "antigravity", UserDir: "~/.gemini/antigravity/skills", ProjectDir: ".agents/skills"},
	}
}

func defaultSelectedAgents() map[string]bool {
	marked := make(map[string]bool)
	fallback := "claude-code,codex,opencode,kimi"
	active := activeFileConfig()
	if len(active.DefaultAgents) > 0 {
		fallback = strings.Join(active.DefaultAgents, ",")
	}
	names := envDefault("GH_SKILL_DEFAULT_AGENTS", fallback)
	valid := make(map[string]bool)
	for _, p := range allInstallTargets() {
		valid[p.Name] = true
	}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if valid[name] {
			marked[name] = true
		}
	}
	return marked
}

func (p installTarget) dirFor(scope, projectRoot string) string {
	if scope == "user" {
		return expandHome(p.UserDir)
	}
	return filepath.Join(projectRoot, p.ProjectDir)
}

func (p installTarget) displayDirFor(scope string) string {
	if scope == "user" {
		return p.UserDir
	}
	return p.ProjectDir
}

func localSkillArg(path string) string {
	dir := skill{Path: path}.Dir()
	return strings.TrimPrefix(dir, "skills/")
}

// allowedSources returns the OWNER/REPO slugs skills may come from
// (supply-chain policy). Local directory sources are always allowed.
func allowedSources() []string {
	active := activeFileConfig()
	fallback := envDefault("GH_SKILL_DEFAULT_SOURCE", active.Source)
	if len(active.AllowedSources) > 0 {
		fallback = strings.Join(active.AllowedSources, ",")
	}
	raw := envDefault("GH_SKILL_ALLOWED_SOURCES", fallback)
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sourceApproved(source string, allowed []string) bool {
	if isLocalSource(source) {
		return true
	}
	slug := strings.ToLower(strings.TrimSpace(source))
	for _, a := range allowed {
		if slug == a {
			return true
		}
	}
	return false
}

type planEntry struct {
	Skill  string
	Agent  string
	Dest   string
	Action string // install, update, reinstall, or adopt; display only
	Args   []string
	// local-source tracking: after a successful --from-local install the
	// TUI records TreeSha into the copy found under DestAbs whose
	// local-path equals SourceSkillAbs.
	DestAbs        string
	SourceSkillAbs string
	TreeSha        string
}

// buildPlan expands marked skills × install targets into concrete
// `gh skill install` invocations. Targets resolving to the same directory
// (e.g. codex and cursor at project scope share .agents/skills) are
// installed once; the rest are reported in skipped. sourceRoot/treeShas are
// set for local (git clone) sources and empty otherwise. needed filters out
// paths already current at an agent destination (nil = install everything);
// per-agent force overrides bypass the filter and add --force.
func buildPlan(cfg config, gh string, paths []string, targets []installTarget, marked map[string]bool, scope string, force bool, projectRoot string, sourceRoot string, treeShas map[string]string, needed func(string, installTarget) bool, forced map[string]bool) ([]planEntry, []string) {
	if gh == "" {
		gh = "gh"
	}
	local := isLocalSource(cfg.Source)
	base := func(path string) []string {
		if local {
			// --from-local only resolves skill names (optionally
			// namespaced), not paths: skills/team/foo -> team/foo
			return []string{gh, "skill", "install", cfg.Source, localSkillArg(path), "--from-local"}
		}
		return []string{gh, "skill", "install", cfg.Source, path}
	}
	finish := func(args []string, f bool) []string {
		if f {
			args = append(args, "--force")
		}
		if cfg.Pin != "" && !hasInstallPin(args) && !hasInstallPin(cfg.InstallArgs) {
			args = append(args, "--pin", cfg.Pin)
		}
		return append(args, cfg.InstallArgs...)
	}

	var entries []planEntry
	var skipped []string

	// gh prompts "Select target agent(s)" on a TTY whenever --agent is
	// missing, even with --dir (the answer never affects placement, which
	// --dir overrides). Pass a dummy agent to keep --dir installs prompt-free.
	dirAgent := cfg.AgentArg
	if dirAgent == "" {
		dirAgent = "github-copilot"
	}

	sourceAbs := func(path string) string {
		if sourceRoot == "" {
			return ""
		}
		return filepath.Join(sourceRoot, filepath.FromSlash(skill{Path: path}.Dir()))
	}

	switch {
	case cfg.DirArg != "":
		destAbs, _ := filepath.Abs(expandHome(cfg.DirArg))
		for _, path := range paths {
			entries = append(entries, planEntry{
				Skill:          path,
				Agent:          "--dir",
				Dest:           cfg.DirArg,
				Args:           finish(append(base(path), "--dir", cfg.DirArg, "--agent", dirAgent), force),
				DestAbs:        destAbs,
				SourceSkillAbs: sourceAbs(path),
				TreeSha:        treeShas[sourceAbs(path)],
			})
		}
	case cfg.AgentArg != "":
		destAbs := ""
		for _, p := range targets {
			if p.GHAgent == cfg.AgentArg {
				destAbs = p.dirFor(scope, projectRoot)
			}
		}
		for _, path := range paths {
			entries = append(entries, planEntry{
				Skill:          path,
				Agent:          cfg.AgentArg,
				Dest:           cfg.AgentArg,
				Args:           finish(append(base(path), "--agent", cfg.AgentArg, "--scope", scope), force),
				DestAbs:        destAbs,
				SourceSkillAbs: sourceAbs(path),
				TreeSha:        treeShas[sourceAbs(path)],
			})
		}
	default:
		seen := make(map[string]string)
		forceByDest := make(map[string]bool)
		var chosen []installTarget
		for _, p := range targets {
			if !marked[p.Name] {
				continue
			}
			dest := p.dirFor(scope, projectRoot)
			forceByDest[dest] = forceByDest[dest] || forced[p.Name]
			if prev, ok := seen[dest]; ok {
				skipped = append(skipped, fmt.Sprintf("%s: same destination as %s (%s)", p.Name, prev, p.displayDirFor(scope)))
				continue
			}
			seen[dest] = p.Name
			chosen = append(chosen, p)
		}
		for _, p := range chosen {
			pf := force || forceByDest[p.dirFor(scope, projectRoot)]
			var todo []string
			for _, path := range paths {
				if pf || needed == nil || needed(path, p) {
					todo = append(todo, path)
				}
			}
			if len(todo) == 0 {
				skipped = append(skipped, p.Name+": all up to date")
				continue
			}
			for _, path := range todo {
				args := base(path)
				if p.GHAgent != "" {
					args = append(args, "--agent", p.GHAgent, "--scope", scope)
				} else {
					args = append(args, "--dir", p.dirFor(scope, projectRoot), "--agent", dirAgent)
				}
				entries = append(entries, planEntry{
					Skill:          path,
					Agent:          p.Name,
					Dest:           p.displayDirFor(scope),
					Args:           finish(args, pf),
					DestAbs:        p.dirFor(scope, projectRoot),
					SourceSkillAbs: sourceAbs(path),
					TreeSha:        treeShas[sourceAbs(path)],
				})
			}
		}
	}
	return entries, skipped
}

func hasInstallPin(args []string) bool {
	for i, arg := range args {
		if arg == "--pin" && i+1 < len(args) {
			return true
		}
		if strings.HasPrefix(arg, "--pin=") {
			return true
		}
	}
	return false
}
