# MiniDB Studio Agent Notes

## Current Phase
- MiniDB Studio v3.2 in progress

## Goals In Progress
- Keep the v3.1 engine stable while adding repair/export tooling and maintenance recommendations. Completed.
- Preserve the current local embedded architecture while improving operational maturity. Completed for v3.2 scope.
- Extend the maintenance UI and console around the new repair/export engine APIs. Completed.

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
- Introduce collections at the engine layer instead of faking them only in the UI.
- Keep the existing framed storage format and enrich payloads with collection and value-kind metadata.
- Add JSON validation as an optional record kind instead of replacing plain text storage.

## V3.1 Scope
- First-class collections
- Collection-aware commands and explorer browsing
- Paged record listing
- JSON-aware record creation/editing with validation
- Tests and docs for the above
- Explicitly not yet in this slice:
  - background maintenance scheduler
  - repair/export workflows
  - secondary indexes beyond collection-aware listing

## V3.2 Scope
- Safe export for one collection or the entire database
- Repair/salvage into a fresh database directory
- Maintenance recommendation heuristics
- Maintenance UI and console support for the above
- Explicitly not yet in this slice:
  - automatic background workers
  - field-level document indexing
  - remote/cloud features

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
- `.\scripts\build-desktop.ps1` still completes successfully after the v3.1 collections and JSON-aware UI changes.
- `.\scripts\build-desktop.ps1` also completes successfully after the v3.2 repair/export and maintenance UI changes.
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
- v3.1 engine updates:
  - first-class collections
  - collection-aware commands
  - paged listing support
  - JSON-aware value-kind metadata and validation
- v3.1 UI updates:
  - collection selector in explorer
  - paged browsing controls
  - collection/value-kind aware record editing
- v3.1 tests:
  - collection separation
  - collection paging
  - JSON validation and metadata
  - collection-aware batch commands
- v3.2 engine updates:
  - NDJSON export
  - repair/salvage into a fresh destination DB
  - maintenance recommendation heuristics
- v3.2 UI updates:
  - maintenance health display
  - export action
  - repair action
- v3.2 tests:
  - export
  - repair salvage
  - maintenance recommendations

## Next Steps
- MiniDB Studio v3.2 is now in a usable checkpoint state.
- Future work can focus on background maintenance workers, richer repair/export flows, and stronger indexing.
