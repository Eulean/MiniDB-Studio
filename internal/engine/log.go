package engine

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"
)

var recordMagic = [4]byte{'M', 'D', 'B', '1'}

const (
	commandSet    = "SET"
	commandDelete = "DELETE"
)

// logRecord is the durable unit written to the append-only log.
// The payload is JSON so the structure stays readable and extensible.
type logRecord struct {
	Command   string    `json:"command"`
	Key       string    `json:"key"`
	Value     string    `json:"value,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// appendRecord writes a framed record and forces the bytes to durable storage.
func appendRecord(file *os.File, record logRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal log record: %w", err)
	}

	checksum := crc32.ChecksumIEEE(payload)
	header := make([]byte, 8)
	copy(header[:4], recordMagic[:])
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("write log header: %w", err)
	}

	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write log payload: %w", err)
	}

	trailer := make([]byte, 4)
	binary.BigEndian.PutUint32(trailer, checksum)

	if _, err := file.Write(trailer); err != nil {
		return fmt.Errorf("write log checksum: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync log file: %w", err)
	}

	return nil
}

// recordReader streams framed records from disk and validates their checksum.
type recordReader struct {
	file     *os.File
	fileSize int64
	offset   int64
}

func newRecordReader(file *os.File) (*recordReader, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat log file: %w", err)
	}

	return &recordReader{
		file:     file,
		fileSize: info.Size(),
	}, nil
}

// next returns the next valid record, signals io.EOF when cleanly finished,
// and ignores only a truncated final record caused by an interrupted write.
func (r *recordReader) next() (logRecord, error) {
	var record logRecord

	if r.offset >= r.fileSize {
		return record, io.EOF
	}

	header := make([]byte, 8)
	if _, err := io.ReadFull(r.file, header); err != nil {
		return record, r.classifyReadError("read record header", err)
	}
	r.offset += int64(len(header))

	if string(header[:4]) != string(recordMagic[:]) {
		return record, fmt.Errorf("log recovery error at offset %d: invalid record magic", r.offset-int64(len(header)))
	}

	payloadLength := binary.BigEndian.Uint32(header[4:])
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(r.file, payload); err != nil {
		return record, r.classifyReadError("read record payload", err)
	}
	r.offset += int64(len(payload))

	checksumBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.file, checksumBytes); err != nil {
		return record, r.classifyReadError("read record checksum", err)
	}
	r.offset += int64(len(checksumBytes))

	expectedChecksum := binary.BigEndian.Uint32(checksumBytes)
	actualChecksum := crc32.ChecksumIEEE(payload)
	if expectedChecksum != actualChecksum {
		return record, fmt.Errorf("log recovery error at offset %d: checksum mismatch", r.offset-int64(len(payload)+len(checksumBytes)))
	}

	if err := json.Unmarshal(payload, &record); err != nil {
		return record, fmt.Errorf("log recovery error at offset %d: decode record: %w", r.offset-int64(len(payload)+len(checksumBytes)), err)
	}

	return record, nil
}

// classifyReadError decides whether EOF means an interrupted final record or a real recovery failure.
func (r *recordReader) classifyReadError(action string, err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		if r.offset < r.fileSize {
			return io.EOF
		}
	}

	return fmt.Errorf("log recovery error at offset %d: %s: %w", r.offset, action, err)
}
