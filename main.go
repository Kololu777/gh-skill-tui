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
	Source            string
	Ref               string
	Pin               string
	Scope             string
	Force             bool
	AgentArg          string
	DirArg            string
	InstallArgs       []string
	CheckIgnoreSkills []string
	Command           string
	SelectOnly        bool
	DryRun            bool
}

func main() {
	if err := loadFileConfig(configPathFromArgs(os.Args[1:])); err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}
	if err := loadProjectConfig(findProjectRoot()); err != nil {
		fmt.Fprintln(os.Stderr, "project config error:", err)
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
	if cfg.Command == "check" {
		if err := runCheckCommand(cfg, os.Stdout); err != nil {
			if errors.Is(err, errCheckFailed) {
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "check error:", err)
			os.Exit(2)
		}
		return
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
	active := activeFileConfig()
	cfg := config{
		Source:            envDefault("GH_SKILL_DEFAULT_SOURCE", firstNonEmpty(active.Source, defaultSource)),
		Ref:               firstNonEmpty(active.Branch, active.Ref),
		Pin:               firstNonEmpty(active.Hash, active.Commit, active.Pin),
		Scope:             envDefault("GH_SKILL_DEFAULT_SCOPE", firstNonEmpty(active.Scope, "project")),
		Force:             active.Force,
		CheckIgnoreSkills: checkIgnoreSkills(active),
	}
	sourceSet := false
	commandSet := false
	refSet := false
	pinSet := false

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
			refSet = true
		case arg == "--branch":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.Ref = v
			refSet = true
		case strings.HasPrefix(arg, "--ref="):
			cfg.Ref = strings.TrimPrefix(arg, "--ref=")
			refSet = true
		case strings.HasPrefix(arg, "--branch="):
			cfg.Ref = strings.TrimPrefix(arg, "--branch=")
			refSet = true
		case arg == "--hash" || arg == "--commit":
			v, err := takeValue(&i, arg)
			if err != nil {
				return cfg, err
			}
			cfg.Pin = v
			pinSet = true
		case strings.HasPrefix(arg, "--hash="):
			cfg.Pin = strings.TrimPrefix(arg, "--hash=")
			pinSet = true
		case strings.HasPrefix(arg, "--commit="):
			cfg.Pin = strings.TrimPrefix(arg, "--commit=")
			pinSet = true
		case strings.HasPrefix(arg, "--pin="):
			cfg.Pin = strings.TrimPrefix(arg, "--pin=")
			pinSet = true
			cfg.InstallArgs = append(cfg.InstallArgs, arg)
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
		case !commandSet && arg == "check":
			cfg.Command = "check"
			commandSet = true
		case strings.HasPrefix(arg, "-"):
			cfg.InstallArgs = append(cfg.InstallArgs, arg)
			if valueInstallFlags[arg] && i+1 < len(args) {
				i++
				if arg == "--pin" {
					cfg.Pin = args[i]
					pinSet = true
				}
				cfg.InstallArgs = append(cfg.InstallArgs, args[i])
			}
		case !commandSet && !sourceSet:
			cfg.Source = arg
			sourceSet = true
		case commandSet && cfg.Command == "check" && !sourceSet:
			cfg.Source = arg
			sourceSet = true
		default:
			cfg.InstallArgs = append(cfg.InstallArgs, arg)
		}
	}

	if cfg.Scope != "project" && cfg.Scope != "user" {
		return cfg, fmt.Errorf("--scope must be project or user, got %q", cfg.Scope)
	}
	if cfg.Command == "check" {
		cfg = applyCheckTableConfig(cfg, sourceSet, refSet, pinSet)
	}

	return cfg, nil
}

func printUsage(out *os.File) {
	lines := []string{
		"usage: gh-skill-tui [--source OWNER/REPO|DIR] [--ref REF] [--scope project|user] [--agent AGENT]",
		"                    [--dir DIR] [--force] [--config PATH] [--select] [--dry-run]",
		"                    [OWNER/REPO|DIR] [gh skill install flags...]",
		"       gh-skill-tui check [--source OWNER/REPO|DIR] [--ref REF] [--pin HASH]",
		"",
		"panels: 0 tree  1 skills  2 agents  3 installed  4 preview",
		"keys:   0-4/h/l focus panel  j/k move  space select  enter detail/confirm",
		"        i install/update/adopt plan  p PR/MR plan  d delete plan",
		"        P pick an outside-source destination  u scope  f overwrite override  s search  r rescan  q back/quit",
		"",
		"config: ~/.config/gh-skill-tui/config.toml (or --config PATH)",
		"        project-local .gh-skill-tui.toml (aliases: gh-skill-tui.toml, .gst.toml, gst.toml)",
		"        branch/ref and hash/commit/pin may pin a project to one source revision",
		"        check_ignore_skills (or [check].ignore_skills) exempts outside skills",
		"        source, scope, force, default_agents, allowed_sources, allowed_local_roots,",
		"        new_skill_dir, diff_command (e.g. \"delta --color-only --paging=never\"), [[agents]]",
		"        legacy [[providers]] is still accepted as a compatibility alias",
		"env (overrides config): GH_SKILL_DEFAULT_SOURCE, GH_SKILL_DEFAULT_AGENTS,",
		"        GH_SKILL_DEFAULT_SCOPE, GH_SKILL_ALLOWED_SOURCES, GH_SKILL_ALLOWED_LOCAL_ROOTS",
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return
		}
	}
}
