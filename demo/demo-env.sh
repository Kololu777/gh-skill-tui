#!/usr/bin/env bash
# demo-env.sh — isolated environment for the VHS demo tapes in this directory.
#
# The tapes never record your real HOME: everything runs inside a throwaway
# fake HOME (default /tmp/demo) so paths render as ~/… or /tmp/demo/… and no
# locally installed skills appear. Keep DEMO_ROOT short: the resolved gh path
# (e.g. /tmp/demo/bin/gh) is shown verbatim in the recorded install plans.
# The skills source is the orphan branch `gh-skill-tui-demo` on
# Kololu777/private-skills, pinned by commit so recordings are reproducible.
#
# Usage:
#   ./demo-env.sh setup            # create env + build/link binaries
#   ./demo-env.sh state-install    # before recording install.tape
#   ./demo-env.sh state-update     # before update.tape
#   ./demo-env.sh state-delete     # before delete.tape
#   ./demo-env.sh state-pr         # before pr.tape
#
# Record with:
#   DEMO_ROOT=/tmp/demo vhs install.tape
#
# pr.tape creates a real branch + PR on the source repository; close the PR
# and delete the gh-skill-tui/* branch afterwards. Remove /tmp/demo when done.
set -euo pipefail

DEMO_ROOT="${DEMO_ROOT:-/tmp/demo}"
SOURCE="Kololu777/private-skills"
V1="0a0f7b0" # gh-skill-tui-demo branch: seed skills (v1)
V2="9919ac3" # gh-skill-tui-demo branch: hello-world edited upstream (v2)

FAKE_HOME="$DEMO_ROOT"
PROJECT="$FAKE_HOME/myproject"

write_config() { # $1 = pinned hash
	cat >"$FAKE_HOME/.config/gh-skill-tui/config.toml" <<-EOF
		source = "$SOURCE"
		hash = "$1"
		default_agents = ["claude-code", "codex"]
	EOF
}

clean_installs() {
	rm -rf "$PROJECT/.claude" "$PROJECT/.agents" "$PROJECT/.opencode"
}

# Installs run inside the fake HOME with a minimal PATH, exactly like the
# recorded session, so injected tracking metadata matches what the TUI sees.
demo_install() { # $1 = skill path, $2 = pin, $3... = agents
	local skill="$1" pin="$2" agent
	shift 2
	for agent in "$@"; do
		(
			# shellcheck disable=SC1091
			source "$DEMO_ROOT/env.sh"
			gh skill install "$SOURCE" "$skill" \
				--pin "$pin" --agent "$agent" --scope project --force >/dev/null
		)
	done
}

case "${1:?usage: demo-env.sh setup|state-install|state-update|state-delete|state-pr}" in
setup)
	mkdir -p "$FAKE_HOME/.config/gh-skill-tui" "$FAKE_HOME/bin" "$PROJECT"
	cat >"$FAKE_HOME/.gitconfig" <<-'EOF'
		[user]
			name = demo
			email = demo@example.com
		[init]
			defaultBranch = main
	EOF
	git -C "$PROJECT" init -q 2>/dev/null || true

	# gh >= 2.90 is needed for the built-in `gh skill` command; /usr/bin/gh
	# may be older, so pin the demo to whatever `gh` resolves to right now.
	ln -sf "$(readlink -f "$(command -v gh)")" "$FAKE_HOME/bin/gh"

	bin="${GH_SKILL_TUI_BIN:-}"
	if [ -z "$bin" ]; then
		bin="$DEMO_ROOT/gh-skill-tui.bin"
		(cd "$(dirname "$0")/.." && go build -o "$bin" .)
	fi
	ln -sf "$bin" "$FAKE_HOME/bin/gh-skill-tui"

	# The token is fetched while the real HOME is still in effect and lives
	# only in the session environment; it is never written to disk.
	cat >"$DEMO_ROOT/env.sh" <<-EOF
		export GH_TOKEN="\${GH_TOKEN:-\$(gh auth token 2>/dev/null)}"
		export HOME="$FAKE_HOME"
		export PATH="\$HOME/bin:/usr/bin:/bin"
		unset XDG_CONFIG_HOME XDG_DATA_HOME
		cd "\$HOME/myproject"
	EOF
	write_config "$V1"
	echo "demo env ready at $DEMO_ROOT"
	;;
state-install)
	clean_installs
	write_config "$V1"
	;;
state-update)
	clean_installs
	write_config "$V1"
	demo_install skills/hello-world "$V1" claude-code codex
	demo_install skills/code-review-checklist "$V1" claude-code codex
	write_config "$V2" # source moved on: hello-world now shows ↓
	;;
state-delete)
	clean_installs
	write_config "$V2"
	demo_install skills/hello-world "$V2" claude-code codex
	demo_install skills/commit-message "$V2" claude-code codex
	;;
state-pr)
	clean_installs
	write_config "$V2"
	demo_install skills/hello-world "$V2" claude-code codex
	# a local improvement on the claude-code copy -> the TUI shows `m`
	cat >>"$PROJECT/.claude/skills/hello-world/SKILL.md" <<-'EOF'

		## Follow-up

		- After greeting, offer one relevant follow-up question.
	EOF
	;;
*)
	echo "unknown mode: $1" >&2
	exit 2
	;;
esac
