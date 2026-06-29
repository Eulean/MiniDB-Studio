package app

import (
	"strings"
	"sync"
)

// Page identifies the active screen in the desktop application.
type Page string

const (
	PageHome        Page = "Overview"
	PageExplorer    Page = "Data Explorer"
	PageQuery       Page = "Query Studio"
	PageConsole     Page = "Command Console"
	PageMaintenance Page = "Maintenance"
	PagePresets     Page = "Preset Library"
	PageAbout       Page = "About"
)

// Snapshot is a read-only copy of UI state used by the status bar and history panel.
type Snapshot struct {
	CurrentPage      Page
	DatabaseLocation string
	CurrentStatus    string
	LastResult       string
	CommandHistory   string
}

// State keeps UI-oriented state separate from the database engine.
type State struct {
	mu sync.RWMutex

	currentPage      Page
	databaseLocation string
	currentStatus    string
	lastResult       string
	commandHistory   []string
}

// NewState constructs the shared application state with sensible defaults.
func NewState(databaseLocation string) *State {
	return &State{
		currentPage:      PageHome,
		databaseLocation: databaseLocation,
		currentStatus:    "Ready",
		lastResult:       "Application started",
		commandHistory:   make([]string, 0, 32),
	}
}

// SetPage tracks the current page so navigation and status can stay in sync.
func (s *State) SetPage(page Page) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.currentPage = page
}

// SetCurrentStatus updates the short status text shown in the footer.
func (s *State) SetCurrentStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.currentStatus = status
}

// SetLastResult updates the latest operation result shown in the footer.
func (s *State) SetLastResult(result string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastResult = result
}

// AppendHistory adds a new console history entry.
func (s *State) AppendHistory(entry string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commandHistory = append(s.commandHistory, entry)
}

// Snapshot returns a thread-safe copy of the current UI state.
func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Snapshot{
		CurrentPage:      s.currentPage,
		DatabaseLocation: s.databaseLocation,
		CurrentStatus:    s.currentStatus,
		LastResult:       s.lastResult,
		CommandHistory:   strings.Join(s.commandHistory, "\n\n"),
	}
}
