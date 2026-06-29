# MiniDB Studio

MiniDB Studio is a Windows-first native desktop application built with Go and Fyne around a custom embedded key-value database engine written from scratch with the Go standard library.

This repository now targets MiniDB v7.0:
- single-process file locking
- segmented append-only storage
- offset-based in-memory index
- crash-safe snapshots
- replay recovery
- compaction
- validation and diagnostics
- atomic batch writes
- first-class collections
- paged record browsing
- JSON-aware records
- export tooling
- repair/salvage tooling
- maintenance recommendations
- sorted per-collection key indexing
- prefix-accelerated paging
- nested JSON path indexing
- multi-condition JSON document queries
- string contains and numeric comparison operators
- array membership queries for scalar arrays
- boolean OR query composition with grouped parentheses
- logical NOT support for JSON queries
- quoted JSON query values with spaces
- NDJSON document import workflows
- NDJSON import preview and dry-run support
- schema-aware dataset preview field summaries
- filtered JSON export by query expression
- change-aware import review with overwrite samples
- field-level overwrite diff hints for import review
- saved dataset presets for repeated preview/import/export workflows
- preset-aware console commands for dataset workflows
- console preset inspection commands for listing and viewing saved workflows
- console preset management commands for saving and deleting workflows
- console preset rename commands for completing workflow lifecycle management
- portable preset commands for duplication and JSON config import/export
- cleaner, more modern, and more resizable native desktop layouts
- custom desktop theme and polished workflow dialogs
- stronger in-app overview and operator guidance
- native desktop management UI

It is intentionally not a SQL server, network service, or distributed database.

## Project Purpose

MiniDB Studio has two goals:

1. Teach how a small durable database can be built from first principles.
2. Provide a practical Windows-first native desktop tool for exploring and operating that database locally.

## Architecture Diagram

```text
MiniDB Studio Desktop App
|
+-- cmd/minidb
|   +-- tagged desktop entrypoint
|   +-- non-desktop validation stub
|
+-- internal/ui
|   +-- MainWindow
|   +-- Data Explorer page
|   +-- Command Console page
|   +-- Maintenance page
|   +-- dialogs
|
+-- internal/app
|   +-- UI orchestration
|   +-- shared app state / status
|
+-- internal/engine
|   +-- DB lifecycle
|   +-- file locking
|   +-- framed record format
|   +-- segmented logs
|   +-- snapshot creation / loading
|   +-- startup recovery
|   +-- validation
|   +-- compaction
|   +-- stats
|   +-- command execution
|   +-- collections / document-aware records
|   +-- sorted collection indexes
|
+-- internal/storage
|   +-- app-data path resolution
|
+-- tests
    +-- engine integration tests
```

## Final File Tree

```text
minidb-studio/
  .gitignore
  AGENT.md
  README.md
  go.mod
  go.sum
  cmd/
    minidb/
      main_desktop.go
      main_stub.go
  internal/
    app/
      application.go
      state.go
    engine/
      commands.go
      compact.go
      db.go
      export.go
      import.go
      indexes.go
      json_index.go
      lock.go
      log.go
      maintenance.go
      metadata_codec.go
      recovery.go
      repair.go
      segments.go
      snapshot.go
      stats.go
      types.go
      validate.go
    storage/
      paths.go
    ui/
      console_page.go
      dialogs.go
      explorer_page.go
      main_window.go
      maintenance_page.go
  scripts/
    build-desktop.ps1
    setup-local-zig.ps1
  tests/
    engine_test.go
```

## Setup

### Prerequisites

- Go 1.23 or newer
- Windows is the primary target
- For the real desktop build, CGO plus a working C/C++ compiler is required

### Clone And Prepare

```powershell
git clone <your-repo-url>
cd BuildYourOwnDatabase
go mod tidy
```

## How Persistence Works

MiniDB stores data in the local application-data folder instead of beside the executable.

On Windows the default location is:

```text
%LOCALAPPDATA%\MiniDB Studio
```

### Durable Files

- `minidb.meta.json`
  - durable counters
  - next sequence
  - active segment id
  - snapshot / compaction timestamps
- `minidb.lock`
  - prevents two MiniDB processes from opening the same DB directory at once
- `segment-000001.log`, `segment-000002.log`, ...
  - append-only mutation segments
- `snapshot.dat`
  - crash-safe materialized snapshot of current live state

### Record Format

Each durable frame uses:

1. 4-byte magic header
2. 4-byte payload length
3. JSON payload
4. 4-byte CRC32 checksum

This allows:
- values with spaces and newlines
- corruption detection
- safe ignoring of only the incomplete final record

### Write Path

For `SET`, `DELETE`, and `BATCH`:

1. Build one durable mutation batch payload.
2. Append it to the active segment.
3. Sync the segment.
4. Update the in-memory index only after the durable write succeeds.
5. Persist metadata with an atomic temp-file swap.

### Read Path

- The in-memory index stores metadata and file offsets, not full values by default.
- `GET` loads the value lazily from the referenced snapshot or segment frame.
- Explorer previews also load values lazily.
- Collection browsing uses sorted in-memory key slices so prefix filters and paging avoid full map scans.
- JSON documents also maintain scalar field indexes, including nested dot paths such as `profile.email`.

### Recovery Path

On startup:

1. Acquire the DB lock.
2. Load metadata.
3. Load the latest valid snapshot if present.
4. Replay only segment operations newer than the snapshot sequence.
5. Rebuild the offset-based index and the sorted per-collection key slices.

If the final frame is incomplete because of an interrupted write, MiniDB ignores only that final incomplete frame.

If corruption appears earlier in storage, startup returns a clear recovery error.

### Snapshot Path

`SNAPSHOT`:

1. Writes a temporary snapshot file.
2. Syncs it.
3. Atomically replaces the old snapshot.
4. Stores the latest snapshot sequence and timestamp in metadata.

### Compaction Path

`COMPACT`:

1. Reads current live entries.
2. Rewrites them into a fresh compacted segment.
3. Removes obsolete segments.
4. Clears stale snapshot replay assumptions.
5. Records the last compaction time.

## Supported Commands

- `SET key value`
- `GET key`
- `DELETE key`
- `KEYS`
- `KEYS prefix`
- `COLLECTIONS`
- `SETIN collection key value`
- `GETIN collection key`
- `DELETEIN collection key`
- `KEYSIN collection [prefix]`
- `SETJSON collection key json-value`
- `FINDIN collection path=value [path=value ...]`
- `FINDIN collection path>=value [path~=value ...]`
- `FINDIN collection cond cond OR cond cond`
- `FINDIN collection active=true OR (profile.score>=90 tags=admin)`
- `FINDIN collection (tags=admin OR tags=reviewer) NOT archived=true`
- `FINDIN collection profile.name="Ada Lovelace"`
- `IMPORTNDJSON collection "C:\path\file.ndjson"`
- `PREVIEWNDJSON collection "C:\path\file.ndjson"`
- `IMPORTNDJSON collection "C:\path\file.ndjson" id overwrite dry-run`
- `IMPORTNDJSON collection "C:\path\file.ndjson" id overwrite`
- `EXPORTQUERY collection "active=true" "C:\path\filtered.jsonl"`
- `PREVIEWPRESET "Docs Workflow" "C:\path\file.ndjson"`
- `SAVEPRESET "Docs Workflow" docs id overwrite "active=true"`
- `LISTPRESETS`
- `SHOWPRESET "Docs Workflow"`
- `DUPLICATEPRESET "Docs Workflow" "Docs Copy"`
- `RENAMEDPRESET "Docs Workflow" "Docs Archive"`
- `EXPORTPRESETCONFIG "Docs Archive" "C:\path\docs-archive-preset.json"`
- `IMPORTPRESETCONFIG "C:\path\docs-archive-preset.json"`
- `IMPORTPRESET "Docs Workflow" "C:\path\file.ndjson" dry-run`
- `EXPORTPRESET "Docs Workflow" "C:\path\filtered.jsonl"`
- `DELETEPRESET "Docs Workflow"`
- `STATS`
- `COMPACT`
- `SNAPSHOT`
- `VALIDATE`
- `EXPORT collection`
- `EXPORT ALL`
- `REPAIR`
- `BATCH ... END`

### Batch Example

```text
BATCH
SET user:1 Alice
SET user:2 Bob Smith
DELETE user:3
END
```

## Desktop Features

### Data Explorer

- browse by collection
- filter by key prefix
- browse paged keys with value preview, size, kind, and updated time
- filter JSON collections with nested-path, multi-condition, OR-composed, and operator-based query expressions
- view full value details
- view per-record metadata
- create/edit records with collection and raw/json kind
- create records
- edit records
- delete records with confirmation

### Command Console

- multi-line input
- one command at a time unless explicit `BATCH`
- monospace output/history
- readable errors

### Maintenance

- show database health recommendations
- show live/storage statistics
- create snapshot
- validate database files
- repair into a fresh destination
- compact database
- export one collection or the full database
- import NDJSON documents into a chosen collection
- preview NDJSON imports before writing
- export only the JSON documents matching a query
- create backup archive
- open data folder

### Status Bar

- database location
- current status
- last operation result

## Run The App

### Default Non-Desktop Entrypoint

This keeps `go test ./...` and `go vet ./...` green in environments without desktop build prerequisites:

```powershell
go run ./cmd/minidb
```

### Native Desktop App With Local Zig Toolchain

```powershell
.\scripts\setup-local-zig.ps1
$env:CGO_ENABLED="1"
$env:CC="C:\zig-local\zig.exe cc"
$env:CXX="C:\zig-local\zig.exe c++"
go run -tags desktop ./cmd/minidb
```

### Run The Built Executable

```powershell
.\scripts\build-desktop.ps1
.\dist\MiniDBStudio.exe
```

## Package A Windows Executable

### Build With The Included Helper Scripts

```powershell
.\scripts\setup-local-zig.ps1
.\scripts\build-desktop.ps1
```

### Manual Build With Go

```powershell
$env:CGO_ENABLED="1"
$env:CC="C:\zig-local\zig.exe cc"
$env:CXX="C:\zig-local\zig.exe c++"
go build -tags desktop -o .\dist\MiniDBStudio.exe ./cmd/minidb
```

### Package With Fyne CLI

```powershell
go install fyne.io/tools/cmd/fyne@latest
$env:CGO_ENABLED="1"
$env:CC="C:\zig-local\zig.exe cc"
$env:CXX="C:\zig-local\zig.exe c++"
fyne package -tags desktop -os windows -name "MiniDB Studio"
```

## Validation

The following commands pass in this workspace:

```powershell
go test ./...
go vet ./...
```

The native desktop executable was also rebuilt successfully with:

```powershell
.\scripts\build-desktop.ps1
```

## Desktop Startup Smoke Test

Use this short checklist before calling a desktop build "ready":

1. Close every existing `MiniDBStudio.exe` instance.
2. Run `.\scripts\build-desktop.ps1`.
3. Launch `.\dist\MiniDBStudio.exe`.
4. Confirm the window stays open for at least 10 seconds.
5. Confirm the Explorer loads without a crash.
6. Create one raw record and one JSON record.
7. Run `FINDIN docs email=...` from the console if a JSON record was added.
8. Open Maintenance and confirm stats render.
9. Close the app and relaunch it.
10. Confirm the records persist after reopen.

MiniDB Studio now also tries to recover common local-startup issues automatically:
- old `MDB1` local data is migrated into the current storage format
- stale `minidb.lock` files are cleaned up when the owning PID is no longer alive

## Implemented V7.0 Features

- single-process lock file protection
- snapshot create/load path
- segmented append-only logs
- offset-based live index
- lazy value reads
- batch writes
- validation command
- richer stats
- backup archive generation
- first-class collections
- collection-aware console commands
- JSON validation and value-kind metadata
- paged explorer browsing
- record metadata in the explorer
- maintenance actions for snapshot and validation
- NDJSON export
- salvage repair into a fresh destination DB
- maintenance health/recommendation heuristics
- sorted per-collection key index maintenance on writes, deletes, snapshot loads, recovery, and compaction
- faster prefix browsing through binary-search key windows
- nested JSON path indexing for string, number, boolean, and null values
- multi-condition JSON equality matching through sorted-key intersection
- `FINDIN collection path=value [path=value ...]` console queries
- explorer-side JSON query expressions powered by the same engine query path
- string contains queries through `~=`
- numeric comparisons through `>`, `>=`, `<`, and `<=`
- scalar array membership queries through the existing equality syntax
- boolean OR composition across condition groups
- logical NOT on conditions and grouped expressions
- quoted JSON query values so spaces can be matched safely
- Explorer JSON query expressions share the same grouped OR/AND/NOT parser as the console
- NDJSON import with `skip` or `overwrite` conflict handling
- Maintenance import workflow with file picker, key field, and conflict mode controls
- NDJSON preview reports with duplicate/conflict/invalid-line accounting
- dry-run import validation that leaves the database unchanged
- schema-aware field summaries with observed path/type hints during preview
- filtered JSON export through query expressions in both console and Maintenance
- import preview classification for new, overwrite, and skip outcomes
- compact current/incoming preview samples before confirming overwrite-capable imports
- field-level added, removed, and changed path hints for overwrite candidates
- nested JSON path change detection inside overwrite review samples
- saved workflow presets for collection/key-field/conflict/query combinations
- maintenance dialogs can load, save, and delete dataset presets
- preset-aware console commands that resolve saved dataset workflows by name
- `LISTPRESETS` to inspect available saved workflows from the console
- `SHOWPRESET "Name"` to inspect one preset's collection, key field, conflict mode, and query
- `SAVEPRESET "Name" collection key_field conflict_mode ["query"]` to create or overwrite workflows from the console
- `DELETEPRESET "Name"` to remove workflows from the console
- `RENAMEDPRESET "Old Name" "New Name"` to rename workflows without changing their saved fields
- `DUPLICATEPRESET "Source" "Copy"` to clone one saved workflow under a new name
- `EXPORTPRESETCONFIG "Name" "C:\path\preset.json"` to export one workflow as portable JSON
- `IMPORTPRESETCONFIG "C:\path\preset.json"` to import one workflow JSON file into local preset storage
- custom Fyne theme with cleaner colors, spacing, and sizing
- stronger card-based shell, footer status cards, and resizable page layouts
- larger scroll-safe editor and maintenance workflow dialogs
- richer About/overview page with workflow guidance and command families

## Current Limitations

- no SQL
- no networking
- no authentication
- no replication
- no distributed storage
- no cloud sync
- no server-mode multi-user support
- no transactions beyond single durable batch frames
- no background snapshot scheduler
- no secondary indexes
- no query planner or schema system
- no regex JSON queries yet
- no field-level or secondary indexes beyond equality indexes and per-collection sorted key slices
- no interactive merge resolution during repair
- no nested key-field extraction during NDJSON import
- no full visual side-by-side diff viewer before overwrite imports
- no preset sync across machines or users
- no preset-aware autocomplete in the console yet
- no streaming background import progress UI
- no scheduled background maintenance worker yet

## Roadmap After V7.0

- configurable automatic snapshot/compaction policies
- stronger lock stale-state recovery
- richer validation / repair tooling
- optional collection namespaces
- deeper document-oriented helpers and wildcard query workflows
- richer visual diffs, schema suggestions, and non-SQL dataset workflows
