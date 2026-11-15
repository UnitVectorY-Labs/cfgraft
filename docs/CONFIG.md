---
layout: default
title: Configuration
nav_order: 2
permalink: /config
---

# Configuration

Edit `~/.config/cfgraft/config.yaml` with a text editor to add, change, or remove sources and mappings. This location is under the current user's home directory, even if a mapping home override is configured. If the file does not exist, cfgraft creates an empty configuration when a command loads it. Create the directory and file yourself if you want to configure it before the first run.

The file is YAML. Indent with spaces and quote values when necessary. For example:

```yaml
sources:
  dotfiles:
    repo: https://example.com/dotfiles.git
    ref:
      type: branch
      name: main
    mappings:
      - source: shell/zshrc
        target: ~/.zshrc
      - source: nvim
        target: ~/.config/nvim
      - source: cfgraft/config.yaml
        target: ~/.config/cfgraft/config.yaml
```

Replace the repository URL, ref, and paths with your own. This example maps two configuration paths and lets cfgraft manage its own configuration from the repository. You must create an initial local configuration before cfgraft can sync it.

## Fields

- `sources` is a mapping from a nonempty source ID, such as `dotfiles`, to its settings. An empty configuration can use `sources: {}`.
- `repo` is the Git repository URL used to clone or refresh the source.
- `ref.type` is `branch`, `tag`, or `commit`; `ref.name` is the corresponding branch name, tag name, or commit identifier.
- `mappings` is a list of files or directories to synchronize. Each entry has a `source` and a `target`.
- `source` is a nonempty path relative to the repository root. It cannot escape that root. Use `.` to map the repository root.
- `target` is the destination file or directory. It must be an absolute path, `~/` followed by a path, or `"~"` for the home directory itself. Quote `"~"` because an unquoted tilde is YAML null.
- `local_id` is optional and selects the repository cache path relative to cfgraft's repos directory. It must not escape that directory. If omitted, cfgraft derives a cache name from the source ID and repository URL.
- `condition` is optional on a source or mapping. When present, the entry is enabled only if the machine-local marker file for that name exists. See below.

A file source maps to a file target; a directory source maps its contents into a target directory. Mapping roots may overlap, but sync stops before writing if source files resolve to the same destination or if a destination file conflicts with a directory needed by another mapping.

## Machine-local conditions

Use a `condition` to share the same config across machines while selecting sources or mappings locally:

```yaml
sources:
  dotfiles:
    repo: https://example.com/dotfiles.git
    ref: {type: branch, name: main}
    mappings:
      - {source: mac/app.conf, target: ~/.config/app.conf, condition: mac}
      - {source: linux/app.conf, target: ~/.config/app.conf, condition: linux}
```

On a Mac, run `touch ~/.config/cfgraft/condition-mac`; on Linux, create `~/.config/cfgraft/condition-linux` instead. Create the directory first if needed. Remove a marker to disable its entries. The marker must be a regular file; its contents are ignored. Condition names must begin with an ASCII letter or digit and contain only ASCII letters, digits, underscores, and hyphens. Names are case-sensitive. Markers live beside `config.yaml` under the current user's home, independent of the mapping home override.

A source without a condition is enabled; a mapping without one is enabled when its source is enabled. A source condition disables the entire source, including repository refresh. If both source and mapping have conditions, both must be enabled. `cfgraft validate` checks all entries and shows condition status; `sync` and `diff` operate only on enabled entries. Conditions are evaluated when each command starts, so a marker changed by a sync takes effect on the next command.

Disabling an entry has the same state behavior as removing it: a subsequent sync prunes its stale state without deleting destination files. Re-enabling it can therefore produce a conflict if an existing destination differs from the repository content.

## Home directory in targets

`~` resolves to the current user's home directory by default. To use a different home for mapping targets, create `~/.config/cfgraft/home` containing a single absolute directory path, for example:

```text
/Users/jared
```

A trailing newline is allowed. This file is machine-specific, so it can be left outside the shared repository while the same `config.yaml` uses `~/` on Linux and macOS. The override affects mapping targets only; it does not change where cfgraft stores its configuration, caches, or state. Other relative paths, `~user`, and environment variables such as `$HOME` are not expanded.

## Applying edits

Save the YAML and run `cfgraft validate` to check its structure and paths and list each source and mapping, then run `cfgraft sync --dry-run` to review the planned changes, then run `cfgraft sync` to apply them. A dry run still refreshes the repository caches. Existing destination files are protected by cfgraft's conflict checks; see [Usage](USAGE.md) for sync flags and safety behavior.

Removing a source or mapping from the configuration does not delete its destination files. A subsequent sync reports and prunes the no-longer-referenced state; remove unwanted destination files manually.
