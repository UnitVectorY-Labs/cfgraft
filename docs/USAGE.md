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
cfgraft tui --help
cfgraft sync --help
cfgraft diff --help
cfgraft version --help
```

Use `tui` for interactive configuration:

```text
cfgraft tui
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
        target: /Users/jared/.zshrc
      - source: nvim
        target: /Users/jared/.config/nvim
```

Each source has a repository URL, a ref, and one or more mappings. Ref types are `branch`, `tag`, or `commit`.

When sources are added through the TUI, the source ID is derived from the repository URL. For example, `git@github.com:example/dotfiles.git` becomes `dotfiles`. If that ID already exists, `cfgraft` appends a numeric suffix.

Targets must be absolute paths. `cfgraft` does not expand `~`, `$HOME`, or other environment variables in target paths. Destination mappings must not overlap; for example, mapping both `/Users/jared/.config` and `/Users/jared/.config/nvim` is rejected.

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

If a source or mapping has been removed from `config.yaml`, `sync` prunes the no-longer-referenced state entries. This state cleanup does not delete destination files; managed-file deletion only happens through an explicit configured cleanup flow such as the TUI removal confirmation.

## Sync Flags

`--dry-run` performs the full planning path, including repository refresh, but does not write destination files, delete files, or update state.

`--force` allows repository content to overwrite conflicts.

`--interactive` prompts for each conflict before overwriting. If any conflict is declined, sync stops without writing.

`--verbose` shows repository refresh details and no-op decisions.

## Color Output

`cfgraft` uses color and styling for human-readable CLI output and the TUI. Set `NO_COLOR` to any value to disable colored/styled output:

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

## TUI

Run `cfgraft tui` to launch a Bubble Tea terminal UI for managing `config.yaml` and running targeted sync operations.

The TUI uses Bubble Tea v2 with an ASCII `cfgraft` header, contextual breadcrumbs, Bubbles list/table/text-input components, bottom action buttons, hover highlighting, styled selections, and colored messages/errors. It supports:

1. Viewing configured sources.
2. Adding a source with a Git URL, ref type, and ref name.
3. Editing an existing source.
4. Removing a source from config.
5. Viewing mappings for a selected source.
6. Adding mappings.
7. Editing mappings.
8. Removing mappings from config.
9. Syncing all sources.
10. Syncing one selected source.
11. Diffing all sources.
12. Diffing one selected source.

The source list is vertical and rendered with the Bubbles list component. Page-level actions are shown as bottom buttons such as Add, Sync, and Diff. A selected source page shows source details in a Bubbles table, then the mappings in a separate Bubbles list; if a source has no mappings, the mappings area shows `No Mappings` in muted text. The selected source page exposes Edit, Remove, Add Mapping, Sync, Diff, and Back as bottom buttons. Mouse hover highlights clickable actions and list rows, and mouse clicks move the active selection.

Keyboard focus moves between the primary content and the bottom button bar with tab and shift-tab. Shortcut letters activate immediately from anywhere on the screen: A for Add, S for Sync or Save, D for Diff, E for Edit, R for Remove, and B for Back. Enter activates the selected list row or focused button. The bottom of each screen renders contextual keyboard help with the Bubbles help component; press `?` to toggle the expanded help view.

Forms use Bubbles text inputs for normal terminal text editing behavior, including paste support. On mapping add/edit screens, Enter moves into or out of the active field. While editing a mapping path, up/down selects autocomplete suggestions and tab completes the selected suggestion. Source-path suggestions are relative to the checked-out repository cache and are shown one directory level at a time; target-path suggestions come from the host filesystem after typing a starting path. Use the bottom buttons or shortcut letters to save, add, remove, or go back.

When a source is added or edited, the TUI writes the validated config and checks out the configured repository cache in a background Bubble Tea command. Sync and diff actions also run in the background. While one of these operations is running, the TUI shows a modal with a Bubbles spinner and progress bar for the active operation.

Command output is shown in a Bubbles viewport so large sync or diff results remain scrollable instead of running off the bottom of the screen. Pager keys and mouse wheel scrolling work in the output view.

Source IDs are derived from the Git URL rather than entered manually. Mapping changes are written to config after validation; sync is still triggered manually from the selected source menu.

Mapping source and target fields use text input suggestions. Source path suggestions come from the checked-out repository cache as repository-relative paths, one directory level at a time, and target path suggestions come from the local filesystem near the path being typed. Mapping forms show a check table above the path fields to indicate whether the source path exists in the source cache. If a mapping target would require new parent folders, the TUI asks for confirmation before saving the mapping.

Removing a source or mapping from the TUI removes only the configuration entry. Local destination files are left in place.

## Future Push-Back Workflow

`cfgraft` treats repositories as read-only sources for normal sync. A future explicit workflow may detect local drift, show diffs, copy selected local changes back into a source repository, commit, and push them. That workflow is intentionally not part of default sync behavior.
