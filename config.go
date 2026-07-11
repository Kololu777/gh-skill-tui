package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// fileConfig is ~/.config/gh-skill-tui/config.toml. Every setting is
// optional; precedence is CLI flag > environment variable > config file >
// built-in default.
type fileConfig struct {
	Source                  string   `toml:"source"`
	Scope                   string   `toml:"scope"`
	Ref                     string   `toml:"ref"`
	Branch                  string   `toml:"branch"`
	Pin                     string   `toml:"pin"`
	Hash                    string   `toml:"hash"`
	Commit                  string   `toml:"commit"`
	Force                   bool     `toml:"force"`
	DefaultAgents           []string `toml:"default_agents"`
	AllowedSources          []string `toml:"allowed_sources"`
	AllowedLocalRoots       []string `toml:"allowed_local_roots"`
	CheckIgnoreSkills       []string `toml:"check_ignore_skills"`
	CheckIgnoreSkillsHyphen []string `toml:"check-ignore-skills"`
	// CheckIgnoreSkill keeps the singular, hyphenated spelling available for
	// project files that want a setting named like the command itself.
	CheckIgnoreSkill []string        `toml:"check-ignore-skill"`
	IgnoreSkills     []string        `toml:"ignore_skills"`
	Check            checkFileConfig `toml:"check"`
	// DiffCommand pipes unified diffs through an external colorizer
	// (e.g. "delta --color-only --paging=never"); its ANSI output is
	// rendered as-is. Empty uses the built-in colorizer.
	DiffCommand string `toml:"diff_command"`
	// NewSkillDir is where p places outside-source skills in the source when
	// no better destination can be inferred (e.g. "skills/share"). Empty
	// falls back to a source dir named "share".
	NewSkillDir string `toml:"new_skill_dir"`

	// Agents is the public name. Providers remains a deprecated input alias
	// so existing config files continue to work during the terminology change.
	Agents          []agentConfig `toml:"agents"`
	LegacyProviders []agentConfig `toml:"providers"`
}

// checkFileConfig is accepted under [check] as well as at the project TOML
// root. The root form keeps project files short, while the table form makes
// it clear that ignore rules affect `gst check` only.
type checkFileConfig struct {
	Source                  string   `toml:"source"`
	Ref                     string   `toml:"ref"`
	Branch                  string   `toml:"branch"`
	Pin                     string   `toml:"pin"`
	Hash                    string   `toml:"hash"`
	Commit                  string   `toml:"commit"`
	CheckIgnoreSkills       []string `toml:"check_ignore_skills"`
	CheckIgnoreSkillsHyphen []string `toml:"check-ignore-skills"`
	CheckIgnoreSkill        []string `toml:"check-ignore-skill"`
	IgnoreSkills            []string `toml:"ignore_skills"`
}

// agentConfig declares an extra agent target (or overrides a built-in one by
// name). GHAgent left empty means gh has no native support and the
// skill is installed with --dir.
type agentConfig struct {
	Name       string `toml:"name"`
	Short      string `toml:"short"`
	GHAgent    string `toml:"agent"`
	UserDir    string `toml:"user_dir"`
	ProjectDir string `toml:"project_dir"`
}

// fileCfg is loaded once at startup; the zero value means "no config file".
var fileCfg fileConfig

// projectCfg is the optional configuration found in the current repository.
// It is deliberately kept separate from fileCfg: the latter is the user's
// global config and is loaded from --config before argument parsing.
var projectCfg fileConfig
var projectCfgPath string

var projectConfigNames = []string{
	".gh-skill-tui.toml",
	"gh-skill-tui.toml",
	".gst.toml",
	"gst.toml",
}

func defaultConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "gh-skill-tui", "config.toml")
	}
	return ""
}

// configPathFromArgs pre-scans the arguments for --config so the file can be
// loaded before regular flag parsing needs its defaults.
func configPathFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return defaultConfigPath()
}

// loadFileConfig reads the config file into fileCfg. A missing file is fine;
// a malformed one is a hard error (fail loudly, like a broken flake).
func loadFileConfig(path string) error {
	fileCfg = fileConfig{}
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := unmarshalConfig(path, data, &fileCfg); err != nil {
		return err
	}
	return validateFileConfig(path, fileCfg)
}

// loadProjectConfig finds the nearest project-local TOML. A project file is
// intentionally discovered from the current directory upward, so running
// gst in a subdirectory still uses the repository's policy. The local file
// wins over the user config, but CLI flags and environment variables retain
// their normal higher precedence.
func loadProjectConfig(root string) error {
	projectCfg = fileConfig{}
	projectCfgPath = ""
	path := findProjectConfig(root)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := unmarshalConfig(path, data, &projectCfg); err != nil {
		return err
	}
	if err := validateFileConfig(path, projectCfg); err != nil {
		return err
	}
	projectCfgPath = path
	return nil
}

func unmarshalConfig(path string, data []byte, dst *fileConfig) error {
	if err := toml.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	return nil
}

func validateFileConfig(path string, cfg fileConfig) error {
	groups := []struct {
		name  string
		items []agentConfig
	}{
		{name: "agents", items: cfg.Agents},
		{name: "providers", items: cfg.LegacyProviders},
	}
	for _, group := range groups {
		for i, item := range group.items {
			if item.Name == "" || item.UserDir == "" || item.ProjectDir == "" {
				return fmt.Errorf("%s: %s[%d] needs name, user_dir and project_dir", path, group.name, i)
			}
		}
	}
	return nil
}

// findProjectConfig returns the nearest supported project config. Multiple
// names are accepted for a gentle migration from the short `gst` spelling to
// the canonical `.gh-skill-tui.toml`; the first name in the list wins.
func findProjectConfig(root string) string {
	start, err := os.Getwd()
	if err != nil {
		return ""
	}
	if root == "" {
		root = start
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		for _, name := range projectConfigNames {
			path := filepath.Join(dir, name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path
			}
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
	}
	return ""
}

// activeFileConfig overlays project-local values on the user config. Empty
// values in the local file leave the global value intact; this is sufficient
// for project policy and avoids making the existing optional config fields
// pointer-heavy.
func activeFileConfig() fileConfig {
	out := fileCfg
	p := projectCfg
	if p.Source != "" {
		out.Source = p.Source
	}
	if p.Scope != "" {
		out.Scope = p.Scope
	}
	if p.Ref != "" {
		out.Ref = p.Ref
	}
	if p.Branch != "" {
		out.Branch = p.Branch
	}
	if p.Pin != "" {
		out.Pin = p.Pin
	}
	if p.Hash != "" {
		out.Hash = p.Hash
	}
	if p.Commit != "" {
		out.Commit = p.Commit
	}
	if p.Force {
		out.Force = true
	}
	if len(p.DefaultAgents) > 0 {
		out.DefaultAgents = p.DefaultAgents
	}
	if len(p.AllowedSources) > 0 {
		out.AllowedSources = p.AllowedSources
	}
	if len(p.AllowedLocalRoots) > 0 {
		out.AllowedLocalRoots = p.AllowedLocalRoots
	}
	if p.DiffCommand != "" {
		out.DiffCommand = p.DiffCommand
	}
	if p.NewSkillDir != "" {
		out.NewSkillDir = p.NewSkillDir
	}
	if len(p.Agents) > 0 {
		out.Agents = p.Agents
	}
	if len(p.LegacyProviders) > 0 {
		out.LegacyProviders = p.LegacyProviders
	}
	if len(p.CheckIgnoreSkills) > 0 || len(p.CheckIgnoreSkillsHyphen) > 0 || len(p.CheckIgnoreSkill) > 0 || len(p.IgnoreSkills) > 0 || hasCheckConfig(p.Check) {
		out.CheckIgnoreSkills = append([]string(nil), p.CheckIgnoreSkills...)
		out.CheckIgnoreSkillsHyphen = append([]string(nil), p.CheckIgnoreSkillsHyphen...)
		out.CheckIgnoreSkill = append([]string(nil), p.CheckIgnoreSkill...)
		out.IgnoreSkills = append([]string(nil), p.IgnoreSkills...)
		out.Check = p.Check
	}
	return out
}

func hasCheckConfig(cfg checkFileConfig) bool {
	return cfg.Source != "" || cfg.Ref != "" || cfg.Branch != "" || cfg.Pin != "" ||
		cfg.Hash != "" || cfg.Commit != "" || len(cfg.CheckIgnoreSkills) > 0 ||
		len(cfg.CheckIgnoreSkillsHyphen) > 0 || len(cfg.CheckIgnoreSkill) > 0 || len(cfg.IgnoreSkills) > 0
}

func checkIgnoreSkills(cfg fileConfig) []string {
	var out []string
	out = append(out, cfg.CheckIgnoreSkills...)
	out = append(out, cfg.CheckIgnoreSkillsHyphen...)
	out = append(out, cfg.CheckIgnoreSkill...)
	out = append(out, cfg.IgnoreSkills...)
	out = append(out, cfg.Check.CheckIgnoreSkills...)
	out = append(out, cfg.Check.CheckIgnoreSkillsHyphen...)
	out = append(out, cfg.Check.CheckIgnoreSkill...)
	out = append(out, cfg.Check.IgnoreSkills...)
	return uniqueStrings(out)
}

// applyCheckTableConfig applies the optional [check] table after normal
// argument parsing. Root-level project settings are shared with the TUI;
// table-level source settings are check-only and still yield to CLI flags.
func applyCheckTableConfig(cfg config, sourceSet, refSet, pinSet bool) config {
	table := fileCfg.Check
	if projectCfgPath != "" {
		table = projectCfg.Check
	}
	if !sourceSet && os.Getenv("GH_SKILL_DEFAULT_SOURCE") == "" && table.Source != "" {
		cfg.Source = table.Source
	}
	if !refSet {
		if table.Branch != "" {
			cfg.Ref = table.Branch
		} else if table.Ref != "" {
			cfg.Ref = table.Ref
		}
	}
	if !pinSet {
		cfg.Pin = firstNonEmpty(table.Hash, table.Commit, table.Pin, cfg.Pin)
	}
	var ignores fileConfig
	ignores.CheckIgnoreSkills = table.CheckIgnoreSkills
	ignores.CheckIgnoreSkillsHyphen = table.CheckIgnoreSkillsHyphen
	ignores.CheckIgnoreSkill = table.CheckIgnoreSkill
	ignores.IgnoreSkills = table.IgnoreSkills
	cfg.CheckIgnoreSkills = uniqueStrings(append(cfg.CheckIgnoreSkills, checkIgnoreSkills(ignores)...))
	return cfg
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// mergeConfigAgents appends configured agents to the built-ins, overriding
// built-ins with the same name. Legacy [[providers]] entries are applied
// first, so the canonical [[agents]] spelling wins when both define a name.
func mergeConfigAgents(builtin []installTarget) []installTarget {
	active := activeFileConfig()
	configured := append([]agentConfig(nil), active.LegacyProviders...)
	configured = append(configured, active.Agents...)
	if len(configured) == 0 {
		return builtin
	}
	byName := make(map[string]int)
	out := append([]installTarget(nil), builtin...)
	for i, p := range out {
		byName[p.Name] = i
	}
	for _, pc := range configured {
		p := installTarget(pc)
		if p.Short == "" {
			p.Short = p.Name
		}
		if i, ok := byName[p.Name]; ok {
			out[i] = p
		} else {
			byName[p.Name] = len(out)
			out = append(out, p)
		}
	}
	return out
}
