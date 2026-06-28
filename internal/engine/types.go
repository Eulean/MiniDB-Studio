package engine

import "time"

const (
	defaultSegmentSizeLimit = 1 << 20
	frameMagic              = "MDB2"
	sourceTypeSegment       = "segment"
	sourceTypeSnapshot      = "snapshot"
	payloadKindMutation     = "mutation_batch"
	payloadKindSnapshotHead = "snapshot_header"
	payloadKindSnapshotItem = "snapshot_entry"
)

const (
	commandSet    = "SET"
	commandDelete = "DELETE"
)

const (
	DefaultCollection = "default"
	ValueKindRaw      = "raw"
	ValueKindJSON     = "json"
)

// OpenOptions lets tests and future app features tune database internals safely.
type OpenOptions struct {
	SegmentSizeLimit int64
}

func (o OpenOptions) withDefaults() OpenOptions {
	if o.SegmentSizeLimit <= 0 {
		o.SegmentSizeLimit = defaultSegmentSizeLimit
	}

	return o
}

// Record is the explorer-friendly view of one live key and its metadata.
type Record struct {
	Collection   string
	Key          string
	ValuePreview string
	ValueSize    int
	ValueKind    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EntryMetadata is the full metadata view for one live record.
type EntryMetadata struct {
	Collection   string
	Key          string
	ValueSize    int
	ValueKind    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastSequence uint64
}

// BatchOperation is the public API for atomic multi-operation writes.
type BatchOperation struct {
	Command    string
	Collection string
	Key        string
	Value      string
	ValueKind  string
}

// JSONQueryCondition describes one equality predicate against an indexed JSON path.
type JSONQueryCondition struct {
	Path     string
	Operator string
	Value    string
}

// Stats describes the current durable and in-memory database state.
type Stats struct {
	LiveKeyCount       int
	TotalLiveDataSize  int64
	ActiveSegmentSize  int64
	SegmentCount       int
	SnapshotCount      int
	TotalSetOperations uint64
	TotalDeleteOps     uint64
	LastCompactionTime time.Time
	LastSnapshotTime   time.Time
	StartupReplayCount uint64
}

// ValidationReport summarizes snapshot and segment health without mutating the database.
type ValidationReport struct {
	SnapshotPresent        bool
	SnapshotValid          bool
	SnapshotSequence       uint64
	SegmentCount           int
	ValidSegments          []string
	IncompleteTailSegments []string
	CorruptedSegments      []string
	Warnings               []string
}

// ExportReport describes the result of exporting one collection or the whole database.
type ExportReport struct {
	DestinationPath string
	Collection      string
	ExportedRecords int
}

// RepairReport describes a salvage operation into a fresh database directory.
type RepairReport struct {
	DestinationDir   string
	RecoveredRecords int
	UsedSnapshot     bool
	Warnings         []string
}

// MaintenanceReport summarizes health and recommendation signals for the maintenance UI.
type MaintenanceReport struct {
	Healthy               bool
	Recommendations       []string
	SnapshotRecommended   bool
	CompactionRecommended bool
	ValidationRecommended bool
}

// indexEntry is the in-memory pointer to the latest durable value for a key.
type indexEntry struct {
	CanonicalKey string              `json:"canonical_key"`
	Collection   string              `json:"collection"`
	Key          string              `json:"key"`
	ValueKind    string              `json:"value_kind"`
	JSONFields   map[string][]string `json:"json_fields,omitempty"`
	SourceType   string              `json:"source_type"`
	SourcePath   string              `json:"source_path"`
	FrameOffset  int64               `json:"frame_offset"`
	OperationIdx int                 `json:"operation_index"`
	ValueSize    int                 `json:"value_size"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	LastSequence uint64              `json:"last_sequence"`
}

// mutationBatch is the durable write unit stored in segment files.
// Single SET/DELETE commands are represented as a batch with one operation.
type mutationBatch struct {
	Kind        string               `json:"kind"`
	CommittedAt time.Time            `json:"committed_at"`
	Operations  []persistedOperation `json:"operations"`
}

// persistedOperation is one SET or DELETE operation inside a durable mutation batch.
type persistedOperation struct {
	Sequence   uint64    `json:"sequence"`
	Command    string    `json:"command"`
	Collection string    `json:"collection,omitempty"`
	Key        string    `json:"key"`
	Value      string    `json:"value,omitempty"`
	ValueKind  string    `json:"value_kind,omitempty"`
	ValueSize  int       `json:"value_size"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// snapshotHeader identifies the snapshot cutoff point and creation time.
type snapshotHeader struct {
	Kind         string    `json:"kind"`
	LastSequence uint64    `json:"last_sequence"`
	CreatedAt    time.Time `json:"created_at"`
}

// snapshotEntry stores one fully materialized live key inside a snapshot file.
type snapshotEntry struct {
	Kind       string    `json:"kind"`
	Sequence   uint64    `json:"sequence"`
	Collection string    `json:"collection,omitempty"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	ValueKind  string    `json:"value_kind,omitempty"`
	ValueSize  int       `json:"value_size"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// metadata persists counters, timestamps, and sequencing information across restarts.
type metadata struct {
	ActiveSegmentID    uint64    `json:"active_segment_id"`
	NextSegmentID      uint64    `json:"next_segment_id"`
	NextSequence       uint64    `json:"next_sequence"`
	LastSnapshotTime   time.Time `json:"last_snapshot_time"`
	LastSnapshotSeq    uint64    `json:"last_snapshot_sequence"`
	LastCompactionTime time.Time `json:"last_compaction_time"`
	TotalSetOperations uint64    `json:"total_set_operations"`
	TotalDeleteOps     uint64    `json:"total_delete_operations"`
	StartupReplayCount uint64    `json:"startup_replay_count"`
}
