package engine

import (
	"fmt"
	"io"
)

// replayLog rebuilds the in-memory index and counters from durable records.
func (db *DB) replayLog() error {
	if _, err := db.logFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek log file for replay: %w", err)
	}

	reader, err := newRecordReader(db.logFile)
	if err != nil {
		return err
	}

	index := make(map[string]string)
	setOps := db.setOps
	deleteOps := db.deleteOps
	var replayedRecords uint64

	for {
		record, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		replayedRecords++

		switch record.Command {
		case commandSet:
			index[record.Key] = record.Value
			if replayedRecords > db.snapshotRecordCount {
				setOps++
			}
		case commandDelete:
			delete(index, record.Key)
			if replayedRecords > db.snapshotRecordCount {
				deleteOps++
			}
		default:
			return fmt.Errorf("log recovery error: unknown command %q", record.Command)
		}
	}

	db.index = index
	db.setOps = setOps
	db.deleteOps = deleteOps

	if _, err := db.logFile.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek log file to end after replay: %w", err)
	}

	return nil
}
