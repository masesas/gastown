package config

import "fmt"

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
// is missing, unreadable, or the field is unset.
func GetReadinessMode(rigPath string) ReadinessMode {
	settings, err := LoadRigSettings(RigSettingsPath(rigPath))
	if err != nil || settings == nil || settings.Readiness == "" {
		return DefaultReadinessMode
	}
	return ReadinessMode(settings.Readiness)
}
