package engine

import (
	"encoding/json"
	"fmt"
)

func encodeMetadata(meta metadata) ([]byte, error) {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode metadata: %w", err)
	}

	return data, nil
}

func decodeMetadata(data []byte) (metadata, error) {
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return metadata{}, fmt.Errorf("decode metadata: %w", err)
	}

	return meta, nil
}
