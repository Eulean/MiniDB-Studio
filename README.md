# MiniDB Studio

MiniDB Studio is a Windows-first native desktop application built with Go and Fyne around a custom persistent key-value engine written from scratch with the Go standard library.

This is MiniDB v1:
- local single-process storage
- append-only durability
- in-memory indexing
- replay-based recovery
- manual compaction
- native desktop management tools

It is intentionally not a SQL database, network service, or distributed system yet.

## Project Purpose

MiniDB Studio has two goals:

1. Teach how a small durable database engine can be built from first principles.
2. Provide a practical native desktop tool for managing that engine locally on Windows.

## Architecture Diagram

```text
MiniDB Studio Desktop App
|
+-- cmd/minidb
|   +-- desktop entrypoint
|
+-- internal/ui
|   +-- MainWindow
|   +-- Data Explorer page
|   +-- Command Console page
|   +-- Maintenance page
|   +-- Record dialogs
|
+-- internal/app
|   +-- Application orchestration
|   +-- UI-facing state and status
|
+-- internal/engine
|   +-- DB lifecycle
|   +-- Commands (SET/GET/DELETE/KEYS/STATS/COMPACT)
|   +-- Append-only log writer
|   +-- Startup recovery / replay
|   +-- Compaction
|   +-- Statistics and metadata
|
+-- internal/storage
|   +-- Windows-first app-data path resolution
|
+-- tests
    +-- engine unit tests
```

## Final File Tree

```text
minidb-studio/
  AGENT.md
  README.md
  go.mod
  go.sum
  cmd/
    minidb/
      main_desktop.go
      main_stub.go
  scripts/
    build-desktop.ps1
    setup-local-zig.ps1
  internal/
    app/
      application.go
      state.go
    engine/
      commands.go
      compact.go
      db.go
      log.go
      metadata_codec.go
      recovery.go
      stats.go
    storage/
      paths.go
    ui/
      console_page.go
      dialogs.go
      explorer_page.go
      main_window.go
      maintenance_page.go
  tests/
    engine_test.go
```

## Setup

### Prerequisites

- Go 1.23 or newer
- Windows is the primary target
- For running the native Fyne desktop build, install a working C toolchain in `PATH` because Fyne's desktop driver depends on CGO on Windows

### Clone And Prepare

```powershell
git clone <your-repo-url>
cd BuildYourOwnDatabase
go mod tidy
```

## How Persistence Works

MiniDB stores data in a local application-data folder instead of beside the executable.

On Windows the default location is:

```text
%LOCALAPPDATA%\MiniDB Studio
```

The engine uses:

- `minidb.log` for the append-only operation log
- `minidb.meta.json` for durable metadata such as compaction time and operation counters

### Write Path

For `SET` and `DELETE`:

1. Build a durable record.
2. Encode it as:
   - 4-byte magic header
   - 4-byte payload length
   - JSON payload
   - 4-byte CRC32 checksum
3. Append it to the log.
4. Call `Sync`.
5. Update the in-memory index.

### Read Path

- Reads come from an in-memory `map[string]string`.
- `sync.RWMutex` protects concurrent access inside one process.

### Recovery Path

On startup:

1. Open the log.
2. Replay records in order.
3. Rebuild the in-memory index.
4. Recalculate live state.

If the final record is incomplete because of an unexpected shutdown, MiniDB ignores only that final incomplete record.

If corruption is detected earlier in the log, startup returns a clear recovery error.

### Compaction Path

`COMPACT`:

1. Writes only live records into a temporary compacted log.
2. Syncs the compacted file.
3. Safely replaces the old log.
4. Reloads the in-memory state.
5. Stores the last compaction timestamp.

## Supported Commands

The command console supports one command at a time:

- `SET key value`
- `GET key`
- `DELETE key`
- `KEYS`
- `KEYS prefix`
- `STATS`
- `COMPACT`

Notes:

- Keys cannot be empty.
- Values may contain spaces.
- Values may contain newlines when entered in the multi-line console input.

## Desktop Features

### Data Explorer

- Filter by key prefix
- Browse records in a two-column table
- View full record details
- Create records
- Edit records
- Delete records with confirmation

### Command Console

- Multi-line input
- Run one command at a time
- Monospace output/history panel
- Readable error reporting

### Maintenance

- View stats
- Compact the database
- Create a backup of the durable log
- Open the local data folder

### Status Bar

- Database location
- Current status
- Last operation result

## Run The App

### Default Non-Desktop Entrypoint

This keeps `go test ./...` and `go vet ./...` green in environments without Fyne desktop build prerequisites:

```powershell
go run ./cmd/minidb
```

### Native Desktop App

To run the real Fyne desktop app:

```powershell
$env:CGO_ENABLED="1"
go run -tags desktop ./cmd/minidb
```

If CGO or a C compiler is not installed, the desktop build will not start until that toolchain is available.

## Package A Windows Executable

### Build With Go

```powershell
$env:CGO_ENABLED="1"
go build -tags desktop -o .\dist\MiniDBStudio.exe ./cmd/minidb
```

### Build With The Included Helper Scripts

```powershell
.\scripts\setup-local-zig.ps1
.\scripts\build-desktop.ps1
```

These scripts install a local Zig toolchain at `C:\zig-local` and use it as the CGO compiler for the Fyne desktop build.

### Package With Fyne CLI

```powershell
go install fyne.io/tools/cmd/fyne@latest
$env:CGO_ENABLED="1"
fyne package -tags desktop -os windows -name "MiniDB Studio"
```

## Validation

The following commands pass in this workspace:

```powershell
go test ./...
go vet ./...
```

Note:

- The real desktop executable build was implemented but could not be fully compiled in this workspace because the local environment does not currently have a usable C compiler in `PATH` for CGO.
- That gap is now handled with a verified local Zig-based build path:

```powershell
$env:CGO_ENABLED="1"
$env:CC="C:\zig-local\zig.exe cc"
$env:CXX="C:\zig-local\zig.exe c++"
go build -tags desktop ./cmd/minidb
```

## Limitations Of v1

- No SQL parser
- No networking
- No authentication
- No multi-user access
- No replication
- No distributed storage
- No cloud sync
- No transactions
- No secondary indexes
- No background compaction scheduler
- No schema or typed records

## Roadmap For v2

- Better command parsing with quoted keys and values
- Batch command execution
- Import and export tools
- Optional snapshots
- Background compaction suggestions
- Safer key rename workflow
- Stronger log repair tooling
- Richer desktop search and sort tools
