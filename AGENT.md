# MiniDB Studio Agent Notes

## Current Phase
- MiniDB Studio v10.0 in progress

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

## V3.3 Scope
- Stronger in-memory indexing for collection-aware browsing
- Sorted per-collection key slices for predictable ordering
- Prefix-accelerated paging so explorer scans do not walk the whole live map
- Recovery, compaction, and snapshot rebuild paths that keep the derived indexes correct
- Tests and docs for sorted/paged browsing behavior
- Explicitly not yet in this slice:
  - secondary indexes on JSON fields
  - background auto-maintenance workers
  - remote/cloud features

## V3.4 Scope
- Top-level JSON field indexing for document-style records
- Simple equality queries without adding SQL
- `FINDIN collection field=value` console support
- Explorer field filters powered by the same engine query path
- Recovery, snapshot, compaction, and overwrite/delete behavior that keeps JSON indexes correct
- Tests and docs for JSON field query behavior
- Explicitly not yet in this slice:
  - SQL parsing
  - nested-path querying
  - range queries
  - background indexing workers
  - remote/cloud features

## V3.5 Scope
- Nested JSON path indexing using dot-path field names
- Multi-condition JSON equality queries with sorted-key intersection
- `FINDIN collection path=value path=value` console support
- Explorer-side JSON query expressions using the same engine parser
- Recovery, snapshot, and compaction behavior that preserves nested query indexes
- Tests for nested-field lookup, multi-condition matching, and command parsing
- Explicitly not yet in this slice:
  - SQL parsing
  - range queries
  - array membership indexing
  - background indexing workers
  - remote/cloud features

## V3.6 Scope
- Richer JSON query operators without adding SQL
- String contains queries using `~=`
- Numeric comparisons using `>`, `>=`, `<`, `<=`
- Array membership matching through the existing equality syntax
- Explorer query expressions and console commands that share the same operator parser
- Tests for contains, numeric, and array-membership document queries
- Explicitly not yet in this slice:
  - SQL parsing
  - regex queries
  - OR groups or parentheses
  - background indexing workers
  - remote/cloud features

## V3.7 Scope
- Boolean OR support across JSON query groups
- Existing space-separated conditions remain AND within one group
- `FINDIN collection cond cond OR cond cond` console support
- Explorer JSON query expressions support the same OR syntax
- Sorted-key union across OR groups while reusing existing indexed filtering
- Tests for OR behavior, mixed operators with OR, nested paths with OR, and invalid syntax
- Explicitly not yet in this slice:
  - SQL parsing
  - parentheses / grouped precedence
  - NOT queries
  - background indexing workers
- remote/cloud features

## V3.8 Scope
- Grouped JSON query precedence with parentheses
- Existing implicit AND semantics remain inside a sequence of conditions
- `FINDIN collection active=true OR (profile.score>=90 tags=admin)` console support
- Nested grouped OR/AND expressions compile into the same engine-side OR-of-ANDs evaluator
- Explorer JSON query expressions support the same grouped syntax
- Tests for grouped precedence, nested grouped OR, and invalid parentheses
- Explicitly not yet in this slice:
  - SQL parsing
  - NOT queries
  - quoted values with spaces in JSON query predicates
  - background indexing workers
  - remote/cloud features

## V3.9 Scope
- JSON query negation with `NOT`
- Quoted JSON query values so spaces can be matched safely
- Grouped `NOT` support compiled into the same OR-of-ANDs evaluator
- `FINDIN collection (tags=admin OR tags=reviewer) NOT archived=true` console support
- `FINDIN collection profile.name="Ada Lovelace"` console support
- Explorer JSON query expressions support `NOT` and quoted values through the same parser
- Tests for negation, grouped negation, quoted values, and invalid quote syntax
- Explicitly not yet in this slice:
  - SQL parsing
  - regex queries
  - wildcard path matching
  - background indexing workers
  - remote/cloud features

## V4.0 Scope
- NDJSON import for one target collection
- Stable key extraction from a default `id` field or a caller-supplied key field
- Conflict handling with `skip` and `overwrite`
- Console support through `IMPORTNDJSON`
- Maintenance-page import workflow with file picker and options
- Tests for success paths, custom key field, conflict handling, invalid JSON, and missing key field
- Explicitly not yet in this slice:
  - streaming progress UI
  - nested key-field path extraction
  - remote/cloud import sources
  - background import workers

## V4.1 Scope
- NDJSON import preview before durable writes
- Dry-run import validation using the same engine path
- Preview statistics for invalid lines, missing keys, duplicate source keys, and existing-key conflicts
- Console support through `PREVIEWNDJSON` and `IMPORTNDJSON ... dry-run`
- Maintenance-page preview flow before confirming import
- Tests for preview accounting, duplicate detection, conflict detection, and dry-run no-op behavior
- Explicitly not yet in this slice:
  - field-level preview diff rendering
  - streaming progress bars
  - remote/cloud import sources
  - background import workers

## V4.2 Scope
- Schema-aware NDJSON preview field summaries
- Flattened field-path discovery with observed type hints
- Preview-side field counts so users can inspect unknown datasets before import

## V4.3 Scope
- Richer dataset review workflows in the maintenance UI
- Preview dialogs that show both record counts and discovered field hints
- Import confirmation flow that reuses the preview report directly

## V4.4 Scope
- Filtered JSON export by collection query expression
- Console support through `EXPORTQUERY`
- Maintenance export workflow with optional query text

## V5.0 Scope
- Dataset review workflow that combines preview, dry-run import, real import, and filtered export
- Console and desktop parity for the main dataset operations
- Tests for schema-aware preview, dry-run safety, and filtered export behavior
- Explicitly not yet in this slice:
  - schema evolution tracking across files
  - import diff visualization
  - background import/export jobs
  - remote/cloud dataset sources

## V5.1 Scope
- Import review with `new`, `overwrite`, and `skip` classification counts
- Preview-side change samples with compact current/incoming value previews
- Dry-run/import consistency checks against the same analyzed review report
- Maintenance review dialog surfaces overwrite candidates before confirmation
- Tests for classification counts, overwrite samples, and dry-run consistency

## V5.2 Scope
- Field-level JSON change hints for overwrite candidates
- Added, removed, and changed path summaries in preview samples
- Nested path detection for object-field changes
- Maintenance review dialog surfaces compact field-level diff hints before confirmation
- Tests for added, removed, changed, and nested path detection

## V5.4 Scope
- Saved dataset workflow presets stored locally in app data
- Presets capture collection, key field, conflict mode, and optional query text
- Maintenance preview/import/export dialogs support preset load, save, and delete
- Tests for preset save/load, overwrite-by-name, delete, and empty-name rejection

## V5.5 Scope
- Preset-aware console commands for preview, import, dry-run import, and filtered export
- App-layer preset resolution so the engine stays focused on database/data operations
- Tests for preset-backed preview/import/export commands and missing-preset errors

## V5.6 Scope
- Preset inspection commands through `LISTPRESETS` and `SHOWPRESET`
- App-layer formatting for saved workflow summaries without expanding engine scope
- Console/help documentation updates so preset discovery is self-serve
- Tests for preset listing, preset detail output, and missing-preset errors

## V5.7 Scope
- Preset management commands through `SAVEPRESET` and `DELETEPRESET`
- Compact console syntax for creating or overwriting saved workflows
- App-layer validation for preset name, collection, key field, and conflict mode
- Tests for preset save, overwrite, delete, and validation failures

## V5.8 Scope
- Preset rename command through `RENAMEDPRESET`
- Durable rename behavior with conflict checks so one workflow cannot silently replace another
- Console/help documentation updates for the completed preset lifecycle
- Tests for rename success, missing-source failure, and rename-conflict failure

## V5.9 Scope
- Preset duplication command through `DUPLICATEPRESET`
- Portable preset JSON export/import through `EXPORTPRESETCONFIG` and `IMPORTPRESETCONFIG`
- Store-level validation and file helpers so workflow configs can move between local setups
- Tests for duplicate success/conflicts plus preset config export/import flows

## V6.0 Scope
- Cleaner and more modern native desktop shell
- Better use of cards, grouped sections, and resizable split layouts
- Explorer, Console, and Maintenance layouts tuned for larger and smaller window sizes
- Keep every existing action working while improving clarity and desktop usability

## V6.1 Scope
- Custom Fyne theme for a cleaner desktop visual identity
- Improved spacing, button colors, focus colors, and typography sizing
- Keep the app native while making it feel less default and more production-ready

## V6.2 Scope
- Better dialog and workflow form presentation
- Larger, scroll-safe record editor and maintenance forms
- Consistent card-based structure for export, preview, and import flows

## V6.3 Scope
- Stronger About/overview experience
- Better product framing, workflow guidance, and command-family documentation inside the app
- Finish the UI pass with more intentional desktop information architecture

## V7.0 Scope
- Consolidate the modernized shell, theme, dialogs, and overview experience
- Keep layouts resizable and readable across the main work pages
- Prepare the repo for commit and push after the final validation pass

## V7.1 Scope
- Add a true Overview landing page with workspace health, collection summaries, and recent activity
- Promote the app from a page set into a fuller desktop studio workflow
- Persist activity history inside the application data directory

## V7.2 Scope
- Durable activity history feed for successful and failed user-visible operations
- Shared activity surface for dashboard and preset-management workflows
- Keep the activity layer in `internal/app` instead of mixing it into engine internals

## V7.3 Scope
- Collection profile summaries with record counts, value-kind counts, sample keys, and last-update timestamps
- Dashboard-ready collection insight without introducing server-style schema analyzers
- Reuse existing engine metadata instead of expanding the on-disk format

## V7.4 Scope
- Dedicated Preset Library page for save, load, rename, duplicate, import, export, and delete flows
- Keep every visible preset action working from a first-class desktop surface
- Reuse the existing preset store so the engine boundary stays clean

## V8.0 Scope
- Add a read-only mini SQL layer for `SELECT ... FROM ... WHERE ... LIMIT ...`
- Translate SQL-shaped reads into the existing collection and JSON-query paths
- Keep SQL deliberately compact and non-authoritative: no joins, writes, DDL, networking, or server semantics

## V8.1 Scope
- Extend mini SQL with `ORDER BY ... ASC|DESC`
- Keep sorting constrained to known result columns instead of arbitrary expressions
- Reuse one read-only query path for both desktop and console execution

## V8.2 Scope
- Add `SELECT COUNT(*)` for lightweight query aggregation
- Keep aggregation intentionally narrow so MiniDB does not pretend to be a full SQL engine

## V8.3 Scope
- Durable saved-query library stored beside the MiniDB workspace
- Query names, SQL text, and notes managed through `internal/app`
- Keep query persistence separate from dataset presets because the workflow is different

## V8.4 Scope
- First-class Query Studio page in the desktop shell
- Save, load, rename, delete, run, and export read-only SQL workflows without dropping into maintenance dialogs

## V9.0 Scope
- Query Studio result export to TSV for offline review and spreadsheet handoff
- Dashboard summary updated with saved-query counts so the home page reflects operational maturity
- Remove temporary startup tracing so the desktop build stays release-clean

## V10.0 Scope
- Add lightweight grouped SQL summaries through `GROUP BY ... COUNT(*)`
- Keep aggregation intentionally small and local-first instead of expanding into a full analytical SQL engine
- Add collection schema inspection so operators can understand observed JSON field paths and sample values before querying or importing
- Surface schema inspection in Query Studio so analytical exploration lives in one desktop workflow

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
- startup hardening updates:
  - legacy `MDB1` local-data migration into current `MDB2` frames
  - stale lock file recovery using PID liveness checks
  - desktop startup error window instead of silent fatal exit
  - explorer initialization-order crash fix

## Next Steps
- MiniDB Studio v10.0 now has the first lightweight analysis layer on top of the local engine.
- After that lands, future work can focus on richer aggregates, deeper schema intelligence, saved result views, wildcard/path helpers, and background maintenance workers.
- Keep the engine local-first and single-process while growing the operator experience around practical data exploration.
