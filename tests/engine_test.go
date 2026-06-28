package tests

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func writeLegacyFrameFile(t *testing.T, path string, records []map[string]any) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("create legacy frame file: %v", err)
	}
	defer file.Close()

	for _, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal legacy record: %v", err)
		}

		header := make([]byte, 8)
		copy(header[:4], []byte("MDB1"))
		binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

		checksum := make([]byte, 4)
		binary.BigEndian.PutUint32(checksum, crc32.ChecksumIEEE(payload))

		if _, err := file.Write(header); err != nil {
			t.Fatalf("write legacy header: %v", err)
		}
		if _, err := file.Write(payload); err != nil {
			t.Fatalf("write legacy payload: %v", err)
		}
		if _, err := file.Write(checksum); err != nil {
			t.Fatalf("write legacy checksum: %v", err)
		}
	}
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

func TestLegacyMDB1SegmentMigratesOnOpen(t *testing.T) {
	dir := t.TempDir()
	legacySegment := filepath.Join(dir, "segment-000001.log")
	writeLegacyFrameFile(t, legacySegment, []map[string]any{
		{
			"command":   "SET",
			"key":       "legacy:user",
			"value":     "Ada",
			"timestamp": time.Date(2026, 6, 28, 3, 37, 48, 0, time.UTC),
		},
	})

	if err := os.WriteFile(filepath.Join(dir, "minidb.meta.json"), []byte(`{"snapshot_record_count":0}`), 0o644); err != nil {
		t.Fatalf("write legacy metadata: %v", err)
	}

	db, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("open migrated legacy db: %v", err)
	}
	defer db.Close()

	value, ok := db.Get("legacy:user")
	if !ok || value != "Ada" {
		t.Fatalf("unexpected migrated value: value=%q ok=%v", value, ok)
	}

	backups, err := filepath.Glob(filepath.Join(dir, "legacy-backup-*"))
	if err != nil {
		t.Fatalf("glob legacy backup dir: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one legacy backup directory, got %v", backups)
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

func TestStaleLockFileIsRecovered(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "minidb.lock")
	if err := os.WriteFile(lockPath, []byte("999999"), 0o644); err != nil {
		t.Fatalf("write stale lock file: %v", err)
	}

	original := engine.SetProcessExistsForLockForTests(func(pid int) (bool, error) {
		return false, nil
	})
	defer engine.RestoreProcessExistsForLockForTests(original)

	db, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("open db with stale lock: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected recreated lock file, got stat error: %v", err)
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

func TestCollectionsAreSeparated(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetInCollection("users", "1", "Alice"); err != nil {
		t.Fatalf("set users/1: %v", err)
	}
	if err := db.SetInCollection("orders", "1", "Order-1"); err != nil {
		t.Fatalf("set orders/1: %v", err)
	}

	userValue, ok := db.GetFromCollection("users", "1")
	if !ok || userValue != "Alice" {
		t.Fatalf("unexpected users/1 value: %q ok=%v", userValue, ok)
	}

	orderValue, ok := db.GetFromCollection("orders", "1")
	if !ok || orderValue != "Order-1" {
		t.Fatalf("unexpected orders/1 value: %q ok=%v", orderValue, ok)
	}

	collections := db.Collections()
	if len(collections) < 2 {
		t.Fatalf("expected multiple collections, got %#v", collections)
	}
}

func TestPagedCollectionListing(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	for i := 0; i < 7; i++ {
		if err := db.SetInCollection("users", fmt.Sprintf("user:%d", i), "value"); err != nil {
			t.Fatalf("set paged record %d: %v", i, err)
		}
	}

	pageOne := db.RecordsInCollection("users", "", 0, 3)
	pageTwo := db.RecordsInCollection("users", "", 3, 3)
	if len(pageOne) != 3 || len(pageTwo) != 3 {
		t.Fatalf("unexpected page lengths: %d %d", len(pageOne), len(pageTwo))
	}
	if pageOne[0].Key == pageTwo[0].Key {
		t.Fatalf("expected different records across pages, got %q", pageOne[0].Key)
	}
	if db.CountRecordsInCollection("users", "") != 7 {
		t.Fatalf("unexpected collection count")
	}
}

func TestCollectionKeysStaySortedAcrossMutations(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	for _, key := range []string{"user:20", "user:03", "user:11", "user:01"} {
		if err := db.SetInCollection("users", key, "value"); err != nil {
			t.Fatalf("set %q: %v", key, err)
		}
	}

	if err := db.DeleteFromCollection("users", "user:11"); err != nil {
		t.Fatalf("delete user:11: %v", err)
	}
	if err := db.SetInCollection("users", "user:05", "value"); err != nil {
		t.Fatalf("set user:05: %v", err)
	}

	got := db.KeysInCollection("users", "")
	want := []string{"user:01", "user:03", "user:05", "user:20"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected sorted keys: got=%v want=%v", got, want)
	}
}

func TestCollectionPrefixPagingUsesSortedSlice(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	for _, key := range []string{"acct:001", "acct:002", "acct:010", "other:001", "acct:020"} {
		if err := db.SetInCollection("users", key, "value"); err != nil {
			t.Fatalf("set %q: %v", key, err)
		}
	}

	records := db.RecordsInCollection("users", "acct:", 1, 2)
	if len(records) != 2 {
		t.Fatalf("expected two paged records, got %d", len(records))
	}
	if records[0].Key != "acct:002" || records[1].Key != "acct:010" {
		t.Fatalf("unexpected paged prefix keys: %#v", records)
	}
	if db.CountRecordsInCollection("users", "acct:") != 4 {
		t.Fatalf("unexpected prefix count")
	}
}

func TestRecoveryRebuildsCollectionIndexes(t *testing.T) {
	db, dir := openTestDB(t)

	for _, key := range []string{"acct:100", "acct:002", "acct:010"} {
		if err := db.SetInCollection("users", key, "value"); err != nil {
			t.Fatalf("set %q: %v", key, err)
		}
	}
	if err := db.DeleteFromCollection("users", "acct:010"); err != nil {
		t.Fatalf("delete acct:010: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer reopened.Close()

	got := reopened.KeysInCollection("users", "acct:")
	want := []string{"acct:002", "acct:100"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected recovered keys: got=%v want=%v", got, want)
	}
}

func TestJSONValidationAndMetadata(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile", `{"name":"Ada"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set json record: %v", err)
	}

	if err := db.SetTypedInCollection("docs", "bad", `{"name":`, engine.ValueKindJSON); err == nil {
		t.Fatal("expected invalid json to fail")
	}

	metadata, ok := db.GetRecordMetadataInCollection("docs", "profile")
	if !ok {
		t.Fatal("expected metadata for docs/profile")
	}
	if metadata.ValueKind != engine.ValueKindJSON {
		t.Fatalf("expected json kind, got %q", metadata.ValueKind)
	}
}

func TestFindKeysByJSONField(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"name":"Ada","email":"ada@example.com","active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"name":"Grace","email":"grace@example.com","active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"name":"Linus","active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	matches := db.FindKeysByJSONFieldInCollection("docs", "", "active", "true")
	want := []string{"profile:1", "profile:2"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected json matches: got=%v want=%v", matches, want)
	}

	paged := db.RecordsByJSONFieldInCollection("docs", "profile:", "active", "true", 1, 1)
	if len(paged) != 1 || paged[0].Key != "profile:2" {
		t.Fatalf("unexpected paged json query result: %#v", paged)
	}
}

func TestFindKeysByNestedJSONPathAndMultipleConditions(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"email":"ada@example.com","role":"admin"},"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"email":"ada@example.com","role":"viewer"},"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"email":"grace@example.com","role":"admin"},"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	matches := db.FindKeysByJSONConditionsInCollection("docs", "", []engine.JSONQueryCondition{
		{Path: "profile.email", Value: "ada@example.com"},
		{Path: "active", Value: "true"},
	})
	want := []string{"profile:1", "profile:2"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected nested-path matches: got=%v want=%v", matches, want)
	}

	adminMatches := db.FindKeysByJSONConditionsInCollection("docs", "", []engine.JSONQueryCondition{
		{Path: "profile.email", Value: "ada@example.com"},
		{Path: "profile.role", Value: "admin"},
		{Path: "active", Value: "true"},
	})
	if len(adminMatches) != 1 || adminMatches[0] != "profile:1" {
		t.Fatalf("unexpected multi-condition intersection: %v", adminMatches)
	}
}

func TestJSONQueryOperatorsForContainsNumericAndArrayMembership(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"bio":"Ada builds local databases","score":95},"tags":["admin","builder"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"bio":"Grace reviews systems","score":82},"tags":["reviewer"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"bio":"Linus maintains kernels","score":76},"tags":["admin"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	containsMatches := db.FindKeysByJSONConditionsInCollection("docs", "", []engine.JSONQueryCondition{
		{Path: "profile.bio", Operator: "~=", Value: "local"},
	})
	if len(containsMatches) != 1 || containsMatches[0] != "profile:1" {
		t.Fatalf("unexpected contains matches: %v", containsMatches)
	}

	numericMatches := db.FindKeysByJSONConditionsInCollection("docs", "", []engine.JSONQueryCondition{
		{Path: "profile.score", Operator: ">=", Value: "80"},
		{Path: "active", Operator: "=", Value: "true"},
	})
	wantNumeric := []string{"profile:1", "profile:2"}
	if strings.Join(numericMatches, ",") != strings.Join(wantNumeric, ",") {
		t.Fatalf("unexpected numeric matches: got=%v want=%v", numericMatches, wantNumeric)
	}

	arrayMatches := db.FindKeysByJSONConditionsInCollection("docs", "", []engine.JSONQueryCondition{
		{Path: "tags", Operator: "=", Value: "admin"},
		{Path: "active", Operator: "=", Value: "true"},
	})
	if len(arrayMatches) != 1 || arrayMatches[0] != "profile:1" {
		t.Fatalf("unexpected array membership matches: %v", arrayMatches)
	}
}

func TestJSONFieldIndexUpdatesAcrossOverwriteDeleteAndReopen(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.SetTypedInCollection("docs", "profile", `{"email":"ada@example.com","active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set initial json record: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile", `{"email":"grace@example.com","active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("overwrite json record: %v", err)
	}

	if matches := db.FindKeysByJSONFieldInCollection("docs", "", "email", "ada@example.com"); len(matches) != 0 {
		t.Fatalf("expected overwritten email index to be removed, got %v", matches)
	}
	if matches := db.FindKeysByJSONFieldInCollection("docs", "", "email", "grace@example.com"); len(matches) != 1 {
		t.Fatalf("expected replacement email index, got %v", matches)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := engine.OpenInDir(dir)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}

	if matches := reopened.FindKeysByJSONFieldInCollection("docs", "", "active", "false"); len(matches) != 1 || matches[0] != "profile" {
		t.Fatalf("unexpected reopened json query matches: %v", matches)
	}

	if err := reopened.DeleteFromCollection("docs", "profile"); err != nil {
		t.Fatalf("delete reopened profile: %v", err)
	}
	if matches := reopened.FindKeysByJSONFieldInCollection("docs", "", "active", "false"); len(matches) != 0 {
		t.Fatalf("expected delete to clear json index, got %v", matches)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened db: %v", err)
	}
}

func TestFindInCommand(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile", `{"profile":{"email":"ada@example.com"},"name":"Ada","active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile: %v", err)
	}

	result, err := db.Execute("FINDIN docs profile.email=ada@example.com active=true")
	if err != nil {
		t.Fatalf("execute FINDIN: %v", err)
	}
	if !strings.Contains(result, "1 matches") || !strings.Contains(result, "profile") {
		t.Fatalf("unexpected FINDIN result: %q", result)
	}
}

func TestFindInCommandWithOperators(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile", `{"profile":{"bio":"Ada builds local databases","score":95},"tags":["admin","builder"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile: %v", err)
	}

	result, err := db.Execute("FINDIN docs profile.score>=90 profile.bio~=local tags=admin")
	if err != nil {
		t.Fatalf("execute operator FINDIN: %v", err)
	}
	if !strings.Contains(result, "1 matches") || !strings.Contains(result, "profile") {
		t.Fatalf("unexpected operator FINDIN result: %q", result)
	}
}

func TestExportCollectionAndAll(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetInCollection("users", "1", "Alice"); err != nil {
		t.Fatalf("set users/1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile", `{"name":"Ada"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile: %v", err)
	}

	usersExport := filepath.Join(dir, "users.jsonl")
	report, err := db.ExportCollection("users", usersExport)
	if err != nil {
		t.Fatalf("export users: %v", err)
	}
	if report.ExportedRecords != 1 {
		t.Fatalf("expected one exported user record, got %d", report.ExportedRecords)
	}

	allExport := filepath.Join(dir, "all.jsonl")
	report, err = db.ExportCollection("all", allExport)
	if err != nil {
		t.Fatalf("export all: %v", err)
	}
	if report.ExportedRecords != 2 {
		t.Fatalf("expected two exported records, got %d", report.ExportedRecords)
	}

	data, err := os.ReadFile(allExport)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if !strings.Contains(string(data), `"collection":"users"`) || !strings.Contains(string(data), `"collection":"docs"`) {
		t.Fatalf("unexpected export contents: %s", string(data))
	}
}

func TestRepairSalvagesValidRecords(t *testing.T) {
	db, dir := openTestDB(t)

	if err := db.SetInCollection("users", "1", "Alice"); err != nil {
		t.Fatalf("set users/1: %v", err)
	}
	if err := db.SetInCollection("users", "2", "Bob"); err != nil {
		t.Fatalf("set users/2: %v", err)
	}

	segmentPath := filepath.Join(dir, "segment-000001.log")
	file, err := os.OpenFile(segmentPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if _, err := file.Write([]byte("BROKEN")); err != nil {
		t.Fatalf("append broken bytes: %v", err)
	}
	file.Close()

	repairDir := filepath.Join(dir, "repaired-db")
	report, err := db.RepairTo(repairDir)
	if err != nil {
		t.Fatalf("repair db: %v", err)
	}
	if report.RecoveredRecords != 2 {
		t.Fatalf("expected two recovered records, got %d", report.RecoveredRecords)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close original db: %v", err)
	}

	repaired, err := engine.OpenInDir(repairDir)
	if err != nil {
		t.Fatalf("open repaired db: %v", err)
	}
	defer repaired.Close()

	value, ok := repaired.GetFromCollection("users", "1")
	if !ok || value != "Alice" {
		t.Fatalf("unexpected repaired users/1 value: %q ok=%v", value, ok)
	}
}

func TestMaintenanceRecommendations(t *testing.T) {
	db, _ := openTestDBWithOptions(t, engine.OpenOptions{SegmentSizeLimit: 220})
	defer db.Close()

	largeValue := strings.Repeat("z", 180)
	for i := 0; i < 12; i++ {
		if err := db.SetInCollection("users", fmt.Sprintf("user:%d", i), largeValue); err != nil {
			t.Fatalf("set maintenance record %d: %v", i, err)
		}
	}

	report, err := db.MaintenanceReport()
	if err != nil {
		t.Fatalf("maintenance report: %v", err)
	}
	if !report.SnapshotRecommended {
		t.Fatal("expected snapshot recommendation")
	}
	if !report.CompactionRecommended {
		t.Fatal("expected compaction recommendation")
	}
	if len(report.Recommendations) == 0 {
		t.Fatal("expected at least one recommendation")
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

	result, err = db.Execute("BATCH\nSETJSON docs profile {\"name\":\"Ada\"}\nSETIN users user:9 Zoe\nEND")
	if err != nil {
		t.Fatalf("execute collection-aware batch command: %v", err)
	}
	if result != "OK" {
		t.Fatalf("unexpected collection-aware batch result: %q", result)
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
