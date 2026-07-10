package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultSource = "Kololu777/private-skills"

type config struct {
	Source      string
	Ref         string
	Scope       string
	Force       bool
	AgentArg    string
	DirArg      string
	InstallArgs []string
	SelectOnly  bool
	DryRun      bool
}

func main() {
	if err := loadFileConfig(configPathFromArgs(os.Args[1:])); err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			printUsage(os.Stdout)
			return
		}
		fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	final, ok := finalModel.(model)
	if !ok || final.cancelled {
		return
	}

	if cfg.SelectOnly && final.accepted {
		for _, path := range final.selectedPaths() {
			fmt.Println(path)
		}
	}
}

var errHelp = errors.New("help requested")

func parseArgs(args []string) (config, error) {
	cfg := config{
		Source: envDefault("GH_SKILL_DEFAULT_SOURCE", firstNonEmpty(fileCfg.Source, defaultSource)),
		Scope:  envDefault("GH_SKILL_DEFAULT_SCOPE", firstNonEmpty(fileCfg.Scope, "project")),
		Force:  fileCfg.Force,
	}
	sourceSet := false

	valueInstallFlags := map[string]bool{
		"--pin": true,
	}

	takeValue := func(i *int, name string) (string, error) {
		*i++
		if *i >= len(args) {
			return "", fmt.Errorf("%s requires a value", name)
		}
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			return cfg, errHelp
		case arg == "--select":
			cfg.SelectOnly = true
		case arg == "--dry-run":
			cfg.DryRun = true
		case arg == "--force" || arg == "-f":
			cfg.Force = true
		case arg == "--config":
			// already consumed by configPathFromArgs before parsing
			i++
		case strings.HasPrefix(arg, "--config="):
		case arg == "--ref":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.Ref = v
		case strings.HasPrefix(arg, "--ref="):
			cfg.Ref = strings.TrimPrefix(arg, "--ref=")
		case arg == "--source":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.Source = v
			sourceSet = true
		case strings.HasPrefix(arg, "--source="):
			cfg.Source = strings.TrimPrefix(arg, "--source=")
			sourceSet = true
		case arg == "--scope":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.Scope = v
		case strings.HasPrefix(arg, "--scope="):
			cfg.Scope = strings.TrimPrefix(arg, "--scope=")
		case arg == "--agent":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.AgentArg = v
		case strings.HasPrefix(arg, "--agent="):
			cfg.AgentArg = strings.TrimPrefix(arg, "--agent=")
		case arg == "--dir":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.DirArg = v
		case strings.HasPrefix(arg, "--dir="):
			cfg.DirArg = strings.TrimPrefix(arg, "--dir=")
		case arg == "--":
			cfg.InstallArgs = append(cfg.InstallArgs, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-"):
			cfg.InstallArgs = append(cfg.InstallArgs, arg)
			if valueInstallFlags[arg] && i+1 < len(args) {
				i++
				cfg.InstallArgs = append(cfg.InstallArgs, args[i])
			}
		case !sourceSet:
			cfg.Source = arg
			sourceSet = true
		default:
			cfg.InstallArgs = append(cfg.InstallArgs, arg)
		}
	}

	if cfg.Scope != "project" && cfg.Scope != "user" {
		return cfg, fmt.Errorf("--scope must be project or user, got %q", cfg.Scope)
	}

	return cfg, nil
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "usage: gh-skill-tui [--source OWNER/REPO|DIR] [--ref REF] [--scope project|user] [--agent AGENT]")
	fmt.Fprintln(out, "                    [--dir DIR] [--force] [--config PATH] [--select] [--dry-run]")
	fmt.Fprintln(out, "                    [OWNER/REPO|DIR] [gh skill install flags...]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "panels: 0 tree  1 skills  2 agents  3 installed  4 preview")
	fmt.Fprintln(out, "keys:   0-4/h/l focus panel  j/k move  space select  enter detail/confirm")
	fmt.Fprintln(out, "        i install/update/adopt plan  p PR/MR plan  d delete plan")
	fmt.Fprintln(out, "        P pick an outside-source destination  u scope  f overwrite override  s search  r rescan  q back/quit")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "config: ~/.config/gh-skill-tui/config.toml (or --config PATH)")
	fmt.Fprintln(out, "        source, scope, force, default_agents, allowed_sources, allowed_local_roots,")
	fmt.Fprintln(out, "        new_skill_dir, diff_command (e.g. \"delta --color-only --paging=never\"), [[agents]]")
	fmt.Fprintln(out, "        legacy [[providers]] is still accepted as a compatibility alias")
	fmt.Fprintln(out, "env (overrides config): GH_SKILL_DEFAULT_SOURCE, GH_SKILL_DEFAULT_AGENTS,")
	fmt.Fprintln(out, "        GH_SKILL_DEFAULT_SCOPE, GH_SKILL_ALLOWED_SOURCES, GH_SKILL_ALLOWED_LOCAL_ROOTS")
}
