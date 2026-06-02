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

Machine-managed sync state is stored separately from config:

```text
~/.config/cfgraft/state.yaml
```

Do not edit `state.yaml` by hand. Hashes are intentionally kept out of `config.yaml` so the config remains the user-owned source of truth.

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

After refresh, `sync` plans destination updates. A destination is safe to overwrite when it does not exist, already matches the repository content, or still matches the last hash recorded in `state.yaml`. If an existing destination has no state entry, or if it has drifted from the last accepted hash, it is a conflict.

Successful writes update `state.yaml` with the content hash that `cfgraft` placed or explicitly accepted.

## Sync Flags

`--dry-run` performs the full planning path, including repository refresh, but does not write destination files, delete files, or update state.

`--force` allows repository content to overwrite conflicts.

`--interactive` prompts for each conflict before overwriting. If any conflict is declined, sync stops without writing.

`--verbose` shows repository refresh details and no-op decisions.

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

If state entries remain for mappings that are no longer referenced by active config, `sync` reports them as stale managed entries and keeps them in state. Stale entries are not silently forgotten and their destination files are not silently deleted.

## TUI

Running `cfgraft` without a subcommand launches a Bubble Tea terminal UI for managing `config.yaml` and running targeted sync operations.

The TUI supports:

1. Viewing configured sources.
2. Adding a source with an ID, Git URL, ref type, and ref name.
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

Use arrow keys or tab to move through menus, enter to select menu items, and mouse clicks to move the cursor in lists. Forms are saved with `Ctrl+S` and canceled with `Esc`.

When a source is added or edited, the TUI writes the validated config and immediately checks out the configured repository cache. Mapping changes are written to config after validation; sync is still triggered manually from the selected source menu.

Removing a source or mapping from the TUI removes only the configuration entry. Local destination files are left in place.

## Future Push-Back Workflow

`cfgraft` treats repositories as read-only sources for normal sync. A future explicit workflow may detect local drift, show diffs, copy selected local changes back into a source repository, commit, and push them. That workflow is intentionally not part of default sync behavior.
