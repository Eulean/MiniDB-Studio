package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const datasetPresetFileName = "dataset-presets.json"

// DatasetPreset stores reusable defaults for dataset preview, import, and export workflows.
type DatasetPreset struct {
	Name         string `json:"name"`
	Collection   string `json:"collection"`
	KeyField     string `json:"key_field"`
	ConflictMode string `json:"conflict_mode"`
	QueryText    string `json:"query_text"`
}

// DatasetPresetStore persists named presets beside the application data.
type DatasetPresetStore struct {
	path string
	mu   sync.Mutex
}

// NewDatasetPresetStore creates a preset store rooted in the active application data directory.
func NewDatasetPresetStore(dataDir string) *DatasetPresetStore {
	return &DatasetPresetStore{
		path: filepath.Join(dataDir, datasetPresetFileName),
	}
}

// List returns all saved presets sorted by name.
func (s *DatasetPresetStore) List() ([]DatasetPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	presets, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	sort.Slice(presets, func(left, right int) bool {
		return strings.ToLower(presets[left].Name) < strings.ToLower(presets[right].Name)
	})

	return presets, nil
}

// Find returns one preset by name using case-insensitive lookup.
func (s *DatasetPresetStore) Find(name string) (DatasetPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return DatasetPreset{}, fmt.Errorf("preset name must not be empty")
	}

	presets, err := s.loadLocked()
	if err != nil {
		return DatasetPreset{}, err
	}
	for _, preset := range presets {
		if strings.EqualFold(preset.Name, name) {
			return preset, nil
		}
	}

	return DatasetPreset{}, fmt.Errorf("preset %q was not found", name)
}

// Save inserts or overwrites one preset by name.
func (s *DatasetPresetStore) Save(preset DatasetPreset) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	preset, err := normalizeDatasetPreset(preset)
	if err != nil {
		return err
	}

	presets, err := s.loadLocked()
	if err != nil {
		return err
	}

	replaced := false
	for index := range presets {
		if strings.EqualFold(presets[index].Name, preset.Name) {
			presets[index] = preset
			replaced = true
			break
		}
	}
	if !replaced {
		presets = append(presets, preset)
	}

	return s.saveLocked(presets)
}

// Delete removes one preset by name. Missing names are ignored.
func (s *DatasetPresetStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("preset name must not be empty")
	}

	presets, err := s.loadLocked()
	if err != nil {
		return err
	}

	filtered := presets[:0]
	for _, preset := range presets {
		if strings.EqualFold(preset.Name, name) {
			continue
		}
		filtered = append(filtered, preset)
	}

	return s.saveLocked(filtered)
}

// Rename changes a preset name without modifying its workflow fields.
func (s *DatasetPresetStore) Rename(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" {
		return fmt.Errorf("old preset name must not be empty")
	}
	if newName == "" {
		return fmt.Errorf("new preset name must not be empty")
	}

	presets, err := s.loadLocked()
	if err != nil {
		return err
	}

	sourceIndex := -1
	for index := range presets {
		if strings.EqualFold(presets[index].Name, oldName) {
			sourceIndex = index
			break
		}
	}
	if sourceIndex == -1 {
		return fmt.Errorf("preset %q was not found", oldName)
	}

	for index := range presets {
		if index == sourceIndex {
			continue
		}
		if strings.EqualFold(presets[index].Name, newName) {
			return fmt.Errorf("preset %q already exists", newName)
		}
	}

	presets[sourceIndex].Name = newName
	return s.saveLocked(presets)
}

// Duplicate copies one existing preset to a new name without changing its workflow fields.
func (s *DatasetPresetStore) Duplicate(sourceName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourceName = strings.TrimSpace(sourceName)
	newName = strings.TrimSpace(newName)
	if sourceName == "" {
		return fmt.Errorf("source preset name must not be empty")
	}
	if newName == "" {
		return fmt.Errorf("new preset name must not be empty")
	}

	presets, err := s.loadLocked()
	if err != nil {
		return err
	}

	sourceIndex := -1
	for index := range presets {
		if strings.EqualFold(presets[index].Name, sourceName) {
			sourceIndex = index
			break
		}
	}
	if sourceIndex == -1 {
		return fmt.Errorf("preset %q was not found", sourceName)
	}

	for index := range presets {
		if strings.EqualFold(presets[index].Name, newName) {
			return fmt.Errorf("preset %q already exists", newName)
		}
	}

	duplicate := presets[sourceIndex]
	duplicate.Name = newName
	presets = append(presets, duplicate)
	return s.saveLocked(presets)
}

// Export writes one preset to a standalone JSON file that can be shared or backed up.
func (s *DatasetPresetStore) Export(name, destinationPath string) (DatasetPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	destinationPath = strings.TrimSpace(destinationPath)
	if name == "" {
		return DatasetPreset{}, fmt.Errorf("preset name must not be empty")
	}
	if destinationPath == "" {
		return DatasetPreset{}, fmt.Errorf("destination path must not be empty")
	}

	presets, err := s.loadLocked()
	if err != nil {
		return DatasetPreset{}, err
	}

	var preset DatasetPreset
	found := false
	for _, candidate := range presets {
		if strings.EqualFold(candidate.Name, name) {
			preset = candidate
			found = true
			break
		}
	}
	if !found {
		return DatasetPreset{}, fmt.Errorf("preset %q was not found", name)
	}

	if err := writePresetFile(destinationPath, preset); err != nil {
		return DatasetPreset{}, err
	}
	return preset, nil
}

// Import loads one preset JSON file and saves it into local preset storage.
func (s *DatasetPresetStore) Import(sourcePath string) (DatasetPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return DatasetPreset{}, fmt.Errorf("source path must not be empty")
	}

	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return DatasetPreset{}, fmt.Errorf("read preset file: %w", err)
	}

	var preset DatasetPreset
	if err := json.Unmarshal(data, &preset); err != nil {
		return DatasetPreset{}, fmt.Errorf("decode preset file: %w", err)
	}

	preset, err = normalizeDatasetPreset(preset)
	if err != nil {
		return DatasetPreset{}, err
	}

	presets, err := s.loadLocked()
	if err != nil {
		return DatasetPreset{}, err
	}

	replaced := false
	for index := range presets {
		if strings.EqualFold(presets[index].Name, preset.Name) {
			presets[index] = preset
			replaced = true
			break
		}
	}
	if !replaced {
		presets = append(presets, preset)
	}

	if err := s.saveLocked(presets); err != nil {
		return DatasetPreset{}, err
	}
	return preset, nil
}

func (s *DatasetPresetStore) loadLocked() ([]DatasetPreset, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []DatasetPreset{}, nil
		}
		return nil, fmt.Errorf("read dataset presets: %w", err)
	}
	if len(data) == 0 {
		return []DatasetPreset{}, nil
	}

	var presets []DatasetPreset
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, fmt.Errorf("decode dataset presets: %w", err)
	}
	return presets, nil
}

func (s *DatasetPresetStore) saveLocked(presets []DatasetPreset) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create preset directory: %w", err)
	}

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return fmt.Errorf("encode dataset presets: %w", err)
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write dataset presets temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace dataset presets file: %w", err)
	}

	return nil
}

func normalizeDatasetPreset(preset DatasetPreset) (DatasetPreset, error) {
	preset.Name = strings.TrimSpace(preset.Name)
	if preset.Name == "" {
		return DatasetPreset{}, fmt.Errorf("preset name must not be empty")
	}
	preset.Collection = strings.TrimSpace(preset.Collection)
	if preset.Collection == "" {
		return DatasetPreset{}, fmt.Errorf("preset collection must not be empty")
	}
	preset.KeyField = strings.TrimSpace(preset.KeyField)
	if preset.KeyField == "" {
		return DatasetPreset{}, fmt.Errorf("preset key field must not be empty")
	}
	preset.ConflictMode = strings.TrimSpace(strings.ToLower(preset.ConflictMode))
	switch preset.ConflictMode {
	case "skip", "overwrite":
	default:
		return DatasetPreset{}, fmt.Errorf("preset conflict mode must be skip or overwrite")
	}
	preset.QueryText = strings.TrimSpace(preset.QueryText)
	return preset, nil
}

func writePresetFile(destinationPath string, preset DatasetPreset) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create preset export directory: %w", err)
	}

	data, err := json.MarshalIndent(preset, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preset export: %w", err)
	}

	tempPath := destinationPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("write preset export temp file: %w", err)
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		return fmt.Errorf("replace preset export file: %w", err)
	}
	return nil
}
