package portalrunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	stateRootSentinel = ".robotgo-portal-runner-root"
	runSentinel       = ".robotgo-portal-runner-run"
)

// PrepareStateRoot creates or validates private runner storage outside the
// repository. Images may persist there; per-job overlays are separate runs.
func PrepareStateRoot(path, repositoryRoot string) error {
	if err := validateCleanAbsolutePath("portal runner state root", path); err != nil {
		return err
	}
	if err := validateCleanAbsolutePath("repository root", repositoryRoot); err != nil {
		return err
	}
	if err := ensureOutsideRepository(path, repositoryRoot); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create portal runner state root: %w", err)
	}
	if err := validatePrivateDirectory(path); err != nil {
		return err
	}
	return ensureSentinel(filepath.Join(path, stateRootSentinel))
}

// CreateRun creates one private direct child owned by the runner supervisor.
func CreateRun(stateRoot string) (string, error) {
	if err := validateStateRoot(stateRoot); err != nil {
		return "", err
	}
	runDirectory, err := os.MkdirTemp(stateRoot, "run-")
	if err != nil {
		return "", fmt.Errorf("create portal runner runtime: %w", err)
	}
	if err := os.Chmod(runDirectory, 0o700); err != nil {
		_ = os.RemoveAll(runDirectory)
		return "", fmt.Errorf("protect portal runner runtime: %w", err)
	}
	if err := ensureSentinel(filepath.Join(runDirectory, runSentinel)); err != nil {
		_ = os.RemoveAll(runDirectory)
		return "", err
	}
	return runDirectory, nil
}

// CleanupRun removes only a sentinel-owned direct child of the validated state
// root. Symlinks and unrelated paths fail closed.
func CleanupRun(stateRoot, runDirectory string) error {
	if err := validateStateRoot(stateRoot); err != nil {
		return err
	}
	if err := validateCleanAbsolutePath("portal runner runtime", runDirectory); err != nil {
		return err
	}
	if filepath.Dir(runDirectory) != stateRoot ||
		!strings.HasPrefix(filepath.Base(runDirectory), "run-") {
		return errors.New("portal runner runtime is not an owned direct child")
	}
	if err := validatePrivateDirectory(runDirectory); err != nil {
		return err
	}
	if err := validateSentinel(filepath.Join(runDirectory, runSentinel)); err != nil {
		return err
	}
	if err := os.RemoveAll(runDirectory); err != nil {
		return fmt.Errorf("remove portal runner runtime: %w", err)
	}
	return nil
}

func validateStateRoot(path string) error {
	if err := validateCleanAbsolutePath("portal runner state root", path); err != nil {
		return err
	}
	if err := validatePrivateDirectory(path); err != nil {
		return err
	}
	return validateSentinel(filepath.Join(path, stateRootSentinel))
}

func validateCleanAbsolutePath(name, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%s must be a clean absolute path", name)
	}
	return nil
}

func ensureOutsideRepository(stateRoot, repositoryRoot string) error {
	evaluatedRepository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	evaluatedParent, err := filepath.EvalSymlinks(filepath.Dir(stateRoot))
	if err != nil {
		return fmt.Errorf("resolve portal runner state parent: %w", err)
	}
	evaluatedState := filepath.Join(evaluatedParent, filepath.Base(stateRoot))
	relative, err := filepath.Rel(evaluatedRepository, evaluatedState)
	if err != nil {
		return fmt.Errorf("compare portal runner and repository roots: %w", err)
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return errors.New("portal runner state root must be outside the repository")
	}
	return nil
}

func validatePrivateDirectory(path string) error {
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve portal runner directory: %w", err)
	}
	if evaluated != path {
		return errors.New("portal runner directory must not traverse symlinks")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect portal runner directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("portal runner state is not a real directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("portal runner state must not be accessible by group or others")
	}
	return validateCurrentOwner(info)
}

func ensureSentinel(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return validateSentinel(path)
	}
	if err != nil {
		return fmt.Errorf("create portal runner ownership sentinel: %w", err)
	}
	return file.Close()
}

func validateSentinel(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("portal runner ownership sentinel is unavailable")
	}
	if !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 ||
		info.Size() != 0 {
		return errors.New("portal runner ownership sentinel is invalid")
	}
	return validateCurrentOwner(info)
}
