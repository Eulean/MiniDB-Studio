package app

import (
	"sort"
	"time"

	"minidb-studio/internal/engine"
)

// CollectionProfile summarizes one collection for dashboard and preset-planning screens.
type CollectionProfile struct {
	Name            string
	RecordCount     int
	JSONRecordCount int
	RawRecordCount  int
	SampleKeys      []string
	LastUpdated     time.Time
}

// DashboardSnapshot groups the high-signal product summary shown on the home page.
type DashboardSnapshot struct {
	Stats           engine.Stats
	PresetCount     int
	SavedQueryCount int
	CollectionInfo  []CollectionProfile
	RecentActivity  []ActivityEntry
}

// CollectionProfiles summarizes every live collection without changing engine responsibilities.
func (a *Application) CollectionProfiles() ([]CollectionProfile, error) {
	collections := a.db.Collections()
	profiles := make([]CollectionProfile, 0, len(collections))

	for _, collection := range collections {
		keys := a.db.KeysInCollection(collection, "")
		profile := CollectionProfile{
			Name:        collection,
			RecordCount: len(keys),
		}

		sampleLimit := 3
		if len(keys) < sampleLimit {
			sampleLimit = len(keys)
		}
		profile.SampleKeys = append(profile.SampleKeys, keys[:sampleLimit]...)

		for _, key := range keys {
			metadata, ok := a.db.GetRecordMetadataInCollection(collection, key)
			if !ok {
				continue
			}

			switch metadata.ValueKind {
			case engine.ValueKindJSON:
				profile.JSONRecordCount++
			default:
				profile.RawRecordCount++
			}

			if metadata.UpdatedAt.After(profile.LastUpdated) {
				profile.LastUpdated = metadata.UpdatedAt
			}
		}

		profiles = append(profiles, profile)
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].RecordCount == profiles[j].RecordCount {
			return profiles[i].Name < profiles[j].Name
		}
		return profiles[i].RecordCount > profiles[j].RecordCount
	})

	return profiles, nil
}

// DashboardSnapshot returns one compact product summary for the overview page.
func (a *Application) DashboardSnapshot() (DashboardSnapshot, error) {
	stats, err := a.db.Stats()
	if err != nil {
		return DashboardSnapshot{}, err
	}

	profiles, err := a.CollectionProfiles()
	if err != nil {
		return DashboardSnapshot{}, err
	}

	presets, err := a.ListDatasetPresets()
	if err != nil {
		return DashboardSnapshot{}, err
	}

	queries, err := a.ListSavedQueries()
	if err != nil {
		return DashboardSnapshot{}, err
	}

	activity, err := a.RecentActivity(8)
	if err != nil {
		return DashboardSnapshot{}, err
	}

	return DashboardSnapshot{
		Stats:           stats,
		PresetCount:     len(presets),
		SavedQueryCount: len(queries),
		CollectionInfo:  profiles,
		RecentActivity:  activity,
	}, nil
}

// RecentActivity exposes the durable activity feed to dashboard and management pages.
func (a *Application) RecentActivity(limit int) ([]ActivityEntry, error) {
	return a.activityStore.ListRecent(limit)
}
