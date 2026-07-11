package robotgo

import (
	"fmt"
	"sync"
)

// RuntimeConfig contains process-wide defaults used by legacy package-level
// APIs. Prefer explicit per-call arguments where available.
type RuntimeConfig struct {
	MouseDelay    int
	KeyDelay      int
	DisplayID     int
	TreatAsHandle bool
	Scale         bool
}

var runtimeConfigMu sync.RWMutex

// GetRuntimeConfig returns one consistent snapshot of the legacy defaults.
func GetRuntimeConfig() RuntimeConfig {
	runtimeConfigMu.RLock()
	defer runtimeConfigMu.RUnlock()
	return RuntimeConfig{
		MouseDelay: MouseSleep, KeyDelay: KeySleep, DisplayID: DisplayID,
		TreatAsHandle: NotPid, Scale: Scale,
	}
}

// SetRuntimeConfig atomically replaces the defaults used by package-level APIs.
// Direct writes to MouseSleep, KeySleep, DisplayID, NotPid, and Scale remain
// supported for compatibility, but must not race with active operations.
func SetRuntimeConfig(config RuntimeConfig) error {
	if config.MouseDelay < 0 || config.KeyDelay < 0 {
		return fmt.Errorf("robotgo: delays must be non-negative")
	}
	if config.DisplayID < -1 {
		return fmt.Errorf("robotgo: display ID must be -1 or greater")
	}
	runtimeConfigMu.Lock()
	MouseSleep = config.MouseDelay
	KeySleep = config.KeyDelay
	DisplayID = config.DisplayID
	NotPid = config.TreatAsHandle
	Scale = config.Scale
	runtimeConfigMu.Unlock()
	return nil
}

func currentMouseDelay() int     { return GetRuntimeConfig().MouseDelay }
func currentKeyDelay() int       { return GetRuntimeConfig().KeyDelay }
func currentDisplayID() int      { return GetRuntimeConfig().DisplayID }
func currentTreatAsHandle() bool { return GetRuntimeConfig().TreatAsHandle }
func currentScale() bool         { return GetRuntimeConfig().Scale }

func setInputDelays(keyDelay, mouseDelay int) error {
	config := GetRuntimeConfig()
	config.KeyDelay = keyDelay
	config.MouseDelay = mouseDelay
	return SetRuntimeConfig(config)
}
