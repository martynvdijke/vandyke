# vanDyke TRMNL plugins

Four TRMNL e-ink plugins for a vanDyke ledger. They poll the public JSON API and
need only the base URL of the instance.

| Plugin | TRMNL ID | Shows |
| --- | ---: | --- |
| [`vandyke-stats`](plugins/vandyke-stats) | 521911 | Ledger totals, unique misspellings, last 30 days and the last slip |
| [`vandyke-latest`](plugins/vandyke-latest) | 521909 | The three newest misspellings |
| [`vandyke-photo`](plugins/vandyke-photo) | 521910 | The newest entry with a photo |
| [`vandyke-dashboard`](plugins/vandyke-dashboard) | 521908 | Stats, latest entries and the newest photo |

Every plugin exposes one custom field, **Address of vanDyke**, for example
`https://vandyke.example.com`. No API key is needed while the ledger allows
public reads.

## Install

Download the versioned zips from the
[vanDyke releases](https://github.com/martynvdijke/vandyke/releases) and add
them to TRMNL, or push a plugin from source:

```sh
TRMNL_API_KEY=... trmnlp push --force --dir trmnl/plugins/vandyke-stats
```

The plugin IDs are pinned in each `src/settings.yml`, so pushes update the
existing TRMNL plugins instead of creating duplicates.

## Development

```sh
# Transform tests (stdlib only)
python3 trmnl/tests/test_vandyke_transform.py

# Lint every plugin (requires trmnl_preview)
for d in trmnl/plugins/*/; do trmnlp lint --dir "$d"; done

# Build release zips locally
make trmnl-package
```

`make trmnl-package` writes per-plugin archives and a combined archive to
`dist/trmnl/`. The release workflow runs the same packaging step and attaches
the zips to the GitHub release.

## Layout

Each plugin follows the standard TRMNL structure:

```
plugins/<name>/
├── .trmnlp.yml
├── icon-color.svg
├── icon-mono.svg
└── src/
    ├── settings.yml
    ├── transform.py
    └── four Liquid views
```

The transforms use only the Python standard library and degrade to a readable
`error` value when the ledger is unreachable.
