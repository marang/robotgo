package apicompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// ConfigSchemaVersion is the supported compatibility configuration schema.
	ConfigSchemaVersion = 1
)

// Config defines the module, exclusions, and build variants whose public APIs
// are frozen.
type Config struct {
	Schema                  int       `json:"schema"`
	Module                  string    `json:"module"`
	ExcludedPackagePrefixes []string  `json:"excludedPackagePrefixes"`
	Variants                []Variant `json:"variants"`
}

// Variant defines one build context and the baseline it must match.
type Variant struct {
	Name        string   `json:"name"`
	GOOS        string   `json:"goos"`
	GOARCH      string   `json:"goarch"`
	CGOEnabled  bool     `json:"cgoEnabled"`
	Tags        []string `json:"tags,omitempty"`
	Baseline    string   `json:"baseline"`
	Base        string   `json:"base,omitempty"`
	Description string   `json:"description"`
}

// LoadConfig reads and validates a compatibility configuration.
func LoadConfig(path string) (Config, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read API compatibility config: %w", err)
	}

	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode API compatibility config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("decode API compatibility config: trailing value")
		}
		return Config{}, fmt.Errorf("decode API compatibility config trailer: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate rejects ambiguous or unsafe compatibility configuration.
func (cfg Config) Validate() error {
	if cfg.Schema != ConfigSchemaVersion {
		return fmt.Errorf(
			"unsupported API compatibility config schema %d (want %d)",
			cfg.Schema,
			ConfigSchemaVersion,
		)
	}
	if strings.TrimSpace(cfg.Module) == "" {
		return errors.New("API compatibility module must not be empty")
	}
	if len(cfg.Variants) == 0 {
		return errors.New("API compatibility config must define variants")
	}

	seenNames := make(map[string]struct{}, len(cfg.Variants))
	baselineKinds := make(map[string]string, len(cfg.Variants))
	for _, variant := range cfg.Variants {
		if err := validateVariant(variant); err != nil {
			return err
		}
		if _, exists := seenNames[variant.Name]; exists {
			return fmt.Errorf("duplicate API compatibility variant %q", variant.Name)
		}
		seenNames[variant.Name] = struct{}{}

		if existingBase, exists := baselineKinds[variant.Baseline]; exists &&
			existingBase != variant.Base {
			return fmt.Errorf(
				"baseline %q is configured as both a full manifest and a delta",
				variant.Baseline,
			)
		}
		baselineKinds[variant.Baseline] = variant.Base
	}
	for index, variant := range cfg.Variants {
		if variant.Base == "" {
			continue
		}
		foundBase := false
		for _, candidate := range cfg.Variants[:index] {
			if candidate.Baseline == variant.Base && candidate.Base == "" {
				foundBase = true
				break
			}
		}
		if !foundBase {
			return fmt.Errorf(
				"variant %q base %q must reference an earlier full baseline",
				variant.Name,
				variant.Base,
			)
		}
	}

	for _, prefix := range cfg.ExcludedPackagePrefixes {
		if prefix == cfg.Module || !strings.HasPrefix(prefix, cfg.Module+"/") {
			return fmt.Errorf(
				"excluded package prefix %q is outside module %q",
				prefix,
				cfg.Module,
			)
		}
	}
	return nil
}

func validateVariant(variant Variant) error {
	if strings.TrimSpace(variant.Name) == "" {
		return errors.New("API compatibility variant name must not be empty")
	}
	if strings.TrimSpace(variant.GOOS) == "" {
		return fmt.Errorf("variant %q must define goos", variant.Name)
	}
	if strings.TrimSpace(variant.GOARCH) == "" {
		return fmt.Errorf("variant %q must define goarch", variant.Name)
	}
	if strings.TrimSpace(variant.Baseline) == "" {
		return fmt.Errorf("variant %q must define a baseline", variant.Name)
	}
	if err := validateRelativeArtifact(variant.Name, "baseline", variant.Baseline); err != nil {
		return err
	}
	if variant.Base != "" {
		if err := validateRelativeArtifact(variant.Name, "base", variant.Base); err != nil {
			return err
		}
		if variant.Base == variant.Baseline {
			return fmt.Errorf("variant %q delta cannot be its own base", variant.Name)
		}
	}
	if slices.Contains(variant.Tags, "") {
		return fmt.Errorf("variant %q contains an empty build tag", variant.Name)
	}
	if !slices.IsSorted(variant.Tags) {
		return fmt.Errorf("variant %q build tags must be sorted", variant.Name)
	}
	for index := 1; index < len(variant.Tags); index++ {
		if variant.Tags[index] == variant.Tags[index-1] {
			return fmt.Errorf(
				"variant %q contains duplicate build tag %q",
				variant.Name,
				variant.Tags[index],
			)
		}
	}
	return nil
}

func validateRelativeArtifact(variantName, kind, path string) error {
	if filepath.IsAbs(path) {
		return fmt.Errorf("variant %q %s must be relative", variantName, kind)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf(
			"variant %q %s escapes the config directory",
			variantName,
			kind,
		)
	}
	return nil
}

// SelectVariants resolves requested variant names in configuration order.
func (cfg Config) SelectVariants(names []string) ([]Variant, error) {
	if len(names) == 0 {
		return slices.Clone(cfg.Variants), nil
	}

	requested := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, duplicate := requested[name]; duplicate {
			return nil, fmt.Errorf("API compatibility variant %q requested twice", name)
		}
		requested[name] = struct{}{}
	}

	selected := make([]Variant, 0, len(names))
	for _, variant := range cfg.Variants {
		if _, ok := requested[variant.Name]; ok {
			selected = append(selected, variant)
			delete(requested, variant.Name)
		}
	}
	if len(requested) != 0 {
		unknown := make([]string, 0, len(requested))
		for name := range requested {
			unknown = append(unknown, name)
		}
		slices.Sort(unknown)
		return nil, fmt.Errorf("unknown API compatibility variants: %s", strings.Join(unknown, ", "))
	}
	return selected, nil
}

// BaselinePath returns a variant baseline relative to the configuration file.
func BaselinePath(configPath string, variant Variant) string {
	return filepath.Join(filepath.Dir(configPath), filepath.FromSlash(variant.Baseline))
}

// BasePath returns a delta variant's full baseline path.
func BasePath(configPath string, variant Variant) string {
	return filepath.Join(filepath.Dir(configPath), filepath.FromSlash(variant.Base))
}

// BaselineOwner returns the first configured variant for a shared baseline.
// Only that owner may update the baseline without also checking every selected
// alias variant.
func (cfg Config) BaselineOwner(variant Variant) Variant {
	for _, candidate := range cfg.Variants {
		if candidate.Baseline == variant.Baseline {
			return candidate
		}
	}
	return variant
}
