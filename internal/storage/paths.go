package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appFolderName = "MiniDB Studio"

// AppDataDir resolves the application data directory for MiniDB Studio.
// Windows is treated as the primary target, but we keep a portable fallback path.
func AppDataDir() (string, error) {
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, appFolderName), nil
		}
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}

	return filepath.Join(configDir, appFolderName), nil
}

// EnsureAppDataDir creates the application data directory when it does not exist yet.
func EnsureAppDataDir() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create app data directory %q: %w", dir, err)
	}

	return dir, nil
}
