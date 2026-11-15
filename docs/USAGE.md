---
layout: default
title: Usage
nav_order: 3
permalink: /usage
---

# Usage

`cfgraft` synchronizes files and directories from Git repositories into local configuration paths. Git repositories are treated as the source of truth, while local destinations are user-owned targets protected by state-based safety checks.

## File Layout

`cfgraft` stores its files under:

```text
~/.config/cfgraft/
```

The human-editable configuration file is:

```text
~/.config/cfgraft/config.yaml
```

Repository caches are stored under:

```text
~/.config/cfgraft/repos/
```

Machine-managed sync state is stored separately from config under:

```text
~/.config/cfgraft/state/
```

Each configured source gets its own state file named after the source ID, such as:

```text
~/.config/cfgraft/state/home.yaml
```

Older `~/.config/cfgraft/state.yaml` files are still read for compatibility and are removed after the next successful split-state write. Do not edit state files by hand. Hashes are intentionally kept out of `config.yaml` so the config remains the user-owned source of truth.

## Commands

Run `cfgraft` without a subcommand to show top-level help, including available subcommands and global environment flags.

```text
cfgraft
```

Each subcommand also supports `--help`:

```text
cfgraft sync --help
cfgraft diff --help
cfgraft version --help
```

Use `sync` and `diff` for scripting and non-interactive workflows:

```text
cfgraft sync --dry-run
cfgraft diff --verbose
```

## Configuration

Example:

```yaml
sources:
  home:
    repo: git@github.com:example/home.git
    ref:
      type: branch
      name: main
    mappings:
      - source: zsh/zshrc
        target: ~/.zshrc
      - source: nvim
        target: ~/.config/nvim
```

Each source has a repository URL, a ref, and one or more mappings. Ref types are `branch`, `tag`, or `commit`.

Edit `config.yaml` to add and change sources and mappings. Targets must be absolute paths or start with `~/` (or be `~` alone; quote it as `"~"` in YAML). `~` expands to the current user’s home directory by default. To override it, create `~/.config/cfgraft/home` containing the absolute home directory path, for example `/Users/jared` or `/home/jared`; a trailing newline is allowed. The override affects mapping targets only, not where cfgraft finds its own configuration, repositories, or state. Other relative paths, `~user`, `$HOME`, and other environment variables are not expanded. Destination mapping roots may overlap; for example, `/Users/jared/.config` and `/Users/jared/.config/nvim` can be configured together. Sync succeeds when the mappings produce distinct destination files. If two source files resolve to the same destination—or one resolves to a path that another needs as a directory—sync stops with an error before writing any files.

Source paths are always relative to the repository root and may not escape it.

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

Repository caches under `~/.config/cfgraft/repos/` are disposable application-managed data. Do not use them as workspaces.

After refresh, `sync` plans destination updates. A destination is safe to overwrite when it does not exist, already matches the repository content, or still matches the last hash recorded in state. If an existing destination has no state entry, or if it has drifted from the last accepted hash, it is a conflict.

Successful writes update the source-specific state file with the content hash that `cfgraft` placed or explicitly accepted.

If a source or mapping has been removed from `config.yaml`, `sync` prunes the no-longer-referenced state entries. This state cleanup does not delete destination files; remove unwanted destination files manually.

## Sync Flags

`--dry-run` performs the full planning path, including repository refresh, but does not write destination files, delete files, or update state.

`--force` allows repository content to overwrite conflicts.

`--interactive` prompts for each conflict before overwriting. If any conflict is declined, sync stops without writing.

`--verbose` shows repository refresh details and no-op decisions.

## Color Output

`cfgraft` uses color and styling for human-readable CLI output. Set `NO_COLOR` to any value to disable colored/styled output:

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

With `--verbose`, `diff` also reports safety information for conflicts, such as local drift from the last accepted state.

## Binary Files

Binary files and text files are handled with the same safety model. Hashes are based on file contents, not modification times or other metadata.

## Symlinks

Symlink synchronization is not supported in the initial release. If a symlink is encountered in a repository source path, `cfgraft` warns and skips it.

## Manual Config Edits

Users may edit `config.yaml` directly. Manual config edits do not cause silent destination deletion.

If state entries remain for mappings that are no longer referenced by active config, `sync` reports them as stale managed entries and prunes them from state. Stale destination files are not silently deleted.
