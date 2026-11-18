---
layout: default
title: Usage
nav_order: 3
permalink: /usage
---

# Usage

`cfgraft` synchronizes files and directories from Git repositories into local configuration paths. Git repositories are treated as the source of truth, while local destinations are user-owned targets protected by state-based safety checks.

## File Layout

`cfgraft` stores configuration and state under:

```text
~/.config/cfgraft/
```

The human-editable configuration file is:

```text
~/.config/cfgraft/config.yaml
```

Repository caches are stored under:

```text
~/.cache/cfgraft/repos/
```

Machine-managed sync state is stored separately from config under:

```text
~/.config/cfgraft/state/
```

Each configured source gets its own state file named after the source ID, such as:

```text
~/.config/cfgraft/state/home.yaml
```

Do not edit state files by hand. Hashes are intentionally kept out of `config.yaml` so the config remains the user-owned source of truth.

## Commands

Run `cfgraft` without a subcommand to show top-level help, including available subcommands and the configuration path.

```text
cfgraft
```

Each subcommand also supports `--help`:

```text
cfgraft sync --help
cfgraft diff --help
cfgraft version --help
cfgraft config
cfgraft validate
```

Use `sync` and `diff` for scripting and non-interactive workflows:

```text
cfgraft sync --dry-run
cfgraft diff --verbose
```

## Configuration

Edit `~/.config/cfgraft/config.yaml` to define repositories and mappings. Optional machine-local condition files enable selected sources or mappings. See [Configuration](CONFIG.md) for the file format, conditions, examples, and portable home-directory paths.

## Sync

Run:

```text
cfgraft sync
```

`sync` refreshes each configured repository cache before planning file changes:

1. Clone missing repositories.
2. Fetch existing repositories.
3. Reset and clean repository caches, discarding local cache modifications.
4. Check out the configured branch, tag, or commit.
5. For branch refs, update the cache to the latest remote branch state.

Repository caches under `~/.cache/cfgraft/repos/` are disposable application-managed data. Do not use them as workspaces.

After refresh, `sync` plans destination updates. A destination is safe to overwrite when it does not exist, already matches the repository content, or still matches the last hash recorded in state. If an existing destination has no state entry, or if it has drifted from the last accepted hash, it is a conflict.

Successful writes update the source-specific state file with the content hash that `cfgraft` placed or explicitly accepted.

If a source or mapping has been removed from `config.yaml`, `sync` prunes the no-longer-referenced state entries. This state cleanup does not delete destination files; remove unwanted destination files manually.

## Managing cfgraft's Own Configuration

A mapping can manage `~/.config/cfgraft/config.yaml` itself:

```yaml
mappings:
  - source: .config/cfgraft/config.yaml
    target: ~/.config/cfgraft/config.yaml
```

When sync would replace its own configuration, it validates the replacement and plans using its enabled sources and mappings before writing destinations or state. Newly referenced repositories are refreshed, new mappings are synchronized, and state for removed mappings is pruned in the same invocation. Files belonging to removed mappings are preserved. An additional sync is not required to activate the replacement configuration.

The existing safety checks still apply. A differing, unmanaged config requires `cfgraft sync --force`; a managed config that still matches its recorded state can update without force. Conflicts introduced by the replacement configuration also require force. `cfgraft sync --dry-run --force` previews the replacement and its mappings without writing destination files or state.

Sync reports `planning with updated configuration` when it follows a replacement. Invalid replacements, configuration cycles, and chains exceeding 16 updates fail before any destination or state writes. Removing the self-management mapping installs the accepted replacement and stops tracking it. Sync refuses to delete its active configuration when a directory mapping's config source disappears; restore the source file or remove that mapping.

## Sync Flags

`--dry-run` performs the full planning path, including repository refresh, but does not write destination files, delete files, or update state.

`--force` allows repository content to overwrite conflicts. An existing destination that differs from the source and has no recorded state is reported as not managed by cfgraft yet. Use `cfgraft sync --dry-run --force` to preview overwriting it and beginning to manage it, then `cfgraft sync --force` to apply that plan. Existing files that already match the source can be accepted into state without `--force`.

Dry-run output distinguishes blocked conflicts (with the command needed to resolve them) from forced overwrites or deletions. Both dry-run modes leave destination files and state unchanged and print the next command to preview conflict resolution or apply the plan. Sync flags require the `sync` subcommand: use `cfgraft sync --dry-run`, rather than `cfgraft --dry-run`.

`--verbose` shows repository refresh details and no-op decisions.

Boolean flags also accept environment defaults: `CFGRAFT_FORCE`, `CFGRAFT_DRY_RUN`, and `CFGRAFT_VERBOSE`. Values accept Go boolean forms such as `true`, `false`, `1`, and `0`. An explicit flag, including `--force=false`, overrides its environment variable. `CFGRAFT_VERBOSE` applies to both sync and diff.

Run `cfgraft config` for the file format and `cfgraft validate` to check the existing file and list sources and mappings without writing files or accessing Git. Sources are listed by ID with their repository and ref; each mapping shows its repository-relative source and resolved destination. Conditional entries show whether their markers are enabled. Validation checks YAML fields and paths, not repository availability or mapping contents. A missing config is an error. Other commands create an empty config on first use.

## Color Output

`cfgraft` uses color and styling for human-readable CLI output. Set `NO_COLOR` to a nonempty value to disable colored/styled output:

```text
NO_COLOR=1 cfgraft sync --dry-run
```

## Directory Mappings

A mapping source may be a file or directory. Directory mappings mirror files from the source directory into the destination directory, including dotfiles and dot-directories.

Directory sync can create files, update changed files, delete files that were previously managed and removed from the source, and create parent directories as needed. File permissions are preserved where supported, including executable bits.

Unmanaged extra files inside a destination directory are not deleted silently.

## Safe Deletion

If a file was previously placed by `cfgraft`, still matches the recorded state hash, and no longer exists in the source repository, sync may delete it.

If that managed file has drifted locally, deletion is treated as a conflict.

`cfgraft` does not delete files that it does not know it manages.

## Diff

Run:

```text
cfgraft diff
```

`diff` compares cached repository content against local destinations. It does not clone, fetch, pull, reset, or otherwise refresh repositories. Refresh belongs to `sync`; `diff` intentionally reports against the cache already on disk.

Text files are shown with a unified-style diff. Binary files are reported without dumping binary content:

```text
changed  /Users/jared/.local/bin/tool  binary files differ
```

`diff` always reports conflicts and the command needed to resolve them, including existing files that are not managed yet and files that drifted from the last accepted state. These warnings appear before text or binary differences, without requiring `--verbose`. Diff does not accept files into managed state.

## Binary Files

Binary files and text files are handled with the same safety model. Hashes are based on file contents, not modification times or other metadata.

## Symlinks

Symlink synchronization is not supported in the initial release. If a symlink is encountered in a repository source path, `cfgraft` warns and skips it.

## Manual Config Edits

Users may edit `config.yaml` directly. Manual config edits do not cause silent destination deletion.

If state entries remain for mappings that are no longer referenced by active config, `sync` reports them as stale managed entries and prunes them from state. Stale destination files are not silently deleted.
