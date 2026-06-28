package engine

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// frameReader streams framed payloads from disk and detects corruption or interrupted final writes.
type frameReader struct {
	file                   *os.File
	fileSize               int64
	offset                 int64
	tolerateIncompleteTail bool
}

// framedPayload is the decoded boundary information for one durable frame.
type framedPayload struct {
	Offset  int64
	Payload []byte
}

func newFrameReader(file *os.File, tolerateIncompleteTail bool) (*frameReader, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat framed file: %w", err)
	}

	return &frameReader{
		file:                   file,
		fileSize:               info.Size(),
		tolerateIncompleteTail: tolerateIncompleteTail,
	}, nil
}

// next returns the next payload or io.EOF when the file is cleanly exhausted.
// A truncated final frame is treated as EOF so interrupted final writes are ignored safely.
func (r *frameReader) next() (framedPayload, error) {
	frame := framedPayload{}
	if r.offset >= r.fileSize {
		return frame, io.EOF
	}

	startOffset := r.offset
	header := make([]byte, 8)
	if _, err := io.ReadFull(r.file, header); err != nil {
		return frame, r.classifyReadError("read frame header", err)
	}
	r.offset += int64(len(header))

	if string(header[:4]) != frameMagic {
		return frame, fmt.Errorf("log recovery error at offset %d: invalid frame magic", startOffset)
	}

	payloadLength := binary.BigEndian.Uint32(header[4:])
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(r.file, payload); err != nil {
		return frame, r.classifyReadError("read frame payload", err)
	}
	r.offset += int64(len(payload))

	checksumBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.file, checksumBytes); err != nil {
		return frame, r.classifyReadError("read frame checksum", err)
	}
	r.offset += int64(len(checksumBytes))

	expectedChecksum := binary.BigEndian.Uint32(checksumBytes)
	actualChecksum := crc32.ChecksumIEEE(payload)
	if expectedChecksum != actualChecksum {
		return frame, fmt.Errorf("log recovery error at offset %d: checksum mismatch", startOffset)
	}

	return framedPayload{
		Offset:  startOffset,
		Payload: payload,
	}, nil
}

func (r *frameReader) classifyReadError(action string, err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		// Only the final incomplete frame is ignored.
		if r.tolerateIncompleteTail && r.offset < r.fileSize {
			return io.EOF
		}
	}

	return fmt.Errorf("log recovery error at offset %d: %s: %w", r.offset, action, err)
}

// appendJSONFrame writes one framed JSON payload, syncs it, and returns the starting offset.
func appendJSONFrame(file *os.File, value any) (int64, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return 0, fmt.Errorf("marshal framed payload: %w", err)
	}

	startOffset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, fmt.Errorf("seek file before append: %w", err)
	}

	header := make([]byte, 8)
	copy(header[:4], []byte(frameMagic))
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))

	if _, err := file.Write(header); err != nil {
		return 0, fmt.Errorf("write frame header: %w", err)
	}

	if _, err := file.Write(payload); err != nil {
		return 0, fmt.Errorf("write frame payload: %w", err)
	}

	checksumBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(checksumBytes, crc32.ChecksumIEEE(payload))
	if _, err := file.Write(checksumBytes); err != nil {
		return 0, fmt.Errorf("write frame checksum: %w", err)
	}

	if err := file.Sync(); err != nil {
		return 0, fmt.Errorf("sync framed file: %w", err)
	}

	return startOffset, nil
}

func readJSONFrameAt(path string, offset int64, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open framed file %q: %w", path, err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek framed file %q: %w", path, err)
	}

	reader, err := newFrameReader(file, false)
	if err != nil {
		return err
	}
	reader.offset = offset

	frame, err := reader.next()
	if err != nil {
		return err
	}

	if err := json.Unmarshal(frame.Payload, destination); err != nil {
		return fmt.Errorf("decode framed payload at offset %d: %w", offset, err)
	}

	return nil
}
