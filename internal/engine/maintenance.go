package engine

import "fmt"

// MaintenanceReport returns simple recommendation heuristics for the current database state.
func (db *DB) MaintenanceReport() (MaintenanceReport, error) {
	stats, err := db.Stats()
	if err != nil {
		return MaintenanceReport{}, err
	}

	report := MaintenanceReport{
		Healthy:         true,
		Recommendations: []string{},
	}

	if stats.SnapshotCount == 0 && stats.LiveKeyCount >= 10 {
		report.SnapshotRecommended = true
		report.Recommendations = append(report.Recommendations, "Create a snapshot to reduce future startup replay.")
	}

	if stats.ActiveSegmentSize >= db.options.SegmentSizeLimit || stats.SegmentCount >= 4 {
		report.CompactionRecommended = true
		report.Recommendations = append(report.Recommendations, "Compact the database to reduce segment sprawl.")
	}

	validation, err := db.Validate()
	if err != nil {
		return MaintenanceReport{}, err
	}
	if len(validation.IncompleteTailSegments) > 0 || len(validation.CorruptedSegments) > 0 {
		report.ValidationRecommended = true
		report.Healthy = false
		report.Recommendations = append(report.Recommendations, fmt.Sprintf("Validate or repair the database. Corrupted=%d incomplete-tail=%d.", len(validation.CorruptedSegments), len(validation.IncompleteTailSegments)))
	}

	if len(report.Recommendations) == 0 {
		report.Recommendations = append(report.Recommendations, "Database health looks good.")
	}

	return report, nil
}
