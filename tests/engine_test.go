package tests

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"minidb-studio/internal/engine"
)

func openTestDB(t *testing.T) (*engine.DB, string) {
	t.Helper()

	dir := t.TempDir()
	db, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	return db, dir
}

func openTestDBWithOptions(t *testing.T, options engine.OpenOptions) (*engine.DB, string) {
	t.Helper()

	dir := t.TempDir()
	db, err := engine.OpenInDirWithOptions(dir, options)
	if err != nil {
		t.Fatalf("open test db with options: %v", err)
	}

	return db, dir
}

func TestSetGetDelete(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.Set("user:1", "Alice"); err != nil {
		t.Fatalf("set record: %v", err)
	}

	value, ok := db.Get("user:1")
	if !ok || value != "Alice" {
		t.Fatalf("unexpected get result: value=%q ok=%v", value, ok)
	}

	metadata, ok := db.GetRecordMetadata("user:1")
	if !ok {
		t.Fatal("expected metadata for user:1")
	}
	if metadata.ValueSize != len("Alice") {
		t.Fatalf("unexpected value size: %d", metadata.ValueSize)
	}

	if err := db.Delete("user:1"); err != nil {
		t.Fatalf("delete record: %v", err)
	}

	if _, ok := db.Get("user:1"); ok {
		t.Fatal("expected deleted key to be missing")
	}
}

func TestPersistenceAfterCloseAndReopen(t *testing.T) {
	db, dir := openTestDB(t)

	multilineValue := "first line\nsecond line\nthird line"
	if err := db.Set("note:1", multilineValue); err != nil {
		t.Fatalf("set multiline value: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer reopened.Close()

	value, ok := reopened.Get("note:1")
	if !ok || value != multilineValue {
		t.Fatalf("unexpected persisted value: value=%q ok=%v", value, ok)
	}
}

func TestFileLocking(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	_, err := engine.OpenInDir(dir)
	if err == nil {
		t.Fatal("expected second open to fail because of lock")
	}
	if !errors.Is(err, engine.ErrDatabaseLocked) {
		t.Fatalf("expected ErrDatabaseLocked, got %v", err)
	}
}

func TestSnapshotCreationAndReload(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("alpha", "1"); err != nil {
		t.Fatalf("set alpha: %v", err)
	}
	if err := db.Set("beta", "2"); err != nil {
		t.Fatalf("set beta: %v", err)
	}

	if err := db.Snapshot(); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	stats, err := db.Stats()
	if err != nil {
		t.Fatalf("stats after snapshot: %v", err)
	}
	if stats.SnapshotCount != 1 {
		t.Fatalf("expected one snapshot, got %d", stats.SnapshotCount)
	}

	if err := db.Set("gamma", "3"); err != nil {
		t.Fatalf("set gamma after snapshot: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer reopened.Close()

	for key, expected := range map[string]string{"alpha": "1", "beta": "2", "gamma": "3"} {
		value, ok := reopened.Get(key)
		if !ok || value != expected {
			t.Fatalf("unexpected value after snapshot reload for %q: value=%q ok=%v", key, value, ok)
		}
	}

	reopenedStats, err := reopened.Stats()
	if err != nil {
		t.Fatalf("stats after reopen: %v", err)
	}
	if reopenedStats.StartupReplayCount == 0 {
		t.Fatal("expected some replay activity after loading snapshot and later mutations")
	}
}

func TestSegmentedRecovery(t *testing.T) {
	db, dir := openTestDBWithOptions(t, engine.OpenOptions{SegmentSizeLimit: 220})

	largeValue := strings.Repeat("x", 180)
	for i := 0; i < 5; i++ {
		if err := db.Set(fmt.Sprintf("key:%d", i), largeValue); err != nil {
			t.Fatalf("set large value %d: %v", i, err)
		}
	}

	stats, err := db.Stats()
	if err != nil {
		t.Fatalf("stats before reopen: %v", err)
	}
	if stats.SegmentCount < 2 {
		t.Fatalf("expected multiple segments, got %d", stats.SegmentCount)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDirWithOptions(dir, engine.OpenOptions{SegmentSizeLimit: 220})
	if err != nil {
		t.Fatalf("reopen segmented db: %v", err)
	}
	defer reopened.Close()

	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key:%d", i)
		value, ok := reopened.Get(key)
		if !ok || value != largeValue {
			t.Fatalf("unexpected segmented recovery value for %q: value length=%d ok=%v", key, len(value), ok)
		}
	}
}

func TestPrefixKeyListing(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	records := map[string]string{
		"user:1":  "A",
		"user:2":  "B",
		"order:1": "C",
	}

	for key, value := range records {
		if err := db.Set(key, value); err != nil {
			t.Fatalf("set %q: %v", key, err)
		}
	}

	keys := db.Keys("user:")
	if len(keys) != 2 || keys[0] != "user:1" || keys[1] != "user:2" {
		t.Fatalf("unexpected prefix keys: %#v", keys)
	}
}

func TestRecordsLazyPreview(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	value := "hello\nworld\nfrom minidb"
	if err := db.Set("preview:key", value); err != nil {
		t.Fatalf("set preview value: %v", err)
	}

	records := db.Records("preview:", 0, 10)
	if len(records) != 1 {
		t.Fatalf("expected one preview record, got %d", len(records))
	}
	if !strings.Contains(records[0].ValuePreview, "\\n") {
		t.Fatalf("expected escaped newline preview, got %q", records[0].ValuePreview)
	}
}

func TestBatchAtomicity(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.ApplyBatch([]engine.BatchOperation{
		{Command: "SET", Key: "user:1", Value: "Alice"},
		{Command: "SET", Key: "user:2", Value: "Bob"},
	}); err != nil {
		t.Fatalf("apply successful batch: %v", err)
	}

	if err := db.ApplyBatch([]engine.BatchOperation{
		{Command: "SET", Key: "user:3", Value: "Carol"},
		{Command: "DELETE", Key: "missing:key"},
	}); err == nil {
		t.Fatal("expected invalid batch to fail")
	}

	if _, ok := db.Get("user:3"); ok {
		t.Fatal("expected failed batch not to partially update in-memory state")
	}

	result, err := db.Execute("BATCH\nSET user:4 Dana\nDELETE user:2\nEND")
	if err != nil {
		t.Fatalf("execute batch command: %v", err)
	}
	if result != "OK" {
		t.Fatalf("unexpected batch command result: %q", result)
	}

	if _, ok := db.Get("user:2"); ok {
		t.Fatal("expected user:2 to be deleted by batch command")
	}
}

func TestCompactionWithSegments(t *testing.T) {
	db, dir := openTestDBWithOptions(t, engine.OpenOptions{SegmentSizeLimit: 220})

	largeValue := strings.Repeat("y", 180)
	if err := db.Set("stale", largeValue); err != nil {
		t.Fatalf("set stale old: %v", err)
	}
	if err := db.Set("stale", largeValue+"new"); err != nil {
		t.Fatalf("set stale new: %v", err)
	}
	if err := db.Set("keep", largeValue); err != nil {
		t.Fatalf("set keep: %v", err)
	}
	if err := db.Delete("stale"); err != nil {
		t.Fatalf("delete stale: %v", err)
	}

	before, err := db.Stats()
	if err != nil {
		t.Fatalf("stats before compaction: %v", err)
	}

	if err := db.Compact(); err != nil {
		t.Fatalf("compact db: %v", err)
	}

	after, err := db.Stats()
	if err != nil {
		t.Fatalf("stats after compaction: %v", err)
	}
	if after.SegmentCount > before.SegmentCount {
		t.Fatalf("expected segment count to not grow after compaction: before=%d after=%d", before.SegmentCount, after.SegmentCount)
	}
	if after.LastCompactionTime.IsZero() {
		t.Fatal("expected compaction time to be recorded")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDirWithOptions(dir, engine.OpenOptions{SegmentSizeLimit: 220})
	if err != nil {
		t.Fatalf("reopen db after compaction: %v", err)
	}
	defer reopened.Close()

	if _, ok := reopened.Get("stale"); ok {
		t.Fatal("expected stale key to remain deleted after compaction")
	}
	if _, ok := reopened.Get("keep"); !ok {
		t.Fatal("expected keep key after compaction")
	}
}

func TestValidateHealthyDatabase(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.Set("alpha", "1"); err != nil {
		t.Fatalf("set alpha: %v", err)
	}

	report, err := db.Validate()
	if err != nil {
		t.Fatalf("validate healthy db: %v", err)
	}
	if report.SegmentCount != 1 {
		t.Fatalf("expected one segment, got %d", report.SegmentCount)
	}
	if len(report.CorruptedSegments) != 0 {
		t.Fatalf("expected no corrupted segments, got %#v", report.CorruptedSegments)
	}
}

func TestIncompleteFinalLogRecordIsIgnoredAndReported(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("safe", "value"); err != nil {
		t.Fatalf("set safe value: %v", err)
	}

	segmentPath := filepath.Join(dir, "segment-000001.log")
	file, err := os.OpenFile(segmentPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open segment for append: %v", err)
	}
	if _, err := file.Write([]byte("BROKEN")); err != nil {
		t.Fatalf("append incomplete bytes: %v", err)
	}
	file.Close()

	report, err := db.Validate()
	if err != nil {
		t.Fatalf("validate after incomplete tail: %v", err)
	}
	if len(report.IncompleteTailSegments) != 1 {
		t.Fatalf("expected one incomplete-tail segment, got %#v", report.IncompleteTailSegments)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen with incomplete tail: %v", err)
	}
	defer reopened.Close()

	value, ok := reopened.Get("safe")
	if !ok || value != "value" {
		t.Fatalf("unexpected recovered value: value=%q ok=%v", value, ok)
	}
}

func TestEarlierCorruptionReturnsRecoveryError(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("alpha", "value"); err != nil {
		t.Fatalf("set alpha: %v", err)
	}
	if err := db.Set("beta", "value"); err != nil {
		t.Fatalf("set beta: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	segmentPath := filepath.Join(dir, "segment-000001.log")
	data, err := os.ReadFile(segmentPath)
	if err != nil {
		t.Fatalf("read segment file: %v", err)
	}

	data[0] = 'X'
	if err := os.WriteFile(segmentPath, data, 0o644); err != nil {
		t.Fatalf("write corrupted segment: %v", err)
	}

	_, err = engine.OpenInDir(dir)
	if err == nil {
		t.Fatal("expected recovery error for earlier corruption")
	}
	if !strings.Contains(err.Error(), "log recovery error") {
		t.Fatalf("expected recovery error message, got: %v", err)
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	var writers sync.WaitGroup
	for i := 0; i < 20; i++ {
		writers.Add(1)
		go func(id int) {
			defer writers.Done()
			key := fmt.Sprintf("user:%d", id)
			if err := db.Set(key, "value"); err != nil {
				t.Errorf("set in goroutine %d: %v", id, err)
			}
		}(i)
	}

	var readers sync.WaitGroup
	for i := 0; i < 20; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			_ = db.Keys("user")
		}()
	}

	writers.Wait()
	readers.Wait()

	keys := db.Keys("")
	if len(keys) != 20 {
		t.Fatalf("expected 20 keys after concurrent writes, got %d", len(keys))
	}
}
