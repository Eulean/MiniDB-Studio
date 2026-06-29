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

	studioapp "minidb-studio/internal/app"
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

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestJSONQueryExpressionWithOR(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"score":95},"tags":["admin"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"score":82},"tags":["reviewer"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"score":70},"tags":["guest"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	expression := engine.JSONQueryExpression{
		{
			{Path: "active", Operator: "=", Value: "true"},
		},
		{
			{Path: "profile.score", Operator: ">=", Value: "80"},
			{Path: "tags", Operator: "=", Value: "reviewer"},
		},
	}
	matches := db.FindKeysByJSONExpressionInCollection("docs", "", expression)
	want := []string{"profile:1", "profile:2"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected OR expression matches: got=%v want=%v", matches, want)
	}
}

func TestJSONQueryExpressionWithGroupedPrecedence(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"score":95},"tags":["admin"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"score":88},"tags":["staff"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"score":91},"tags":["staff"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:4", `{"profile":{"score":60},"tags":["guest"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:4: %v", err)
	}

	expression, err := engine.ParseJSONQueryExpression("active=true OR (profile.score>=90 tags=staff)")
	if err != nil {
		t.Fatalf("parse grouped expression: %v", err)
	}

	matches := db.FindKeysByJSONExpressionInCollection("docs", "", expression)
	want := []string{"profile:2", "profile:3", "profile:4"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected grouped-precedence matches: got=%v want=%v", matches, want)
	}
}

func TestJSONQueryExpressionDistributesNestedORAcrossGroup(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"score":95},"tags":["admin"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"score":95},"tags":["reviewer"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"score":70},"tags":["admin"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	expression, err := engine.ParseJSONQueryExpression("(tags=admin OR tags=reviewer) active=true profile.score>=90")
	if err != nil {
		t.Fatalf("parse nested OR expression: %v", err)
	}

	matches := db.FindKeysByJSONExpressionInCollection("docs", "", expression)
	want := []string{"profile:1", "profile:2"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected distributed-group matches: got=%v want=%v", matches, want)
	}
}

func TestJSONQueryExpressionWithNOT(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"active":true,"archived":false,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"active":false,"archived":true,"profile":{"name":"Grace Hopper"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"active":true,"archived":true,"profile":{"name":"Ada Byron"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	expression, err := engine.ParseJSONQueryExpression("NOT archived=true active=true")
	if err != nil {
		t.Fatalf("parse NOT expression: %v", err)
	}

	matches := db.FindKeysByJSONExpressionInCollection("docs", "", expression)
	want := []string{"profile:1"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected NOT matches: got=%v want=%v", matches, want)
	}
}

func TestJSONQueryExpressionWithGroupedNOTAndQuotedValues(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"tags":["admin"],"archived":false,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"tags":["reviewer"],"archived":false,"profile":{"name":"Grace Hopper"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"tags":["reviewer"],"archived":true,"profile":{"name":"Grace Hopper"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	expression, err := engine.ParseJSONQueryExpression("(profile.name=\"Ada Lovelace\" OR profile.name=\"Grace Hopper\") NOT archived=true")
	if err != nil {
		t.Fatalf("parse grouped NOT expression: %v", err)
	}

	matches := db.FindKeysByJSONExpressionInCollection("docs", "", expression)
	want := []string{"profile:1", "profile:2"}
	if strings.Join(matches, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected grouped NOT matches: got=%v want=%v", matches, want)
	}
}

func TestParseJSONQueryExpressionSupportsQuotedValues(t *testing.T) {
	expression, err := engine.ParseJSONQueryExpression(`profile.name="Ada Lovelace" profile.bio~="local database builder"`)
	if err != nil {
		t.Fatalf("parse quoted expression: %v", err)
	}
	if len(expression) != 1 || len(expression[0]) != 2 {
		t.Fatalf("unexpected quoted expression shape: %#v", expression)
	}
	if expression[0][0].Value != "Ada Lovelace" {
		t.Fatalf("unexpected first quoted value: %q", expression[0][0].Value)
	}
	if expression[0][1].Value != "local database builder" {
		t.Fatalf("unexpected second quoted value: %q", expression[0][1].Value)
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

func TestFindInCommandWithOR(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"score":95},"tags":["admin"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"score":82},"tags":["reviewer"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}

	result, err := db.Execute("FINDIN docs active=true OR profile.score>=80 tags=reviewer")
	if err != nil {
		t.Fatalf("execute OR FINDIN: %v", err)
	}
	if !strings.Contains(result, "2 matches") || !strings.Contains(result, "profile:1") || !strings.Contains(result, "profile:2") {
		t.Fatalf("unexpected OR FINDIN result: %q", result)
	}
}

func TestFindInCommandWithGroupedPrecedence(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"profile":{"score":95},"tags":["admin"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"profile":{"score":95},"tags":["staff"],"active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:3", `{"profile":{"score":65},"tags":["guest"],"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:3: %v", err)
	}

	result, err := db.Execute("FINDIN docs active=true OR (profile.score>=90 tags=staff)")
	if err != nil {
		t.Fatalf("execute grouped FINDIN: %v", err)
	}
	if !strings.Contains(result, "2 matches") || !strings.Contains(result, "profile:2") || !strings.Contains(result, "profile:3") {
		t.Fatalf("unexpected grouped FINDIN result: %q", result)
	}
}

func TestFindInCommandWithNOTAndQuotedValues(t *testing.T) {
	db, _ := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"tags":["admin"],"archived":false,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"tags":["admin"],"archived":true,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}

	result, err := db.Execute(`FINDIN docs profile.name="Ada Lovelace" NOT archived=true`)
	if err != nil {
		t.Fatalf("execute NOT FINDIN: %v", err)
	}
	if !strings.Contains(result, "1 matches") || !strings.Contains(result, "profile:1") || strings.Contains(result, "profile:2") {
		t.Fatalf("unexpected NOT quoted FINDIN result: %q", result)
	}
}

func TestParseJSONQueryExpressionRejectsInvalidORSyntax(t *testing.T) {
	_, err := engine.ParseJSONQueryExpression("OR active=true")
	if err == nil {
		t.Fatal("expected invalid leading OR to fail")
	}

	_, err = engine.ParseJSONQueryExpression("active=true OR OR tags=admin")
	if err == nil {
		t.Fatal("expected repeated OR to fail")
	}

	_, err = engine.ParseJSONQueryExpression("active=true OR")
	if err == nil {
		t.Fatal("expected trailing OR to fail")
	}
}

func TestParseJSONQueryExpressionRejectsInvalidParentheses(t *testing.T) {
	cases := []string{
		"active=true OR (tags=admin",
		"active=true )",
		"()",
		"(OR active=true)",
	}

	for _, query := range cases {
		if _, err := engine.ParseJSONQueryExpression(query); err == nil {
			t.Fatalf("expected invalid query %q to fail", query)
		}
	}
}

func TestParseJSONQueryExpressionRejectsInvalidQuoteSyntax(t *testing.T) {
	cases := []string{
		`profile.name="Ada Lovelace`,
		`profile.name="Ada"Lov elace"`,
	}

	for _, query := range cases {
		if _, err := engine.ParseJSONQueryExpression(query); err == nil {
			t.Fatalf("expected invalid quoted query %q to fail", query)
		}
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

func TestExportJSONQueryCollection(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"active":true,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"active":false,"profile":{"name":"Grace Hopper"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}

	destination := filepath.Join(dir, "filtered.jsonl")
	report, err := db.ExportJSONQueryCollection("docs", "active=true", destination)
	if err != nil {
		t.Fatalf("export filtered collection: %v", err)
	}
	if report.ExportedRecords != 1 || report.QueryText != "active=true" {
		t.Fatalf("unexpected filtered export report: %#v", report)
	}

	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read filtered export: %v", err)
	}
	if !strings.Contains(string(data), `"key":"profile:1"`) || strings.Contains(string(data), `"key":"profile:2"`) {
		t.Fatalf("unexpected filtered export contents: %s", string(data))
	}
}

func TestImportNDJSONSuccessAndCustomKeyField(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "docs.ndjson")
	content := strings.Join([]string{
		`{"doc_id":"alpha","name":"Ada Lovelace","active":true}`,
		`{"doc_id":"beta","name":"Grace Hopper","active":false}`,
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write ndjson: %v", err)
	}

	report, err := db.ImportNDJSON("docs", sourcePath, "doc_id", engine.ImportConflictSkip, false)
	if err != nil {
		t.Fatalf("import ndjson: %v", err)
	}
	if report.ImportedRecords != 2 || report.SkippedRecords != 0 {
		t.Fatalf("unexpected import report: %#v", report)
	}

	value, ok := db.GetFromCollection("docs", "alpha")
	if !ok || !strings.Contains(value, `"name":"Ada Lovelace"`) {
		t.Fatalf("unexpected imported alpha value: value=%q ok=%v", value, ok)
	}
}

func TestImportNDJSONSkipMode(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "skip.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Incoming"}`), 0o644); err != nil {
		t.Fatalf("write skip ndjson: %v", err)
	}

	report, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictSkip, false)
	if err != nil {
		t.Fatalf("import skip mode: %v", err)
	}
	if report.ImportedRecords != 0 || report.SkippedRecords != 1 {
		t.Fatalf("unexpected skip report: %#v", report)
	}

	value, ok := db.GetFromCollection("docs", "alpha")
	if !ok || !strings.Contains(value, `"name":"Existing"`) {
		t.Fatalf("skip mode should preserve existing value: value=%q ok=%v", value, ok)
	}
}

func TestImportNDJSONOverwriteMode(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "overwrite.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Incoming"}`), 0o644); err != nil {
		t.Fatalf("write overwrite ndjson: %v", err)
	}

	report, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictOverwrite, false)
	if err != nil {
		t.Fatalf("import overwrite mode: %v", err)
	}
	if report.ImportedRecords != 1 || report.SkippedRecords != 0 {
		t.Fatalf("unexpected overwrite report: %#v", report)
	}

	value, ok := db.GetFromCollection("docs", "alpha")
	if !ok || !strings.Contains(value, `"name":"Incoming"`) {
		t.Fatalf("overwrite mode should replace existing value: value=%q ok=%v", value, ok)
	}
}

func TestImportNDJSONRejectsInvalidJSONLine(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "invalid.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha"`), 0o644); err != nil {
		t.Fatalf("write invalid ndjson: %v", err)
	}

	if _, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictSkip, false); err == nil {
		t.Fatal("expected invalid json line to fail")
	}
}

func TestImportNDJSONRejectsMissingKeyField(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "missing-key.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"name":"Ada"}`), 0o644); err != nil {
		t.Fatalf("write missing-key ndjson: %v", err)
	}

	if _, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictSkip, false); err == nil {
		t.Fatal("expected missing key field to fail")
	}
}

func TestPreviewNDJSONImportReportsDuplicatesAndConflicts(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "preview.ndjson")
	content := strings.Join([]string{
		`{"id":"alpha","name":"Incoming Existing"}`,
		`{"id":"beta","name":"New Doc"}`,
		`{"id":"beta","name":"Duplicate In File"}`,
		`{"name":"Missing Key"}`,
		`{"id":"gamma"`,
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write preview ndjson: %v", err)
	}

	report, err := db.PreviewNDJSONImport("docs", sourcePath, "id", engine.ImportConflictSkip)
	if err != nil {
		t.Fatalf("preview ndjson: %v", err)
	}
	if report.TotalLines != 5 {
		t.Fatalf("unexpected total lines: %d", report.TotalLines)
	}
	if report.ValidDocuments != 3 {
		t.Fatalf("unexpected valid documents: %d", report.ValidDocuments)
	}
	if report.InvalidLines != 1 {
		t.Fatalf("unexpected invalid lines: %d", report.InvalidLines)
	}
	if report.MissingKeyCount != 1 {
		t.Fatalf("unexpected missing key count: %d", report.MissingKeyCount)
	}
	if report.DuplicateKeysInFile != 1 {
		t.Fatalf("unexpected duplicate key count: %d", report.DuplicateKeysInFile)
	}
	if report.ExistingKeyConflicts != 1 {
		t.Fatalf("unexpected existing-key conflicts: %d", report.ExistingKeyConflicts)
	}
	if report.NewRecordCount != 2 || report.SkipCount != 1 || report.OverwriteCount != 0 {
		t.Fatalf("unexpected change classification counts: new=%d skip=%d overwrite=%d", report.NewRecordCount, report.SkipCount, report.OverwriteCount)
	}
	if report.TotalDistinctFields == 0 {
		t.Fatal("expected schema field summaries")
	}
	var foundNameField bool
	for _, field := range report.FieldSummaries {
		if field.Path == "name" {
			foundNameField = true
			if strings.Join(field.Types, ",") != "string" {
				t.Fatalf("unexpected name field types: %v", field.Types)
			}
			break
		}
	}
	if !foundNameField {
		t.Fatalf("expected name field summary, got %#v", report.FieldSummaries)
	}
	var foundSkipSample bool
	for _, sample := range report.ChangeSamples {
		if sample.Key == "alpha" && sample.Status == "skip" {
			foundSkipSample = true
			if sample.CurrentPreview == "" || sample.IncomingPreview == "" {
				t.Fatalf("expected skip sample previews, got %#v", sample)
			}
			break
		}
	}
	if !foundSkipSample {
		t.Fatalf("expected skip sample in preview, got %#v", report.ChangeSamples)
	}
}

func TestPreviewNDJSONImportReportsOverwriteSamples(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing","active":false}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "overwrite-preview.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Incoming","active":true}`), 0o644); err != nil {
		t.Fatalf("write overwrite-preview ndjson: %v", err)
	}

	report, err := db.PreviewNDJSONImport("docs", sourcePath, "id", engine.ImportConflictOverwrite)
	if err != nil {
		t.Fatalf("preview overwrite ndjson: %v", err)
	}
	if report.NewRecordCount != 0 || report.SkipCount != 0 || report.OverwriteCount != 1 {
		t.Fatalf("unexpected overwrite preview counts: %#v", report)
	}
	if len(report.ChangeSamples) != 1 || report.ChangeSamples[0].Status != "overwrite" {
		t.Fatalf("expected one overwrite change sample, got %#v", report.ChangeSamples)
	}
	if report.ChangeSamples[0].CurrentPreview == report.ChangeSamples[0].IncomingPreview {
		t.Fatalf("expected current and incoming previews to differ, got %#v", report.ChangeSamples[0])
	}
	if !sliceContains(report.ChangeSamples[0].ChangedFields, "active") || !sliceContains(report.ChangeSamples[0].ChangedFields, "name") {
		t.Fatalf("expected changed field paths in overwrite sample, got %#v", report.ChangeSamples[0])
	}
}

func TestPreviewNDJSONImportReportsAddedRemovedAndNestedFields(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing","legacy":"yes","profile":{"email":"old@example.com","score":1}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "field-diff.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Incoming","profile":{"email":"new@example.com","score":1},"active":true}`), 0o644); err != nil {
		t.Fatalf("write field-diff ndjson: %v", err)
	}

	report, err := db.PreviewNDJSONImport("docs", sourcePath, "id", engine.ImportConflictOverwrite)
	if err != nil {
		t.Fatalf("preview field-diff ndjson: %v", err)
	}
	if len(report.ChangeSamples) != 1 {
		t.Fatalf("expected one change sample, got %#v", report.ChangeSamples)
	}

	sample := report.ChangeSamples[0]
	if !sliceContains(sample.AddedFields, "active") {
		t.Fatalf("expected added field path, got %#v", sample)
	}
	if !sliceContains(sample.RemovedFields, "legacy") {
		t.Fatalf("expected removed field path, got %#v", sample)
	}
	if !sliceContains(sample.ChangedFields, "name") {
		t.Fatalf("expected changed scalar field path, got %#v", sample)
	}
	if !sliceContains(sample.ChangedFields, "profile.email") {
		t.Fatalf("expected nested changed field path, got %#v", sample)
	}
}

func TestImportNDJSONDryRunLeavesDatabaseUnchanged(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "dry-run.ndjson")
	content := strings.Join([]string{
		`{"id":"alpha","name":"Ada"}`,
		`{"id":"beta","name":"Grace"}`,
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dry-run ndjson: %v", err)
	}

	report, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictSkip, true)
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if !report.DryRun || report.ImportedRecords != 2 {
		t.Fatalf("unexpected dry-run report: %#v", report)
	}
	if _, ok := db.GetFromCollection("docs", "alpha"); ok {
		t.Fatal("dry-run import should not persist records")
	}
}

func TestImportNDJSONDryRunMatchesPreviewCounts(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "alpha", `{"id":"alpha","name":"Existing"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed existing record: %v", err)
	}

	sourcePath := filepath.Join(dir, "dry-run-match.ndjson")
	content := strings.Join([]string{
		`{"id":"alpha","name":"Incoming Existing"}`,
		`{"id":"beta","name":"New Doc"}`,
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dry-run-match ndjson: %v", err)
	}

	preview, err := db.PreviewNDJSONImport("docs", sourcePath, "id", engine.ImportConflictSkip)
	if err != nil {
		t.Fatalf("preview dry-run-match ndjson: %v", err)
	}
	report, err := db.ImportNDJSON("docs", sourcePath, "id", engine.ImportConflictSkip, true)
	if err != nil {
		t.Fatalf("dry-run import match: %v", err)
	}
	if preview.NewRecordCount != report.ImportedRecords || preview.SkipCount != report.SkippedRecords {
		t.Fatalf("dry-run should match preview counts: preview=%#v report=%#v", preview, report)
	}
}

func TestPreviewNDJSONCommandWithQuotedPath(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "preview folder", "docs.ndjson")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir preview dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Ada Lovelace"}`), 0o644); err != nil {
		t.Fatalf("write preview source: %v", err)
	}

	result, err := db.Execute(fmt.Sprintf(`PREVIEWNDJSON docs "%s"`, sourcePath))
	if err != nil {
		t.Fatalf("execute PREVIEWNDJSON: %v", err)
	}
	if !strings.Contains(result, "total_lines=1") || !strings.Contains(result, "valid_documents=1") {
		t.Fatalf("unexpected PREVIEWNDJSON result: %q", result)
	}
}

func TestImportNDJSONDryRunCommandLeavesDatabaseUnchanged(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "dry run command.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Ada Lovelace"}`), 0o644); err != nil {
		t.Fatalf("write dry-run command source: %v", err)
	}

	result, err := db.Execute(fmt.Sprintf(`IMPORTNDJSON docs "%s" id overwrite dry-run`, sourcePath))
	if err != nil {
		t.Fatalf("execute IMPORTNDJSON dry-run: %v", err)
	}
	if !strings.Contains(result, "dry_run=true") || !strings.Contains(result, "imported_records=1") {
		t.Fatalf("unexpected IMPORTNDJSON dry-run result: %q", result)
	}
	if _, ok := db.GetFromCollection("docs", "alpha"); ok {
		t.Fatal("dry-run command should not persist records")
	}
}

func TestExportQueryCommandWithQuotedArguments(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	if err := db.SetTypedInCollection("docs", "profile:1", `{"active":true,"profile":{"name":"Ada Lovelace"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "profile:2", `{"active":false,"profile":{"name":"Grace Hopper"}}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("set docs/profile:2: %v", err)
	}

	destination := filepath.Join(dir, "filtered export folder", "docs.jsonl")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("mkdir export destination: %v", err)
	}

	result, err := db.Execute(fmt.Sprintf(`EXPORTQUERY docs "active=true" "%s"`, destination))
	if err != nil {
		t.Fatalf("execute EXPORTQUERY: %v", err)
	}
	if !strings.Contains(result, "exported_records=1") || !strings.Contains(result, "query=active=true") {
		t.Fatalf("unexpected EXPORTQUERY result: %q", result)
	}
}

func TestDatasetPresetStoreSaveLoadOverwriteAndDelete(t *testing.T) {
	store := studioapp.NewDatasetPresetStore(t.TempDir())

	if err := store.Save(studioapp.DatasetPreset{
		Name:         "Docs Import",
		Collection:   "docs",
		KeyField:     "id",
		ConflictMode: "overwrite",
		QueryText:    "active=true",
	}); err != nil {
		t.Fatalf("save preset: %v", err)
	}

	presets, err := store.List()
	if err != nil {
		t.Fatalf("list presets: %v", err)
	}
	if len(presets) != 1 || presets[0].Collection != "docs" {
		t.Fatalf("unexpected saved presets: %#v", presets)
	}

	if err := store.Save(studioapp.DatasetPreset{
		Name:         "Docs Import",
		Collection:   "docs-v2",
		KeyField:     "doc_id",
		ConflictMode: "skip",
		QueryText:    "active=false",
	}); err != nil {
		t.Fatalf("overwrite preset: %v", err)
	}

	presets, err = store.List()
	if err != nil {
		t.Fatalf("list overwritten presets: %v", err)
	}
	if len(presets) != 1 || presets[0].Collection != "docs-v2" || presets[0].KeyField != "doc_id" {
		t.Fatalf("unexpected overwritten preset values: %#v", presets)
	}

	if err := store.Delete("Docs Import"); err != nil {
		t.Fatalf("delete preset: %v", err)
	}

	presets, err = store.List()
	if err != nil {
		t.Fatalf("list presets after delete: %v", err)
	}
	if len(presets) != 0 {
		t.Fatalf("expected deleted presets to be empty, got %#v", presets)
	}
}

func TestDatasetPresetStoreRejectsEmptyName(t *testing.T) {
	store := studioapp.NewDatasetPresetStore(t.TempDir())
	if err := store.Save(studioapp.DatasetPreset{Collection: "docs"}); err == nil {
		t.Fatal("expected empty preset name to fail")
	}
}

func TestPresetAwarePreviewImportAndExportCommands(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if err := application.SaveDatasetPreset(studioapp.DatasetPreset{
		Name:         "Docs Workflow",
		Collection:   "docs",
		KeyField:     "id",
		ConflictMode: "overwrite",
		QueryText:    "active=true",
	}); err != nil {
		t.Fatalf("save dataset preset: %v", err)
	}

	sourcePath := filepath.Join(dir, "preset-source.ndjson")
	content := strings.Join([]string{
		`{"id":"alpha","name":"Ada Lovelace","active":true}`,
		`{"id":"beta","name":"Grace Hopper","active":false}`,
	}, "\n")
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write preset source: %v", err)
	}

	previewResult, err := application.ExecuteCommand(fmt.Sprintf(`PREVIEWPRESET "Docs Workflow" "%s"`, sourcePath))
	if err != nil {
		t.Fatalf("execute PREVIEWPRESET: %v", err)
	}
	if !strings.Contains(previewResult, "preset=Docs Workflow") || !strings.Contains(previewResult, "valid_documents=2") {
		t.Fatalf("unexpected PREVIEWPRESET result: %q", previewResult)
	}

	dryRunResult, err := application.ExecuteCommand(fmt.Sprintf(`IMPORTPRESET "Docs Workflow" "%s" dry-run`, sourcePath))
	if err != nil {
		t.Fatalf("execute IMPORTPRESET dry-run: %v", err)
	}
	if !strings.Contains(dryRunResult, "dry_run=true") || !strings.Contains(dryRunResult, "imported_records=2") {
		t.Fatalf("unexpected IMPORTPRESET dry-run result: %q", dryRunResult)
	}
	if _, ok := db.GetFromCollection("docs", "alpha"); ok {
		t.Fatal("dry-run preset import should not persist records")
	}

	importResult, err := application.ExecuteCommand(fmt.Sprintf(`IMPORTPRESET "Docs Workflow" "%s"`, sourcePath))
	if err != nil {
		t.Fatalf("execute IMPORTPRESET: %v", err)
	}
	if !strings.Contains(importResult, "dry_run=false") || !strings.Contains(importResult, "imported_records=2") {
		t.Fatalf("unexpected IMPORTPRESET result: %q", importResult)
	}

	exportPath := filepath.Join(dir, "preset-export.jsonl")
	exportResult, err := application.ExecuteCommand(fmt.Sprintf(`EXPORTPRESET "Docs Workflow" "%s"`, exportPath))
	if err != nil {
		t.Fatalf("execute EXPORTPRESET: %v", err)
	}
	if !strings.Contains(exportResult, "preset=Docs Workflow") || !strings.Contains(exportResult, "exported_records=1") {
		t.Fatalf("unexpected EXPORTPRESET result: %q", exportResult)
	}
}

func TestPresetAwareListAndShowCommands(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if err := application.SaveDatasetPreset(studioapp.DatasetPreset{
		Name:         "Docs Workflow",
		Collection:   "docs",
		KeyField:     "id",
		ConflictMode: "overwrite",
		QueryText:    "active=true",
	}); err != nil {
		t.Fatalf("save docs preset: %v", err)
	}

	if err := application.SaveDatasetPreset(studioapp.DatasetPreset{
		Name:         "Users Import",
		Collection:   "users",
		KeyField:     "user_id",
		ConflictMode: "skip",
	}); err != nil {
		t.Fatalf("save users preset: %v", err)
	}

	listResult, err := application.ExecuteCommand("LISTPRESETS")
	if err != nil {
		t.Fatalf("execute LISTPRESETS: %v", err)
	}
	if !strings.Contains(listResult, "preset_count=2") || !strings.Contains(listResult, "preset=Docs Workflow") || !strings.Contains(listResult, "preset=Users Import") {
		t.Fatalf("unexpected LISTPRESETS result: %q", listResult)
	}

	showResult, err := application.ExecuteCommand(`SHOWPRESET "Docs Workflow"`)
	if err != nil {
		t.Fatalf("execute SHOWPRESET: %v", err)
	}
	if !strings.Contains(showResult, "name=Docs Workflow") || !strings.Contains(showResult, "collection=docs") || !strings.Contains(showResult, "key_field=id") || !strings.Contains(showResult, "conflict_mode=overwrite") || !strings.Contains(showResult, "query=active=true") {
		t.Fatalf("unexpected SHOWPRESET result: %q", showResult)
	}
}

func TestPresetAwareSaveAndDeleteCommands(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	saveResult, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id overwrite "active=true"`)
	if err != nil {
		t.Fatalf("execute SAVEPRESET: %v", err)
	}
	if !strings.Contains(saveResult, "saved_preset=Docs Workflow") || !strings.Contains(saveResult, "collection=docs") || !strings.Contains(saveResult, "key_field=id") || !strings.Contains(saveResult, "conflict_mode=overwrite") || !strings.Contains(saveResult, "query=active=true") {
		t.Fatalf("unexpected SAVEPRESET result: %q", saveResult)
	}

	showResult, err := application.ExecuteCommand(`SHOWPRESET "Docs Workflow"`)
	if err != nil {
		t.Fatalf("show saved preset: %v", err)
	}
	if !strings.Contains(showResult, "name=Docs Workflow") {
		t.Fatalf("expected saved preset to exist, got %q", showResult)
	}

	overwriteResult, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs doc_id skip`)
	if err != nil {
		t.Fatalf("overwrite SAVEPRESET: %v", err)
	}
	if !strings.Contains(overwriteResult, "key_field=doc_id") || !strings.Contains(overwriteResult, "conflict_mode=skip") {
		t.Fatalf("unexpected overwritten SAVEPRESET result: %q", overwriteResult)
	}

	deleteResult, err := application.ExecuteCommand(`DELETEPRESET "Docs Workflow"`)
	if err != nil {
		t.Fatalf("execute DELETEPRESET: %v", err)
	}
	if deleteResult != "deleted_preset=Docs Workflow" {
		t.Fatalf("unexpected DELETEPRESET result: %q", deleteResult)
	}

	if _, err := application.ExecuteCommand(`SHOWPRESET "Docs Workflow"`); err == nil {
		t.Fatal("expected deleted preset to be missing")
	}
}

func TestDatasetPresetStoreRename(t *testing.T) {
	store := studioapp.NewDatasetPresetStore(t.TempDir())

	if err := store.Save(studioapp.DatasetPreset{
		Name:         "Docs Workflow",
		Collection:   "docs",
		KeyField:     "id",
		ConflictMode: "overwrite",
		QueryText:    "active=true",
	}); err != nil {
		t.Fatalf("save original preset: %v", err)
	}

	if err := store.Rename("Docs Workflow", "Docs Archive"); err != nil {
		t.Fatalf("rename preset: %v", err)
	}

	if _, err := store.Find("Docs Workflow"); err == nil {
		t.Fatal("expected old preset name to be gone after rename")
	}

	renamed, err := store.Find("Docs Archive")
	if err != nil {
		t.Fatalf("find renamed preset: %v", err)
	}
	if renamed.Collection != "docs" || renamed.KeyField != "id" || renamed.ConflictMode != "overwrite" || renamed.QueryText != "active=true" {
		t.Fatalf("unexpected renamed preset content: %#v", renamed)
	}
}

func TestDatasetPresetStoreRenameRejectsConflict(t *testing.T) {
	store := studioapp.NewDatasetPresetStore(t.TempDir())

	for _, preset := range []studioapp.DatasetPreset{
		{Name: "Docs Workflow", Collection: "docs", KeyField: "id", ConflictMode: "overwrite"},
		{Name: "Users Workflow", Collection: "users", KeyField: "user_id", ConflictMode: "skip"},
	} {
		if err := store.Save(preset); err != nil {
			t.Fatalf("save preset %q: %v", preset.Name, err)
		}
	}

	if err := store.Rename("Docs Workflow", "Users Workflow"); err == nil {
		t.Fatal("expected rename conflict to fail")
	}
}

func TestDatasetPresetStoreDuplicateExportAndImport(t *testing.T) {
	storeDir := t.TempDir()
	store := studioapp.NewDatasetPresetStore(storeDir)

	original := studioapp.DatasetPreset{
		Name:         "Docs Workflow",
		Collection:   "docs",
		KeyField:     "id",
		ConflictMode: "overwrite",
		QueryText:    "active=true",
	}
	if err := store.Save(original); err != nil {
		t.Fatalf("save original preset: %v", err)
	}

	if err := store.Duplicate("Docs Workflow", "Docs Copy"); err != nil {
		t.Fatalf("duplicate preset: %v", err)
	}

	duplicate, err := store.Find("Docs Copy")
	if err != nil {
		t.Fatalf("find duplicate preset: %v", err)
	}
	if duplicate.Collection != original.Collection || duplicate.KeyField != original.KeyField || duplicate.QueryText != original.QueryText {
		t.Fatalf("unexpected duplicate preset: %#v", duplicate)
	}

	exportPath := filepath.Join(storeDir, "exports", "docs-copy-preset.json")
	exported, err := store.Export("Docs Copy", exportPath)
	if err != nil {
		t.Fatalf("export preset: %v", err)
	}
	if exported.Name != "Docs Copy" {
		t.Fatalf("unexpected exported preset: %#v", exported)
	}

	exportedData, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported preset file: %v", err)
	}

	var exportedPreset studioapp.DatasetPreset
	if err := json.Unmarshal(exportedData, &exportedPreset); err != nil {
		t.Fatalf("decode exported preset file: %v", err)
	}
	if exportedPreset.Name != "Docs Copy" || exportedPreset.Collection != "docs" {
		t.Fatalf("unexpected exported preset file contents: %#v", exportedPreset)
	}

	if err := store.Delete("Docs Copy"); err != nil {
		t.Fatalf("delete exported preset before import: %v", err)
	}

	imported, err := store.Import(exportPath)
	if err != nil {
		t.Fatalf("import preset file: %v", err)
	}
	if imported.Name != "Docs Copy" || imported.KeyField != "id" || imported.ConflictMode != "overwrite" {
		t.Fatalf("unexpected imported preset: %#v", imported)
	}
}

func TestPresetAwareRenameCommand(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id overwrite "active=true"`); err != nil {
		t.Fatalf("seed SAVEPRESET: %v", err)
	}

	renameResult, err := application.ExecuteCommand(`RENAMEDPRESET "Docs Workflow" "Docs Archive"`)
	if err != nil {
		t.Fatalf("execute RENAMEDPRESET: %v", err)
	}
	if !strings.Contains(renameResult, "renamed_preset=Docs Workflow") || !strings.Contains(renameResult, "new_name=Docs Archive") {
		t.Fatalf("unexpected RENAMEDPRESET result: %q", renameResult)
	}

	if _, err := application.ExecuteCommand(`SHOWPRESET "Docs Workflow"`); err == nil {
		t.Fatal("expected old preset name to be missing after rename")
	}

	showResult, err := application.ExecuteCommand(`SHOWPRESET "Docs Archive"`)
	if err != nil {
		t.Fatalf("show renamed preset: %v", err)
	}
	if !strings.Contains(showResult, "name=Docs Archive") || !strings.Contains(showResult, "query=active=true") {
		t.Fatalf("unexpected renamed preset details: %q", showResult)
	}
}

func TestPresetAwareDuplicateExportAndImportCommands(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id overwrite "active=true"`); err != nil {
		t.Fatalf("seed docs workflow preset: %v", err)
	}

	duplicateResult, err := application.ExecuteCommand(`DUPLICATEPRESET "Docs Workflow" "Docs Copy"`)
	if err != nil {
		t.Fatalf("execute DUPLICATEPRESET: %v", err)
	}
	if !strings.Contains(duplicateResult, "duplicated_preset=Docs Workflow") || !strings.Contains(duplicateResult, "new_name=Docs Copy") {
		t.Fatalf("unexpected DUPLICATEPRESET result: %q", duplicateResult)
	}

	exportPath := filepath.Join(dir, "preset exports", "docs-copy.json")
	exportResult, err := application.ExecuteCommand(fmt.Sprintf(`EXPORTPRESETCONFIG "Docs Copy" "%s"`, exportPath))
	if err != nil {
		t.Fatalf("execute EXPORTPRESETCONFIG: %v", err)
	}
	if !strings.Contains(exportResult, "exported_preset=Docs Copy") || !strings.Contains(exportResult, "destination="+exportPath) {
		t.Fatalf("unexpected EXPORTPRESETCONFIG result: %q", exportResult)
	}

	if _, err := application.ExecuteCommand(`DELETEPRESET "Docs Copy"`); err != nil {
		t.Fatalf("delete preset before import: %v", err)
	}

	importResult, err := application.ExecuteCommand(fmt.Sprintf(`IMPORTPRESETCONFIG "%s"`, exportPath))
	if err != nil {
		t.Fatalf("execute IMPORTPRESETCONFIG: %v", err)
	}
	if !strings.Contains(importResult, "imported_preset=Docs Copy") || !strings.Contains(importResult, "source="+exportPath) {
		t.Fatalf("unexpected IMPORTPRESETCONFIG result: %q", importResult)
	}

	showResult, err := application.ExecuteCommand(`SHOWPRESET "Docs Copy"`)
	if err != nil {
		t.Fatalf("show imported preset: %v", err)
	}
	if !strings.Contains(showResult, "name=Docs Copy") || !strings.Contains(showResult, "query=active=true") {
		t.Fatalf("unexpected imported preset details: %q", showResult)
	}
}

func TestPresetAwareCommandMissingPresetFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	sourcePath := filepath.Join(dir, "missing-preset.ndjson")
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Ada"}`), 0o644); err != nil {
		t.Fatalf("write missing-preset source: %v", err)
	}

	if _, err := application.ExecuteCommand(fmt.Sprintf(`PREVIEWPRESET "Missing Preset" "%s"`, sourcePath)); err == nil {
		t.Fatal("expected missing preset command to fail")
	}
}

func TestShowPresetCommandMissingPresetFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SHOWPRESET "Missing Preset"`); err == nil {
		t.Fatal("expected SHOWPRESET missing preset to fail")
	}
}

func TestRenamePresetCommandMissingPresetFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`RENAMEDPRESET "Missing Preset" "Docs Archive"`); err == nil {
		t.Fatal("expected RENAMEDPRESET missing preset to fail")
	}
}

func TestRenamePresetCommandConflictFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id overwrite`); err != nil {
		t.Fatalf("seed docs preset: %v", err)
	}
	if _, err := application.ExecuteCommand(`SAVEPRESET "Users Workflow" users user_id skip`); err != nil {
		t.Fatalf("seed users preset: %v", err)
	}

	if _, err := application.ExecuteCommand(`RENAMEDPRESET "Docs Workflow" "Users Workflow"`); err == nil {
		t.Fatal("expected rename conflict to fail")
	}
}

func TestDuplicatePresetCommandConflictFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id overwrite`); err != nil {
		t.Fatalf("seed docs preset: %v", err)
	}
	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Copy" docs id overwrite`); err != nil {
		t.Fatalf("seed docs copy preset: %v", err)
	}

	if _, err := application.ExecuteCommand(`DUPLICATEPRESET "Docs Workflow" "Docs Copy"`); err == nil {
		t.Fatal("expected duplicate conflict to fail")
	}
}

func TestImportPresetConfigCommandInvalidFileFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	invalidPath := filepath.Join(dir, "invalid-preset.json")
	if err := os.WriteFile(invalidPath, []byte(`{"name":"Broken"}`), 0o644); err != nil {
		t.Fatalf("write invalid preset file: %v", err)
	}

	if _, err := application.ExecuteCommand(fmt.Sprintf(`IMPORTPRESETCONFIG "%s"`, invalidPath)); err == nil {
		t.Fatal("expected invalid preset file to fail")
	}
}

func TestSavePresetCommandValidationFails(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if _, err := application.ExecuteCommand(`SAVEPRESET "Docs Workflow" docs id merge`); err == nil {
		t.Fatal("expected invalid conflict mode to fail")
	}
}

func TestImportNDJSONCommandWithQuotedPath(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	sourcePath := filepath.Join(dir, "folder with spaces", "docs.ndjson")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir import dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(`{"id":"alpha","name":"Ada Lovelace"}`), 0o644); err != nil {
		t.Fatalf("write quoted-path ndjson: %v", err)
	}

	result, err := db.Execute(fmt.Sprintf(`IMPORTNDJSON docs "%s"`, sourcePath))
	if err != nil {
		t.Fatalf("execute IMPORTNDJSON: %v", err)
	}
	if !strings.Contains(result, "imported_records=1") || !strings.Contains(result, "skipped_records=0") {
		t.Fatalf("unexpected IMPORTNDJSON result: %q", result)
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

func TestActivityStorePersistsRecentEntries(t *testing.T) {
	store := studioapp.NewActivityStore(t.TempDir())

	if err := store.Append(studioapp.ActivityEntry{
		Action: "Import NDJSON",
		Target: "docs -> sample.ndjson",
		Status: "success",
		Detail: "Imported 12 records",
	}); err != nil {
		t.Fatalf("append first activity: %v", err)
	}

	if err := store.Append(studioapp.ActivityEntry{
		Action: "Compact database",
		Target: "database",
		Status: "success",
		Detail: "Compaction completed",
	}); err != nil {
		t.Fatalf("append second activity: %v", err)
	}

	entries, err := store.ListRecent(10)
	if err != nil {
		t.Fatalf("list activities: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(entries))
	}
	if entries[0].Action != "Compact database" {
		t.Fatalf("expected most recent activity first, got %#v", entries[0])
	}
}

func TestApplicationCollectionProfilesSummarizeCollections(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if err := db.SetTypedInCollection("docs", "doc:1", `{"active":true}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed json record: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "doc:2", "plain text", engine.ValueKindRaw); err != nil {
		t.Fatalf("seed raw record: %v", err)
	}
	if err := db.SetTypedInCollection("users", "user:1", `{"name":"Ada"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed second collection: %v", err)
	}

	profiles, err := application.CollectionProfiles()
	if err != nil {
		t.Fatalf("collection profiles: %v", err)
	}
	if len(profiles) < 2 {
		t.Fatalf("expected at least 2 collection profiles, got %#v", profiles)
	}

	var docsProfile studioapp.CollectionProfile
	found := false
	for _, profile := range profiles {
		if profile.Name == "docs" {
			docsProfile = profile
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected docs profile in %#v", profiles)
	}
	if docsProfile.RecordCount != 2 || docsProfile.JSONRecordCount != 1 || docsProfile.RawRecordCount != 1 {
		t.Fatalf("unexpected docs profile: %#v", docsProfile)
	}
}

func TestApplicationExecuteCommandSupportsMiniSQL(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if err := db.SetTypedInCollection("docs", "doc:1", `{"active":true,"name":"Ada"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed doc 1: %v", err)
	}
	if err := db.SetTypedInCollection("docs", "doc:2", `{"active":false,"name":"Grace"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed doc 2: %v", err)
	}

	result, err := application.ExecuteCommand(`SELECT key, value_preview FROM docs WHERE active=true LIMIT 5`)
	if err != nil {
		t.Fatalf("execute SQL select: %v", err)
	}
	if !strings.Contains(result, "key\tvalue_preview") {
		t.Fatalf("unexpected SQL header: %q", result)
	}
	if !strings.Contains(result, "doc:1") {
		t.Fatalf("expected matching row in SQL result: %q", result)
	}
	if strings.Contains(result, "doc:2") {
		t.Fatalf("expected SQL filter to exclude doc:2: %q", result)
	}
	if !strings.Contains(result, "1 row(s)") {
		t.Fatalf("expected row count in SQL result: %q", result)
	}
}

func TestQueryStoreSaveRenameAndDelete(t *testing.T) {
	store := studioapp.NewQueryStore(t.TempDir())

	if err := store.Save(studioapp.SavedQuery{
		Name:      "Active Docs",
		QueryText: `SELECT key FROM docs WHERE active=true LIMIT 10`,
		Notes:     "Main active documents query",
	}); err != nil {
		t.Fatalf("save query: %v", err)
	}

	queries, err := store.List()
	if err != nil {
		t.Fatalf("list queries: %v", err)
	}
	if len(queries) != 1 || queries[0].Name != "Active Docs" {
		t.Fatalf("unexpected saved queries: %#v", queries)
	}

	if err := store.Rename("Active Docs", "Open Docs"); err != nil {
		t.Fatalf("rename query: %v", err)
	}
	query, err := store.Find("Open Docs")
	if err != nil {
		t.Fatalf("find renamed query: %v", err)
	}
	if !strings.Contains(query.QueryText, "SELECT key FROM docs") {
		t.Fatalf("unexpected renamed query text: %#v", query)
	}

	if err := store.Delete("Open Docs"); err != nil {
		t.Fatalf("delete query: %v", err)
	}
	remaining, err := store.List()
	if err != nil {
		t.Fatalf("list queries after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected query store to be empty, got %#v", remaining)
	}
}

func TestMiniSQLCountOrderAndExport(t *testing.T) {
	db, dir := openTestDB(t)
	defer db.Close()

	application := studioapp.NewApplicationForTests(db, studioapp.NewDatasetPresetStore(dir))

	if err := db.SetTypedInCollection("docs", "doc:1", `{"active":true,"name":"Ada"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed doc 1: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := db.SetTypedInCollection("docs", "doc:2", `{"active":true,"name":"Grace"}`, engine.ValueKindJSON); err != nil {
		t.Fatalf("seed doc 2: %v", err)
	}

	countResult, err := application.ExecuteCommand(`SELECT COUNT(*) FROM docs WHERE active=true`)
	if err != nil {
		t.Fatalf("execute SQL count: %v", err)
	}
	if !strings.Contains(countResult, "count") || !strings.Contains(countResult, "\n2\n") {
		t.Fatalf("unexpected count result: %q", countResult)
	}

	orderResult, err := application.ExecuteCommand(`SELECT key FROM docs ORDER BY updated_at DESC LIMIT 2`)
	if err != nil {
		t.Fatalf("execute ordered SQL query: %v", err)
	}
	if strings.Index(orderResult, "doc:2") > strings.Index(orderResult, "doc:1") {
		t.Fatalf("expected doc:2 before doc:1 in descending updated_at order: %q", orderResult)
	}

	exportPath := filepath.Join(dir, "query-result.tsv")
	if err := application.ExportMiniSQLResult(`SELECT key, value_kind FROM docs ORDER BY key ASC LIMIT 2`, exportPath); err != nil {
		t.Fatalf("export SQL result: %v", err)
	}
	exportedData, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported SQL result: %v", err)
	}
	exportedText := string(exportedData)
	if !strings.Contains(exportedText, "key\tvalue_kind") || !strings.Contains(exportedText, "doc:1\tjson") {
		t.Fatalf("unexpected exported SQL result: %q", exportedText)
	}
}
