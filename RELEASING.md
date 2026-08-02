# Releasing gh-skill-tui

The repository uses two permanent branches:

- `main` is the default branch and always points to the latest published
  release. This makes an unqualified `nix run github:Kololu777/gh-skill-tui`
  resolve to released source.
- `release` is the integration hub for the next release. Feature, fix,
  documentation, and Dependabot pull requests target this branch.

All other working and release-candidate branches are temporary. Published
versions are retained as `vX.Y.Z` Git tags and GitHub Releases, not as `vX.Y`
branches.

## Development flow

1. Create a short-lived branch from `release`.
2. Open a pull request back to `release`.
3. Let the CI, lint, and workflow-lint checks pass.
4. Merge the pull request. The Latest Changes workflow records the PR under
   `## Latest Changes` in `release-notes.md`.

Dependabot is configured to use `release` as its target branch.

## Preparing a release

Run **Actions → Prepare Release → Run workflow** and enter the exact `X.Y.Z`
version. The workflow:

1. checks that the version is greater than the version in `package.nix` and
   that `vX.Y.Z` does not exist;
2. creates `release-vX.Y.Z-<run-id>-<attempt>` from `release`;
3. updates `package.nix` and freezes `## Latest Changes` under a dated version
   heading;
4. opens a `release`-labeled pull request to `main`; and
5. enables merge-commit auto-merge, which still waits for every required
   branch check.

Clear the `auto_merge` input to require a final manual merge click.

Do not merge other pull requests into `release` while a release-candidate PR
is open. The candidate is an immutable snapshot, and the publish workflow
intentionally refuses to overwrite a `release` branch that moved after that
snapshot. New pull requests can still be reviewed and should wait for the
release to finish before merging.

## Publishing

Merging the candidate PR starts the Publish Release workflow. It:

1. reads the version from the tested merge commit;
2. creates the permanent annotated `vX.Y.Z` tag and a draft GitHub Release;
3. builds Linux and macOS binaries for amd64 and arm64;
4. verifies the Linux binary version and uploads all binaries plus checksums;
5. publishes the completed GitHub Release;
6. fast-forwards `release` to the tagged merge commit; and
7. removes the temporary release-candidate branch.

After publication, clean macOS, Linux amd64, and Linux arm64 runners verify
that `gh extension install` selects the new binary and reports the new version.

The tag is intentionally retained. GitHub Releases, `go install ...@latest`,
and version-pinned Nix flake references use it. The temporary branch is not.

## Repository settings

The pipeline expects:

- `main` to be the default branch;
- merge commits and repository auto-merge to be enabled;
- the `main` ruleset to require `CI / format, test, vet, build`,
  `CI / golangci-lint`, and `Workflow lint / actionlint`;
- merges to `release` to pause while a release-candidate PR is open;
- the `release` branch to accept the Latest Changes bot commit; and
- a `RELEASE_TOKEN` Actions secret containing a fine-grained personal access
  token with repository Contents, Issues, and Pull Requests read/write access.

The custom token is required for a fully unattended release. Pull requests
created with the built-in `GITHUB_TOKEN` can require manual workflow approval,
and events created by that token are intentionally prevented from recursively
starting most workflows.

## Initial migration

Before the first release with this pipeline, synchronize the permanent
`release` branch to the current `main` commit. The old `develop`, `vX.Y`, and
conflict-resolution branches can be removed only after the new flow has
successfully published and installed one release. Existing `vX.Y.Z` tags and
GitHub Releases must remain.

The existing `release` and `main` release notes have diverged. Reconcile
`release-notes.md` while synchronizing the branches so that the next
`## Latest Changes` section contains only unreleased changes.
