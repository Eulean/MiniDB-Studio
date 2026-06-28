# MiniDB Studio Agent Notes

## Current Phase
- Project complete for MiniDB Studio v1

## Goals In Progress
- Scaffold the Go module and project layout. Completed.
- Build the MiniDB append-only key-value engine from scratch. Completed.
- Add unit tests for persistence, recovery, compaction, and concurrency. Completed.
- Build the native Fyne desktop UI with working explorer, console, maintenance, and about pages. Completed.
- Finalize README and delivery notes. Completed.

## Architecture Direction
- UI framework: Fyne v2 for the native desktop shell in a later phase.
- Database engine: custom append-only log plus in-memory index.
- Durability: every mutating command appends a framed record and calls `Sync`.
- Recovery: replay the log on startup; ignore only an incomplete final record.
- Storage location: application data folder, not the executable directory.

## Current Decisions
- Log records will use a binary frame:
  - 4-byte magic header
  - 4-byte payload length
  - JSON payload
  - 4-byte CRC32 checksum
- This lets us safely store values with spaces and newlines, detect corruption, and distinguish incomplete trailing data.
- Compaction metadata will be stored in a small sidecar metadata file so last compaction time survives restarts.
- Operation totals are also stored in metadata so compaction does not reset historical stats.
- Compaction swaps files in a Windows-safe way by moving the old log aside before renaming the compacted file into place.
- The runnable desktop entrypoint is behind the `desktop` build tag so default `go test ./...` and `go vet ./...` work in environments that do not have Fyne desktop build prerequisites installed.

## Validation
- `go test ./...` passed.
- `go vet ./...` passed.
- The first `go test` occasionally hit a transient Windows file-handle cleanup issue while removing a temporary test executable, but reruns passed cleanly and it did not reflect a project bug.
- The tagged desktop build was verified successfully in this workspace using Zig as a local CGO compiler:
  - `CGO_ENABLED=1`
  - `CC="C:\zig-local\zig.exe cc"`
  - `CXX="C:\zig-local\zig.exe c++"`

## Deliverables Completed
- `internal/storage`: app-data path resolution for Windows-first local storage.
- `internal/engine`: open/close, set/get/delete, key listing, stats, command execution, recovery, and compaction.
- `internal/app`: application orchestration and UI-facing shared state.
- `internal/ui`: native Fyne window, navigation, explorer, console, maintenance, dialogs, and about page.
- `tests`: unit coverage for persistence, replay, corruption handling, compaction, prefix filtering, and concurrency.
- `cmd/minidb`: stub entrypoint for default validation plus tagged desktop entrypoint for the actual native app.
- `scripts`: helper scripts for setting up a local Zig toolchain and building the native desktop executable.
- `.gitignore`: ignores generated desktop binaries and downloaded local toolchains.

## Next Steps
- Optional future work belongs to MiniDB Studio v2, not this v1 delivery.
