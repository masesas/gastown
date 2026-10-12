package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ReadinessMode controls the per-rig readiness check before dispatch.
type ReadinessMode string

const (
	// ReadinessOff disables the check.
	ReadinessOff ReadinessMode = "off"
	// ReadinessWarn dispatches anyway, with a warning and an event.
	ReadinessWarn ReadinessMode = "warn"
	// ReadinessEnforce refuses to dispatch beads that are not ready.
	ReadinessEnforce ReadinessMode = "enforce"
)

// DefaultReadinessMode applies when a rig does not configure readiness.
const DefaultReadinessMode = ReadinessWarn

func validateReadinessMode(mode string) error {
	switch ReadinessMode(mode) {
	case "", ReadinessOff, ReadinessWarn, ReadinessEnforce:
		return nil
	default:
		return fmt.Errorf("invalid readiness %q: must be off, warn, or enforce", mode)
	}
}

// GetReadinessMode returns the readiness mode for a rig from
// settings/config.json, falling back to DefaultReadinessMode when the file
// is missing or the field is unset. When the file exists but fails to load
// (e.g. an unrelated field is invalid), an explicit valid "readiness" value
// in the raw JSON still applies, so enforce does not silently drop to warn.
func GetReadinessMode(rigPath string) ReadinessMode {
	path := RigSettingsPath(rigPath)
	settings, err := LoadRigSettings(path)
	if err == nil && settings != nil {
		if settings.Readiness == "" {
			return DefaultReadinessMode
		}
		return ReadinessMode(settings.Readiness)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		if mode, ok := readinessFromRawSettings(path); ok {
			return mode
		}
	}
	return DefaultReadinessMode
}

// readinessFromRawSettings reads only the "readiness" key from a settings
// file that failed full validation.
func readinessFromRawSettings(path string) (ReadinessMode, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: rig settings path
	if err != nil {
		return "", false
	}
	var raw struct {
		Readiness string `json:"readiness"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Readiness == "" || validateReadinessMode(raw.Readiness) != nil {
		return "", false
	}
	return ReadinessMode(raw.Readiness), true
}
