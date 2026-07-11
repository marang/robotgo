package robotgo

import (
	"sync"
	"testing"
)

func TestRuntimeConfigSnapshotsAreConsistent(t *testing.T) {
	previous := GetRuntimeConfig()
	t.Cleanup(func() {
		if err := SetRuntimeConfig(previous); err != nil {
			t.Errorf("restore runtime config: %v", err)
		}
	})

	configs := []RuntimeConfig{
		{MouseDelay: 1, KeyDelay: 2, DisplayID: 3, TreatAsHandle: true, Scale: false},
		{MouseDelay: 11, KeyDelay: 12, DisplayID: 13, TreatAsHandle: false, Scale: true},
	}
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		config := configs[i%len(configs)]
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := SetRuntimeConfig(config); err != nil {
				t.Errorf("SetRuntimeConfig: %v", err)
				return
			}
			got := GetRuntimeConfig()
			if got != configs[0] && got != configs[1] {
				t.Errorf("torn runtime config snapshot: %+v", got)
			}
		}()
	}
	wait.Wait()
}

func TestRuntimeConfigRejectsInvalidValuesWithoutMutation(t *testing.T) {
	before := GetRuntimeConfig()
	invalid := []RuntimeConfig{
		{MouseDelay: -1, DisplayID: -1},
		{KeyDelay: -1, DisplayID: -1},
		{DisplayID: -2},
	}
	for _, config := range invalid {
		if err := SetRuntimeConfig(config); err == nil {
			t.Fatalf("invalid config accepted: %+v", config)
		}
		if after := GetRuntimeConfig(); after != before {
			t.Fatalf("invalid config changed state: before=%+v after=%+v", before, after)
		}
	}
}
