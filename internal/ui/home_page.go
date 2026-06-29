package ui

import (
	"fmt"
	"strings"

	studioapp "minidb-studio/internal/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// HomePage gives MiniDB Studio a real landing dashboard instead of dropping users into one tool directly.
type HomePage struct {
	application     *studioapp.Application
	onStatusChanged func()
	root            fyne.CanvasObject
	summaryLabel    *widget.Label
	activityLabel   *widget.Label
	profilesLabel   *widget.Label
}

// NewHomePage creates the overview dashboard with product-level summaries and recent activity.
func NewHomePage(application *studioapp.Application, onStatusChanged func()) *HomePage {
	page := &HomePage{
		application:     application,
		onStatusChanged: onStatusChanged,
		summaryLabel:    widget.NewLabel(""),
		activityLabel:   widget.NewLabel(""),
		profilesLabel:   widget.NewLabel(""),
	}

	page.summaryLabel.Wrapping = fyne.TextWrapWord
	page.activityLabel.Wrapping = fyne.TextWrapWord
	page.profilesLabel.Wrapping = fyne.TextWrapWord

	refreshButton := widget.NewButton("Refresh Dashboard", func() {
		page.Refresh()
		application.State().SetLastResult("Dashboard refreshed")
		onStatusChanged()
	})

	introCard := sectionCard(
		"Studio Overview",
		"The home page surfaces the parts of MiniDB that matter most before you drill into records or maintenance.",
		container.NewVBox(
			compactHint("Use this page to check collection growth, preset readiness, and the most recent durable actions."),
			refreshButton,
		),
	)

	summaryCard := sectionCard(
		"Workspace Summary",
		"High-signal counts for storage, presets, and collections.",
		page.summaryLabel,
	)

	activityCard := sectionCard(
		"Recent Activity",
		"Durable history of the last important operations completed in this workspace.",
		page.activityLabel,
	)

	profilesCard := sectionCard(
		"Collection Profiles",
		"A compact inventory of collection size, value kinds, and recent change activity.",
		page.profilesLabel,
	)

	grid := container.NewGridWithColumns(2, summaryCard, activityCard)
	page.root = standardScroll(container.NewVBox(introCard, grid, profilesCard))
	page.Refresh()
	return page
}

// CanvasObject returns the overview dashboard for embedding in the main shell.
func (p *HomePage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads the dashboard cards from the persistent application state.
func (p *HomePage) Refresh() {
	snapshot, err := p.application.DashboardSnapshot()
	if err != nil {
		p.summaryLabel.SetText("Dashboard unavailable: " + err.Error())
		p.activityLabel.SetText("Dashboard unavailable")
		p.profilesLabel.SetText("Dashboard unavailable")
		return
	}

	p.summaryLabel.SetText(strings.Join([]string{
		fmt.Sprintf("Live keys: %d", snapshot.Stats.LiveKeyCount),
		fmt.Sprintf("Collections: %d", len(snapshot.CollectionInfo)),
		fmt.Sprintf("Saved presets: %d", snapshot.PresetCount),
		fmt.Sprintf("Saved queries: %d", snapshot.SavedQueryCount),
		fmt.Sprintf("Segments: %d", snapshot.Stats.SegmentCount),
		fmt.Sprintf("Snapshots: %d", snapshot.Stats.SnapshotCount),
		fmt.Sprintf("Replay count on startup: %d", snapshot.Stats.StartupReplayCount),
	}, "\n"))

	if len(snapshot.RecentActivity) == 0 {
		p.activityLabel.SetText("No recorded activity yet.")
	} else {
		lines := make([]string, 0, len(snapshot.RecentActivity))
		for _, entry := range snapshot.RecentActivity {
			lines = append(lines, fmt.Sprintf("%s | %s | %s | %s", entry.Timestamp.Local().Format("2006-01-02 15:04"), entry.Status, entry.Action, compactText(entry.Target, 40)))
		}
		p.activityLabel.SetText(strings.Join(lines, "\n"))
	}

	if len(snapshot.CollectionInfo) == 0 {
		p.profilesLabel.SetText("No collections available yet.")
		return
	}

	lines := make([]string, 0, len(snapshot.CollectionInfo))
	for _, profile := range snapshot.CollectionInfo {
		lastUpdated := "never"
		if !profile.LastUpdated.IsZero() {
			lastUpdated = profile.LastUpdated.Local().Format("2006-01-02 15:04")
		}
		samples := strings.Join(profile.SampleKeys, ", ")
		if samples == "" {
			samples = "no sample keys yet"
		}
		lines = append(lines, fmt.Sprintf("%s | records=%d json=%d raw=%d | last=%s | sample=%s", profile.Name, profile.RecordCount, profile.JSONRecordCount, profile.RawRecordCount, lastUpdated, samples))
	}
	p.profilesLabel.SetText(strings.Join(lines, "\n"))
}
