package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const activityHistoryFileName = "activity-history.json"

// ActivityEntry records one user-visible operation so the desktop app can show a durable activity feed.
type ActivityEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
}

// ActivityStore persists recent activity in the MiniDB application data folder.
type ActivityStore struct {
	mu    sync.Mutex
	path  string
	limit int
}

// NewActivityStore creates a small durable history store rooted in the active application data directory.
func NewActivityStore(dataDir string) *ActivityStore {
	return &ActivityStore{
		path:  filepath.Join(dataDir, activityHistoryFileName),
		limit: 250,
	}
}

// Append adds one activity entry and persists the bounded history to disk.
func (s *ActivityStore) Append(entry ActivityEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadLocked()
	if err != nil {
		return err
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	entries = append([]ActivityEntry{entry}, entries...)
	if len(entries) > s.limit {
		entries = entries[:s.limit]
	}

	return s.saveLocked(entries)
}

// ListRecent returns the most recent activity entries first.
func (s *ActivityStore) ListRecent(limit int) ([]ActivityEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	return append([]ActivityEntry(nil), entries...), nil
}

func (s *ActivityStore) loadLocked() ([]ActivityEntry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ActivityEntry{}, nil
		}
		return nil, fmt.Errorf("read activity history: %w", err)
	}

	if len(data) == 0 {
		return []ActivityEntry{}, nil
	}

	var entries []ActivityEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode activity history: %w", err)
	}

	return entries, nil
}

func (s *ActivityStore) saveLocked(entries []ActivityEntry) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create activity history directory: %w", err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode activity history: %w", err)
	}

	tempPath := s.path + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create activity history temp file: %w", err)
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write activity history temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync activity history temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close activity history temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace activity history file: %w", err)
	}

	return nil
}
