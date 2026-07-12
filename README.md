# gh-skill-tui

**English** | [日本語](README.ja.md)

## What is gh-skill-tui?

`gh skill` is a very handy CLI that installs and manages agent skills from GitHub repositories. Because `gh skill` is a CLI, managing multiple agent skills across multiple agents at the same time gets complicated. `gh-skill-tui` lets you manage multiple agent skills and multiple agents at once in a TUI, which is easy to grasp visually and simple to operate.

## Demo

**Install** — select skills and agents, press `i`, review the plan, `enter`:

![install demo](demo/install.gif)

**Update** — the source moved on since install (`↓`); `i` proposes an update:

![update demo](demo/update.gif)

**Delete** — `d` removes the managed copies from every agent at once:

![delete demo](demo/delete.gif)

**Propose a PR** — a locally edited copy (`m`) is sent back to the source with `p`:

![pr demo](demo/pr.gif)

## Who is this for?

- You want to manage agent skills in a private repository instead of using external skills.
- You want to install and manage agent skills from a TUI.

## Installation

Building requires Go 1.22 or later. The tool drives the gh command, so install it first:
[GitHub CLI](https://cli.github.com/)

```sh
# Install gh-skill-tui / gh-skill-check
git clone https://github.com/Kololu777/gh-skill-tui.git
cd gh-skill-tui
go install .
ln -s "$(go env GOPATH)/bin/gh-skill-tui" "$(go env GOPATH)/bin/gh-skill-check"
```

`go install` places the binary in `$(go env GOPATH)/bin` (usually `~/go/bin`). Add that directory to your PATH and you can run `gh-skill-tui` / `gh-skill-check` directly. `gh-skill-check` is a symlink to the same binary; the behavior switches on the command name (`gh-skill-tui check` works the same way).

After installing, `gh-skill-tui --version` prints the version. Nix builds report the package version; source builds report the git revision at build time.

Or:

```sh
nix build --impure --expr \
  '(builtins.getFlake "nixpkgs").legacyPackages.${builtins.currentSystem}.callPackage ./package.nix {}'
./result/bin/gh-skill-tui OWNER/skills-repo
./result/bin/gh-skill-check
```

With `home-manager`:

```nix
home.packages = [ (pkgs.callPackage ./package.nix { }) ];
```

## Usage

### Starting

```sh
# When config.toml specifies the source
gh-skill-tui

gh-skill-tui OWNER/skills-repo
```

A source is required. If none is given via a CLI argument, the config file (`source`), or the `GH_SKILL_DEFAULT_SOURCE` environment variable, the command exits with an error.

To build an install / update / delete / PR plan, select the target skills and agents in panels `0:tree`, `1:skills`, and `2:agents`, then press the action key (`i` (install / update) / `d` (delete) / `p` (PR)). Panel `4:preview` shows a confirmation screen; review it and press `enter` to execute.

### Panels

| Panel         | Role                                                          |
| ------------- | ------------------------------------------------------------- |
| `0 tree`      | Shows source skills and outside skills as a tree              |
| `1 skills`    | Lists source skills and outside skills                        |
| `2 agents`    | Selects the agents to install into                            |
| `3 installed` | Shows how many skills are installed per destination           |
| `4 preview`   | Shows skill content, destinations, plans, and execution results |

#### On-screen marks

**Selection state**

- `[ ]` / `[x]` — whether the row is selected as an operation target; `[x]` means selected
- `[~]` — in the tree panel, only part of the subtree is selected

**Source vs local state**

- `✓` — installed from the configured source and up to date
- `↓` — the source was updated after installation
- `m` — the installed copy was edited locally
- `O` — a copy outside the configured source (placed manually, another source, etc.)

#### Key map

| Key                  | Action                                                                    |
| -------------------- | ------------------------------------------------------------------------- |
| `0`–`4`              | Switch panels; `4` jumps to the preview                                   |
| `h` / `l`            | Move to the previous / next panel                                         |
| `j` / `k`, `g` / `G` | Move the cursor; jump to the top / bottom                                 |
| `space`              | Select / deselect the current skill or agent; in the tree, the whole subtree |
| `i`                  | Build an install / update / adopt plan                                    |
| `p`                  | Build a PR / MR plan that proposes edited or outside skills back to the source |
| `d`                  | Build a plan that deletes managed copies                                  |
| `u`                  | Toggle between `project` and `user` scope                                 |
| `f`                  | Toggle force; in the agents panel it applies only to the current destination |
| `r`                  | Rescan installed skills                                                   |
| `s` / `/`            | Search skills by name or path                                             |
| `enter`              | Show details on the normal screen, execute on the plan screen, return to the TUI on the result screen |
| `q` / `esc`          | Go back, clear the search, or quit                                        |

When the preview is long, `ctrl+d` / `ctrl+u` scroll half a page.

### project scope and user scope

- **project scope** — places skills relative to the root of the Git repository found by walking up from the current directory (or the current directory when there is no Git repository). Suited to skills used per project
- **user scope** — places skills under your home directory. Suited to skills used across every project

## Configuration

The config file location is OS-specific (it follows Go's [`os.UserConfigDir()`](https://pkg.go.dev/os#UserConfigDir)).

| OS      | Path                                                     |
| ------- | -------------------------------------------------------- |
| Linux   | `~/.config/gh-skill-tui/config.toml`                     |
| macOS   | `~/Library/Application Support/gh-skill-tui/config.toml` |
| Windows | `%AppData%\gh-skill-tui\config.toml`                     |

You can also place `.gh-skill-tui.toml` in the directory you launch from.

```toml
source = "OWNER/private-skills" # GitHub repository source

# Optional: branch or hash
branch = "main"                         # ref also works
hash = "16c1dceffe9c1a80ef615d4347f065ffb71b3101"

scope = "project"               # select: [project / user]
default_agents = ["claude-code", "codex", "opencode"] # selected at startup

# Optional: diff command
diff_command = "delta --color-only --paging=never"

# Optional: PR / MR body template (path; relative paths resolve from the project root)
pr_template = ".github/skill_pr_template.md"

# Optional: check_ignore_skills (used by the `gh-skill-check` command)
check_ignore_skills = ["local-only", "tools/*"]
```

### PR / MR template (`pr_template`)

The body of the PRs / MRs created with `p` can be built from a template file. Point `pr_template` at a text file (Markdown, YAML, and so on). Relative paths resolve from the project root, and `~` works too.

The following placeholders are available inside the template:

- `{{title}}` — the generated PR title
- `{{body}}` — the generated body (source skill, copy origin, tree hash, and other details)

If the template does not contain `{{body}}`, the generated details are appended at the end.

```markdown
## Summary

{{title}}

## Details

{{body}}

## Checklist

- [ ] Reviewed SKILL.md
```

## Supported agents and destinations

| agent            | user scope                     | project scope      |
| ---------------- | ------------------------------ | ------------------ |
| `claude-code`    | `~/.claude/skills`             | `.claude/skills`   |
| `codex`          | `~/.codex/skills`              | `.agents/skills`   |
| `opencode`       | `~/.config/opencode/skills`    | `.opencode/skills` |
| `kimi`           | `~/.agents/skills`             | `.agents/skills`   |
| `github-copilot` | `~/.copilot/skills`            | `.agents/skills`   |
| `cursor`         | `~/.cursor/skills`             | `.agents/skills`   |
| `gemini`         | `~/.gemini/skills`             | `.agents/skills`   |
| `antigravity`    | `~/.gemini/antigravity/skills` | `.agents/skills`   |

## Checking skills (`gh-skill-check`)

Verifies that the skills of the configured source match the skills in the current project scope. Works well in CI or pre-commit as a command that confirms skills are up to date.

```sh
gh-skill-check
gh-skill-check --scope project
gh-skill-check --scope user
```

To exclude specific skills from the check, set `check_ignore_skills` in `.gh-skill-tui.toml`. Skills matching a pattern are ignored whether they are older than the source, locally edited, or outside the source.

```toml
check_ignore_skills = ["local-only", "tools/*"]
```

## Release Notes

See [release-notes.md](release-notes.md).

## License

MIT License
