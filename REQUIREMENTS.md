# cfgraft Remaining Requirements

The implemented and documented behavior has been moved into `docs/USAGE.md`. This file intentionally contains only functionality that is still incomplete or not fully documented.

## TUI Library Upgrade Plan

Research notes:

1. Bubble Tea, Bubbles, and Lip Gloss v2 are already in use. The current TUI still hand-rolls most navigation, list rendering, scrolling, progress, and mouse hit testing.
2. Bubbles v2 provides the appropriate primitives for this application: `textinput` supports paste and suggestions, `spinner` supports asynchronous progress indicators, `viewport` supports scrollable output, `list` supports selectable/filterable/paged lists, and `filepicker` can be used where direct path browsing is preferable to free-form entry.
3. BubbleZone can simplify mouse regions for custom controls. Its v2 README notes caveats with the newer Bubble Tea/Lip Gloss compositor, so adopt it after the core Bubbles components are in place and only for custom hit areas that Bubbles components do not already handle.

Implementation order:

1. Migrate selectable source and mapping views to Bubbles list components.
   - Replace custom paging/highlight bookkeeping with Bubbles list selection state.
   - Keep mouse selection working through component-native behavior where possible, then add BubbleZone only for remaining custom controls.
2. Streamline navigation and remove obsolete screens.
   - Fully remove dead standalone mappings-screen code after all flows use the selected source screen.
   - Consolidate source detail and mapping management so actions are discoverable from a single source screen.
   - Keep `Tab`/`Shift+Tab`, arrows, mouse, and shortcuts consistent across screens.
3. Add mapping state bookkeeping.
   - Mark newly added or edited mappings dirty until their sync plan has been applied.
   - Show whether sync will add, update, delete, or remap files for each dirty mapping.
   - Persist enough state to identify old managed files that need removal after mapping changes.
4. Add unexpected-file policy for managed folders.
   - Folder mappings should default to disallowing unrelated files mixed into managed targets.
   - Add a mapping option to allow mixed files when desired.
   - Flag unexpected files in managed folders during planning and TUI review.

## TUI Planned Change Review

The TUI can manage sources and mappings and can trigger syncs one source at a time. It still needs a richer review screen that shows planned changes before a selected sync is applied and lets the user confirm or cancel from inside the TUI.

## TUI Adding New Mappings With Existing Destinations

When adding a new mapping through the TUI:

If the destination does not exist, the application may create it automatically without confirmation.

If the destination already exists, the application must show the diff between the existing destination and the source content.

The user must explicitly confirm before the destination is overwritten and adopted.

There is no “keep local and mark as accepted” option for a new mapping. If the user does not accept the repository version as the managed content, the mapping must not be added to the config.

## TUI Removing Mappings With Managed Deletion

When removing a mapping through the TUI, the application must prompt the user about what to do with the destination content.

The TUI should distinguish between:

1. Removing the mapping but leaving files in place.
2. Removing the mapping and deleting files known to be managed by the application.

Deletion must follow the same safety principles as sync: only files known to be managed and unchanged from the last accepted hash may be deleted without further conflict handling.

## Stale State Resolution UX

`sync` reports stale managed entries, keeps them in state, and does not silently delete local files. A fuller stale-state resolution workflow is still needed.

The application should require explicit confirmation or interactive handling before forgetting stale state entries or deleting associated files.

## Future Push-Back Workflow

The application should eventually support an explicit workflow for identifying local drift and sending selected changes back to a configured repository.

This is not the default sync behavior. Repositories are read-only by default from the perspective of managed local targets.

Any push-back workflow must be opt-in.

The future workflow should support:

1. Detecting local files that drifted from last accepted state.
2. Showing diffs.
3. Copying selected local changes back into the source repo.
4. Committing those changes.
5. Pushing them to the remote repository.

This capability must be disabled unless explicitly configured or invoked.
