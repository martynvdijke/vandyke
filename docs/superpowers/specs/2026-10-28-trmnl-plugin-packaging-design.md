# TRMNL plugin ownership and release packaging

## Context

The four vanDyke TRMNL plugins originally lived in the shared
`martynvdijke/trmnl` monorepo. Their API surface, assets and release cadence are
specific to vanDyke, so they should ship with the ledger instead.

## Decision

Move the plugin sources and transform tests into `trmnl/` in this repository.
Remove the duplicates from the TRMNL monorepo after the first release from this
repository succeeds, so only one workflow can push a given TRMNL plugin ID.

The plugin IDs remain pinned:

| Plugin | ID |
| --- | ---: |
| `vandyke-dashboard` | 521908 |
| `vandyke-latest` | 521909 |
| `vandyke-photo` | 521910 |
| `vandyke-stats` | 521911 |

## Release packaging

`scripts/package-trmnl.py VERSION` creates:

- one zip per plugin, with the plugin directory as the archive root;
- one combined zip containing all four plugins and `trmnl/README.md`.

semantic-release runs the script in `prepareCmd` and attaches every
`dist/trmnl/*.zip` to the GitHub release through `@semantic-release/github`.
The `dist/` directory is ignored by git and Docker.

## CI

A reusable `trmnl.yaml` workflow:

- runs the Python transform tests;
- verifies packaging produces five non-empty zips;
- lints each plugin with `trmnlp`;
- on `main`, requires pinned IDs and pushes each plugin to TRMNL when
  `TRML_TOKEN` is available.

The release workflow calls this workflow before semantic-release, so a broken
plugin blocks the release instead of shipping untested assets.

## Rollback

If a plugin push fails, the source and IDs remain in git. Fix the plugin and
push again; `trmnlp push --force` updates the existing TRMNL plugin rather than
creating a duplicate.
