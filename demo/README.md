# VHS demo recordings

`install.gif` / `update.gif` / `delete.gif` / `pr.gif` are recorded with
[VHS](https://github.com/charmbracelet/vhs) from the `*.tape` scripts here.
Nothing from the real HOME is recorded: every tape runs inside a throwaway
fake HOME (`/tmp/demo`), and the skills source is the orphan branch
`gh-skill-tui-demo` on `Kololu777/private-skills`, which contains only
generated sample skills and is pinned by commit for reproducibility.

## Re-recording

Prerequisites: `vhs`, `gh` >= 2.90 (built-in `gh skill`), `gh auth login`
done, and the `gh-skill-tui-demo` branch present on the source repository.

```sh
./demo-env.sh setup            # build the binary, create /tmp/demo

./demo-env.sh state-install && DEMO_ROOT=/tmp/demo vhs install.tape
./demo-env.sh state-update  && DEMO_ROOT=/tmp/demo vhs update.tape
./demo-env.sh state-delete  && DEMO_ROOT=/tmp/demo vhs delete.tape
./demo-env.sh state-pr      && DEMO_ROOT=/tmp/demo vhs pr.tape
```

What each tape shows:

| Tape           | Start state (set by `demo-env.sh`)                     | On camera                                   |
| -------------- | ------------------------------------------------------ | ------------------------------------------- |
| `install.tape` | nothing installed, pinned to v1                        | select 2 skills → `i` → 4 installs          |
| `update.tape`  | 2 skills installed at v1, source pinned to v2 (`↓`)    | select outdated skill → `i` → update        |
| `delete.tape`  | 2 skills installed at v2 (`✓`)                          | select both → `d` → managed copies removed  |
| `pr.tape`      | skill installed at v2, claude-code copy edited (`m`)   | select → `p` → diff plan → real PR created  |

`pr.tape` creates a real branch + PR on the source repository; close the PR
and delete the `gh-skill-tui/*` branch afterwards:

```sh
gh pr close <N> --repo Kololu777/private-skills --delete-branch
```

Remove `/tmp/demo` when done.

## Notes

- The GH token is fetched at record time via `gh auth token` and lives only
  in the session environment; it is never written to disk or shown on screen.
- nixpkgs' vhs 0.11 ships a ttyd whose libwebsockets cannot dlopen its libuv
  event plugin; if `vhs` fails with `could not open ttyd`, run it with
  `LD_LIBRARY_PATH=<nix store path of libwebsockets>/lib` (find it with
  `ldd "$(readlink -f "$(command -v vhs)" | sed 's/\.vhs-wrapped//')"` or
  `ldd $(nix-store -qR $(command -v vhs) | grep ttyd)/bin/ttyd | grep websockets`).
