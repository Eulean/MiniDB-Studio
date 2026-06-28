# MiniDB Studio Agent Notes

## Current Phase
- MiniDB Studio v2 in progress

## Goals In Progress
- Preserve the existing v1 desktop app and upgrade the storage engine in place. Completed.
- Add single-process locking, snapshots, segmented logs, offset-based indexing, validation, and batch writes. Completed.
- Expand tests first for engine correctness, then extend the Fyne UI and docs. Completed.

## Architecture Direction
- UI framework remains Fyne v2.
- Application orchestration remains in `internal/app`.
- The engine will move from a single log file to:
  - segmented append-only log files
  - one active writable segment
  - a crash-safe snapshot file
  - an offset-based in-memory index
- Values will no longer live in memory by default; the index will store file locations plus metadata.
- Storage location remains the application data directory.

## Current Decisions
- Keep the framed binary record format because it already gives:
  - clear boundaries
  - newline-safe values
  - incomplete-tail detection
  - checksum verification
- Extend records with:
  - sequence number
  - created/updated timestamps
  - value size
  - batch operation support
- Add one metadata file to track durable counters and timestamps.
- Use one snapshot file plus numbered segment files.
- Use a lock file to prevent multiple active opens for the same database directory.
- Preserve the `desktop` build tag and Zig-based CGO build flow.

## Proposed File Changes
- Keep:
  - `internal/app`
  - `internal/ui`
  - `internal/storage`
- Refactor and extend `internal/engine` with:
  - `types.go`
  - `segments.go`
  - `snapshot.go`
  - `lock.go`
  - `validate.go`
  - updated `db.go`, `commands.go`, `compact.go`, `recovery.go`, `stats.go`, `log.go`
- Expand `tests/engine_test.go` rather than splitting immediately unless complexity forces a follow-up split.

## V2 Implementation Order
1. Rebuild the engine around segments, snapshots, and offset-based entries.
2. Add file locking, validation, and atomic batch writes.
3. Expand tests for recovery, segmentation, lazy reads, and locking.
4. Extend the Fyne UI for metadata, snapshot, and validation actions.
5. Update README and re-run all validation plus desktop build verification.

## Validation
- `go test ./...` passed.
- `go vet ./...` passed.
- `.\scripts\build-desktop.ps1` completed successfully after the v2 engine/UI refactor.
- The transient Windows temporary test-executable cleanup issue may still appear occasionally on the first run, but reruns pass cleanly and the tests themselves are green.

## Deliverables Completed
- v1 project scaffolding, engine, desktop UI, tests, scripts, and documentation remain available as the baseline.
- v2 engine refactor:
  - lock file handling
  - segmented logs
  - snapshot creation/loading
  - offset-based index entries
  - validation reporting
  - atomic batch operations
  - expanded stats
- v2 UI updates:
  - explorer metadata and richer record listing
  - maintenance buttons for snapshot and validation
  - backup archives for the full durable store
- v2 tests:
  - locking
  - snapshots
  - segmented recovery
  - validation behavior
  - batch atomicity
  - compaction
  - corruption handling
  - concurrent access

## Next Steps
- MiniDB Studio v2 is now in a releasable state.
- Future work belongs to post-v2 enhancements rather than core delivery fixes.
