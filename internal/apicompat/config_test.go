package apicompat

import (
	"slices"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Schema: ConfigSchemaVersion,
		Module: "example.test/module",
		ExcludedPackagePrefixes: []string{
			"example.test/module/examples",
		},
		Variants: []Variant{{
			Name:       "linux",
			GOOS:       "linux",
			GOARCH:     "amd64",
			CGOEnabled: false,
			Baseline:   "linux.api",
		}},
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "schema",
			mutate: func(cfg *Config) {
				cfg.Schema++
			},
		},
		{
			name: "module",
			mutate: func(cfg *Config) {
				cfg.Module = ""
			},
		},
		{
			name: "duplicate variant",
			mutate: func(cfg *Config) {
				cfg.Variants = append(cfg.Variants, cfg.Variants[0])
			},
		},
		{
			name: "escaping baseline",
			mutate: func(cfg *Config) {
				cfg.Variants[0].Baseline = "../linux.api"
			},
		},
		{
			name: "external exclusion",
			mutate: func(cfg *Config) {
				cfg.ExcludedPackagePrefixes = []string{"example.test/other"}
			},
		},
		{
			name: "missing delta base",
			mutate: func(cfg *Config) {
				cfg.Variants[0].Base = "missing.api"
				cfg.Variants[0].Baseline = "linux.delta"
			},
		},
		{
			name: "baseline used as full manifest and delta",
			mutate: func(cfg *Config) {
				cfg.Variants = append(
					cfg.Variants,
					Variant{
						Name:     "other-base",
						GOOS:     "linux",
						GOARCH:   "amd64",
						Baseline: "other.api",
					},
					Variant{
						Name:     "delta",
						GOOS:     "linux",
						GOARCH:   "amd64",
						Baseline: "linux.api",
						Base:     "other.api",
					},
				)
			},
		},
		{
			name: "unsorted build tags",
			mutate: func(cfg *Config) {
				cfg.Variants[0].Tags = []string{"wayland", "portal"}
			},
		},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			cfg.Variants = slices.Clone(valid.Variants)
			cfg.ExcludedPackagePrefixes = slices.Clone(valid.ExcludedPackagePrefixes)
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestSelectVariantsPreservesConfigOrder(t *testing.T) {
	t.Parallel()

	cfg := Config{Variants: []Variant{
		{Name: "first"},
		{Name: "second"},
		{Name: "third"},
	}}
	selected, err := cfg.SelectVariants([]string{"third", "first"})
	if err != nil {
		t.Fatalf("SelectVariants: %v", err)
	}
	if len(selected) != 2 || selected[0].Name != "first" || selected[1].Name != "third" {
		t.Fatalf("selected variants = %#v", selected)
	}
	if _, err := cfg.SelectVariants([]string{"missing"}); err == nil {
		t.Fatal("unknown variant accepted")
	}
	if _, err := cfg.SelectVariants([]string{"first", "first"}); err == nil {
		t.Fatal("duplicate request accepted")
	}
}

func TestBaselineOwner(t *testing.T) {
	t.Parallel()

	cfg := Config{Variants: []Variant{
		{Name: "native", Baseline: "native.api"},
		{Name: "feature", Baseline: "native.api"},
		{Name: "pure", Baseline: "pure.api"},
	}}
	if owner := cfg.BaselineOwner(cfg.Variants[1]); owner.Name != "native" {
		t.Fatalf("shared baseline owner = %q, want native", owner.Name)
	}
	if owner := cfg.BaselineOwner(cfg.Variants[2]); owner.Name != "pure" {
		t.Fatalf("unique baseline owner = %q, want pure", owner.Name)
	}
}
