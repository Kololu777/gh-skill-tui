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
	Source            string   `toml:"source"`
	Scope             string   `toml:"scope"`
	Force             bool     `toml:"force"`
	DefaultAgents     []string `toml:"default_agents"`
	AllowedSources    []string `toml:"allowed_sources"`
	AllowedLocalRoots []string `toml:"allowed_local_roots"`
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
	if err := toml.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	groups := []struct {
		name  string
		items []agentConfig
	}{
		{name: "agents", items: fileCfg.Agents},
		{name: "providers", items: fileCfg.LegacyProviders},
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

// mergeConfigAgents appends configured agents to the built-ins, overriding
// built-ins with the same name. Legacy [[providers]] entries are applied
// first, so the canonical [[agents]] spelling wins when both define a name.
func mergeConfigAgents(builtin []installTarget) []installTarget {
	configured := append([]agentConfig(nil), fileCfg.LegacyProviders...)
	configured = append(configured, fileCfg.Agents...)
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
