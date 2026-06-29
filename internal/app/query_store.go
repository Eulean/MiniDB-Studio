package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const savedQueryFileName = "saved-queries.json"

// SavedQuery stores one reusable read-only SQL query for the Query Studio page.
type SavedQuery struct {
	Name      string    `json:"name"`
	QueryText string    `json:"query_text"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// QueryStore persists reusable SQL snippets beside the MiniDB workspace.
type QueryStore struct {
	mu   sync.Mutex
	path string
}

// NewQueryStore creates a durable query store rooted in the active application data directory.
func NewQueryStore(dataDir string) *QueryStore {
	return &QueryStore{
		path: filepath.Join(dataDir, savedQueryFileName),
	}
}

// List returns all saved queries sorted by name.
func (s *QueryStore) List() ([]SavedQuery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queries, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	sort.Slice(queries, func(i, j int) bool {
		return strings.ToLower(queries[i].Name) < strings.ToLower(queries[j].Name)
	})

	return queries, nil
}

// Find returns one saved query by name.
func (s *QueryStore) Find(name string) (SavedQuery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return SavedQuery{}, fmt.Errorf("query name must not be empty")
	}

	queries, err := s.loadLocked()
	if err != nil {
		return SavedQuery{}, err
	}

	for _, query := range queries {
		if strings.EqualFold(query.Name, name) {
			return query, nil
		}
	}

	return SavedQuery{}, fmt.Errorf("saved query %q was not found", name)
}

// Save creates or updates one saved query.
func (s *QueryStore) Save(query SavedQuery) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query, err := normalizeSavedQuery(query)
	if err != nil {
		return err
	}

	queries, err := s.loadLocked()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if query.CreatedAt.IsZero() {
		query.CreatedAt = now
	}
	query.UpdatedAt = now

	replaced := false
	for index := range queries {
		if strings.EqualFold(queries[index].Name, query.Name) {
			query.CreatedAt = queries[index].CreatedAt
			queries[index] = query
			replaced = true
			break
		}
	}

	if !replaced {
		queries = append(queries, query)
	}

	return s.saveLocked(queries)
}

// Delete removes one saved query by name.
func (s *QueryStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("query name must not be empty")
	}

	queries, err := s.loadLocked()
	if err != nil {
		return err
	}

	filtered := make([]SavedQuery, 0, len(queries))
	found := false
	for _, query := range queries {
		if strings.EqualFold(query.Name, name) {
			found = true
			continue
		}
		filtered = append(filtered, query)
	}

	if !found {
		return fmt.Errorf("saved query %q was not found", name)
	}

	return s.saveLocked(filtered)
}

// Rename changes the name of one saved query without changing its SQL text.
func (s *QueryStore) Rename(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("old and new query names must not be empty")
	}

	queries, err := s.loadLocked()
	if err != nil {
		return err
	}

	var targetIndex = -1
	for index := range queries {
		if strings.EqualFold(queries[index].Name, newName) && !strings.EqualFold(queries[index].Name, oldName) {
			return fmt.Errorf("saved query %q already exists", newName)
		}
		if strings.EqualFold(queries[index].Name, oldName) {
			targetIndex = index
		}
	}

	if targetIndex == -1 {
		return fmt.Errorf("saved query %q was not found", oldName)
	}

	queries[targetIndex].Name = newName
	queries[targetIndex].UpdatedAt = time.Now().UTC()
	return s.saveLocked(queries)
}

func (s *QueryStore) loadLocked() ([]SavedQuery, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SavedQuery{}, nil
		}
		return nil, fmt.Errorf("read saved queries: %w", err)
	}

	if len(data) == 0 {
		return []SavedQuery{}, nil
	}

	var queries []SavedQuery
	if err := json.Unmarshal(data, &queries); err != nil {
		return nil, fmt.Errorf("decode saved queries: %w", err)
	}
	return queries, nil
}

func (s *QueryStore) saveLocked(queries []SavedQuery) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create saved queries directory: %w", err)
	}

	data, err := json.MarshalIndent(queries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode saved queries: %w", err)
	}

	tempPath := s.path + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create saved queries temp file: %w", err)
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write saved queries temp file: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync saved queries temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close saved queries temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace saved queries file: %w", err)
	}
	return nil
}

func normalizeSavedQuery(query SavedQuery) (SavedQuery, error) {
	query.Name = strings.TrimSpace(query.Name)
	query.QueryText = strings.TrimSpace(query.QueryText)
	query.Notes = strings.TrimSpace(query.Notes)

	if query.Name == "" {
		return SavedQuery{}, fmt.Errorf("query name must not be empty")
	}
	if query.QueryText == "" {
		return SavedQuery{}, fmt.Errorf("query text must not be empty")
	}

	return query, nil
}
