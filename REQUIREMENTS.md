# cfgraft Remaining Requirements

The implemented and documented behavior has been moved into `docs/USAGE.md`. This file intentionally contains only functionality that is still incomplete or not fully documented.

## Unexpected-file policy

Consider a mapping option to flag or allow unrelated files in managed destination folders.

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
