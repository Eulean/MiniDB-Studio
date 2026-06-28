package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

type legacyFrameReader struct {
	file     *os.File
	fileSize int64
	offset   int64
}

func newLegacyFrameReader(file *os.File) (*legacyFrameReader, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat legacy framed file: %w", err)
	}

	return &legacyFrameReader{
		file:     file,
		fileSize: info.Size(),
	}, nil
}

func (r *legacyFrameReader) next() (framedPayload, error) {
	frame := framedPayload{}
	if r.offset >= r.fileSize {
		return frame, io.EOF
	}

	startOffset := r.offset
	header := make([]byte, 8)
	if _, err := io.ReadFull(r.file, header); err != nil {
		return frame, fmt.Errorf("read legacy frame header at offset %d: %w", r.offset, err)
	}
	r.offset += int64(len(header))

	if string(header[:4]) != legacyFrameMagic {
		return frame, fmt.Errorf("legacy migration error at offset %d: invalid frame magic", startOffset)
	}

	payloadLength := binary.BigEndian.Uint32(header[4:])
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(r.file, payload); err != nil {
		return frame, fmt.Errorf("read legacy frame payload at offset %d: %w", r.offset, err)
	}
	r.offset += int64(len(payload))

	checksumBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.file, checksumBytes); err != nil {
		return frame, fmt.Errorf("read legacy frame checksum at offset %d: %w", r.offset, err)
	}
	r.offset += int64(len(checksumBytes))

	expectedChecksum := binary.BigEndian.Uint32(checksumBytes)
	actualChecksum := crc32.ChecksumIEEE(payload)
	if expectedChecksum != actualChecksum {
		return frame, fmt.Errorf("legacy migration error at offset %d: checksum mismatch", startOffset)
	}

	return framedPayload{
		Offset:  startOffset,
		Payload: payload,
	}, nil
}
