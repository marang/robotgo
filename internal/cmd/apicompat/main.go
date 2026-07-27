package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/marang/robotgo/internal/apicompat"
)

const (
	defaultConfigPath = "api/compat/config.json"
	defaultRoot       = "."
)

type stringFlags []string

func (values *stringFlags) String() string {
	return fmt.Sprint([]string(*values))
}

func (values *stringFlags) Set(value string) error {
	if value == "" {
		return errors.New("variant must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	var variants stringFlags
	configPath := flag.String("config", defaultConfigPath, "compatibility config path")
	root := flag.String("root", defaultRoot, "module root")
	write := flag.Bool("write", false, "replace selected checked-in baselines")
	flag.Var(&variants, "variant", "variant to check or write (repeatable; default all)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, *root, *configPath, variants, *write); err != nil {
		fmt.Fprintln(os.Stderr, "api compatibility:", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	root string,
	configPath string,
	requested []string,
	write bool,
) error {
	cfg, err := apicompat.LoadConfig(configPath)
	if err != nil {
		return err
	}
	variants, err := cfg.SelectVariants(requested)
	if err != nil {
		return err
	}

	if write {
		return writeBaselines(ctx, root, configPath, cfg, variants)
	}
	for _, variant := range variants {
		if err := checkVariant(ctx, root, configPath, cfg, variant); err != nil {
			return err
		}
		fmt.Printf("%s: public API matches %s\n", variant.Name, variant.Baseline)
	}
	return nil
}

func checkVariant(
	ctx context.Context,
	root string,
	configPath string,
	cfg apicompat.Config,
	variant apicompat.Variant,
) error {
	current, err := apicompat.Snapshot(ctx, root, cfg, variant)
	if err != nil {
		return err
	}
	baseline, err := readExpectedManifest(configPath, variant)
	if err != nil {
		return err
	}
	if err := apicompat.Compare(baseline, current); err != nil {
		return fmt.Errorf(
			"%s: %w\nrun go run ./internal/cmd/apicompat -write -variant %s and review the baseline diff",
			variant.Name,
			err,
			variant.Name,
		)
	}
	return nil
}

func readExpectedManifest(
	configPath string,
	variant apicompat.Variant,
) (apicompat.Manifest, error) {
	if variant.Base == "" {
		return readManifest(
			apicompat.BaselinePath(configPath, variant),
			variant.Name,
		)
	}

	base, err := readManifest(apicompat.BasePath(configPath, variant), variant.Name)
	if err != nil {
		return apicompat.Manifest{}, err
	}
	deltaPath := apicompat.BaselinePath(configPath, variant)
	body, err := os.ReadFile(deltaPath)
	if err != nil {
		return apicompat.Manifest{}, fmt.Errorf(
			"read %s delta %s: %w",
			variant.Name,
			deltaPath,
			err,
		)
	}
	delta, err := apicompat.ParseDelta(string(body))
	if err != nil {
		return apicompat.Manifest{}, fmt.Errorf(
			"parse %s delta %s: %w",
			variant.Name,
			deltaPath,
			err,
		)
	}
	if delta.Base != variant.Base {
		return apicompat.Manifest{}, fmt.Errorf(
			"%s delta names base %s, want %s",
			variant.Name,
			delta.Base,
			variant.Base,
		)
	}
	expected, err := apicompat.ApplyDelta(base, delta)
	if err != nil {
		return apicompat.Manifest{}, fmt.Errorf(
			"apply %s delta %s: %w",
			variant.Name,
			deltaPath,
			err,
		)
	}
	return expected, nil
}

func readManifest(path, variantName string) (apicompat.Manifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return apicompat.Manifest{}, fmt.Errorf(
			"read %s baseline %s: %w",
			variantName,
			path,
			err,
		)
	}
	manifest, err := apicompat.ParseManifest(string(body))
	if err != nil {
		return apicompat.Manifest{}, fmt.Errorf(
			"parse %s baseline %s: %w",
			variantName,
			path,
			err,
		)
	}
	return manifest, nil
}

func writeBaselines(
	ctx context.Context,
	root string,
	configPath string,
	cfg apicompat.Config,
	variants []apicompat.Variant,
) error {
	renderedByPath := make(map[string]string, len(variants))
	fullByPath := make(map[string]apicompat.Manifest, len(variants))
	if err := validateBaselineWriteSelection(cfg, variants); err != nil {
		return err
	}
	for _, variant := range variants {
		manifest, err := apicompat.Snapshot(ctx, root, cfg, variant)
		if err != nil {
			return err
		}
		path := apicompat.BaselinePath(configPath, variant)
		var rendered string
		if variant.Base != "" {
			basePath := apicompat.BasePath(configPath, variant)
			base, exists := fullByPath[basePath]
			if !exists {
				base, err = readManifest(basePath, variant.Name)
				if err != nil {
					return err
				}
			}
			delta, err := apicompat.ManifestDelta(base, manifest, variant.Base)
			if err != nil {
				return fmt.Errorf("generate %s delta: %w", variant.Name, err)
			}
			rendered = delta.Render()
		} else {
			if existing, duplicate := fullByPath[path]; duplicate {
				if err := apicompat.Compare(existing, manifest); err != nil {
					return fmt.Errorf(
						"variants sharing baseline %s expose different public APIs: %w",
						path,
						err,
					)
				}
				rendered = existing.Render()
			} else {
				fullByPath[path] = manifest
				rendered = manifest.Render()
			}
		}
		if existing, duplicate := renderedByPath[path]; duplicate && existing != rendered {
			return fmt.Errorf(
				"variants sharing baseline %s expose different public APIs",
				path,
			)
		}
		renderedByPath[path] = rendered
		fmt.Printf("%s: generated %s\n", variant.Name, variant.Baseline)
	}

	paths := make([]string, 0, len(renderedByPath))
	for path := range renderedByPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		rendered := renderedByPath[path]
		if err := writeFileAtomic(path, []byte(rendered)); err != nil {
			return err
		}
	}
	return nil
}

func validateBaselineWriteSelection(
	cfg apicompat.Config,
	variants []apicompat.Variant,
) error {
	selected := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		selected[variant.Name] = struct{}{}
	}
	for _, variant := range variants {
		owner := cfg.BaselineOwner(variant)
		if owner.Name != variant.Name {
			if _, ownerSelected := selected[owner.Name]; !ownerSelected {
				return fmt.Errorf(
					"variant %s shares %s owned by %s; write the owner or select both variants",
					variant.Name,
					variant.Baseline,
					owner.Name,
				)
			}
		}
	}
	return nil
}

func writeFileAtomic(path string, body []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create baseline directory %s: %w", directory, err)
	}
	temp, err := os.CreateTemp(directory, ".apicompat-*")
	if err != nil {
		return fmt.Errorf("create temporary baseline for %s: %w", path, err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set temporary baseline mode: %w", err)
	}
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary baseline: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary baseline: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace baseline %s: %w", path, err)
	}
	return nil
}
