package tests

import (
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

	if err := db.Delete("user:1"); err != nil {
		t.Fatalf("delete record: %v", err)
	}

	if _, ok := db.Get("user:1"); ok {
		t.Fatal("expected deleted key to be missing")
	}
}

func TestPersistenceAfterReopen(t *testing.T) {
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

func TestReplayRecovery(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("alpha", "1"); err != nil {
		t.Fatalf("set alpha: %v", err)
	}
	if err := db.Set("beta", "2"); err != nil {
		t.Fatalf("set beta: %v", err)
	}
	if err := db.Delete("alpha"); err != nil {
		t.Fatalf("delete alpha: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer reopened.Close()

	if _, ok := reopened.Get("alpha"); ok {
		t.Fatal("expected deleted key to stay deleted after replay")
	}

	value, ok := reopened.Get("beta")
	if !ok || value != "2" {
		t.Fatalf("unexpected beta value after replay: value=%q ok=%v", value, ok)
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

func TestCompaction(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("stale", "old"); err != nil {
		t.Fatalf("set stale old: %v", err)
	}
	if err := db.Set("stale", "new"); err != nil {
		t.Fatalf("set stale new: %v", err)
	}
	if err := db.Set("keep", "value"); err != nil {
		t.Fatalf("set keep: %v", err)
	}
	if err := db.Delete("stale"); err != nil {
		t.Fatalf("delete stale: %v", err)
	}

	statsBefore, err := db.Stats()
	if err != nil {
		t.Fatalf("stats before compaction: %v", err)
	}

	if err := db.Compact(); err != nil {
		t.Fatalf("compact db: %v", err)
	}

	statsAfter, err := db.Stats()
	if err != nil {
		t.Fatalf("stats after compaction: %v", err)
	}

	if statsAfter.LogFileSize >= statsBefore.LogFileSize {
		t.Fatalf("expected compacted log to be smaller: before=%d after=%d", statsBefore.LogFileSize, statsAfter.LogFileSize)
	}

	if statsAfter.LastCompactionTime.IsZero() {
		t.Fatal("expected last compaction time to be recorded")
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen after compaction: %v", err)
	}
	defer reopened.Close()

	if _, ok := reopened.Get("stale"); ok {
		t.Fatal("expected deleted key to stay absent after compaction")
	}

	value, ok := reopened.Get("keep")
	if !ok || value != "value" {
		t.Fatalf("unexpected kept value after compaction: value=%q ok=%v", value, ok)
	}
}

func TestIncompleteFinalLogRecordIsIgnored(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.Set("safe", "value"); err != nil {
		t.Fatalf("set safe value: %v", err)
	}

	logPath := filepath.Join(dir, "minidb.log")
	file, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log for append: %v", err)
	}

	if _, err := file.Write([]byte("BROKEN")); err != nil {
		t.Fatalf("append incomplete bytes: %v", err)
	}
	file.Close()

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

	logPath := filepath.Join(dir, "minidb.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	data[0] = 'X'
	if err := os.WriteFile(logPath, data, 0o644); err != nil {
		t.Fatalf("write corrupted log: %v", err)
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
