//go:build linux

package portalrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maximumArtifactBytes = 2 * 1024 * 1024 * 1024
	imageBuildTimeout    = 45 * time.Minute
	sshReadyTimeout      = 5 * time.Minute
	sshProbeInterval     = 2 * time.Second
	qemuStopTimeout      = 2 * time.Minute
	buildLogLimit        = 32 * 1024 * 1024
	maximumBuildInput    = 1024 * 1024
	imageIdentitySchema  = "1"
)

var guestImageFiles = []string{
	"guest/configure-egress.sh",
	"guest/install.sh",
	"guest/job-completed.sh",
	"guest/job-started.sh",
	"guest/register.sh",
	"guest/wait-session.sh",
}

type imageBuildInput struct {
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256"`
}

type imageBuildMetadata struct {
	Schema         string            `json:"schema"`
	ID             string            `json:"id"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	Inputs         []imageBuildInput `json:"inputs"`
}

// CommandExecutor runs one host-side provisioning command without exposing
// command arguments in returned errors.
type CommandExecutor interface {
	Run(
		ctx context.Context,
		name string,
		args []string,
		stdin io.Reader,
		output io.Writer,
	) error
}

type systemCommandExecutor struct{}

func (systemCommandExecutor) Run(
	ctx context.Context,
	name string,
	args []string,
	stdin io.Reader,
	output io.Writer,
) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 5 * time.Second
	err := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("provisioning command %q failed", filepath.Base(name))
	}
	return nil
}

// ImageBuildOptions defines one immutable portal-runner image build.
type ImageBuildOptions struct {
	ManifestPath   string
	RepositoryRoot string
	StateRoot      string
	GuestFiles     string
	SSHPort        int
	Output         io.Writer
	HTTPClient     *http.Client
	Commands       CommandExecutor
}

// BuildImage creates or reuses the image identified by the exact manifest
// digest. It returns a path under the external private state root.
func BuildImage(
	ctx context.Context,
	options ImageBuildOptions,
) (imagePath string, returnError error) {
	manifest, err := LoadManifest(options.ManifestPath)
	if err != nil {
		return "", err
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Commands == nil {
		options.Commands = systemCommandExecutor{}
	}
	if options.HTTPClient == nil {
		options.HTTPClient = secureHTTPClient()
	}
	if options.SSHPort < 1024 || options.SSHPort > 65535 {
		return "", errors.New("portal runner SSH port must be unprivileged")
	}
	if err := PrepareStateRoot(options.StateRoot, options.RepositoryRoot); err != nil {
		return "", err
	}
	stateLock, err := AcquireStateLock(options.StateRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = stateLock.Close() }()
	if err := validateGuestFiles(options.GuestFiles); err != nil {
		return "", err
	}
	for _, executable := range []string{
		"cloud-localds",
		"qemu-img",
		"qemu-system-x86_64",
		"scp",
		"ssh",
		"ssh-keygen",
	} {
		if _, err := exec.LookPath(executable); err != nil {
			return "", fmt.Errorf("required portal runner host command %q is unavailable", executable)
		}
	}

	imageID, buildMetadata, _, err := computeImageIdentity(
		options.ManifestPath,
		options.RepositoryRoot,
		options.GuestFiles,
	)
	if err != nil {
		return "", err
	}

	imagesDirectory := filepath.Join(options.StateRoot, "images")
	if err := preparePersistentDirectory(imagesDirectory); err != nil {
		return "", err
	}
	baseImage := filepath.Join(
		imagesDirectory,
		"ubuntu-"+manifest.BaseImage.SHA256+".qcow2",
	)
	if err := writeStatus(
		options.Output,
		"artifact verify version=%s\n",
		manifest.BaseImage.Version,
	); err != nil {
		return "", err
	}
	if err := ensureArtifact(
		ctx,
		options.HTTPClient,
		manifest.BaseImage,
		baseImage,
	); err != nil {
		return "", err
	}

	finalImage := filepath.Join(
		imagesDirectory,
		manifest.Lane+"-"+imageID+".qcow2",
	)
	finalManifest := finalImage + ".build.json"
	if reusableImage(finalImage, finalManifest, buildMetadata) {
		if err := removeStaleImages(
			imagesDirectory,
			manifest.Lane,
			finalImage,
			finalManifest,
		); err != nil {
			return "", err
		}
		if err := writeStatus(
			options.Output,
			"image ready id=%s reused=true\n",
			imageID[:16],
		); err != nil {
			return "", err
		}
		return finalImage, nil
	}
	if _, err := os.Lstat(finalImage); err == nil {
		return "", errors.New("portal runner image exists without matching manifest")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect portal runner image: %w", err)
	}

	runDirectory, err := CreateRun(options.StateRoot)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := CleanupRun(options.StateRoot, runDirectory); err != nil {
			imagePath = ""
			returnError = errors.Join(returnError, err)
		}
	}()
	buildContext, cancel := context.WithTimeout(ctx, imageBuildTimeout)
	defer cancel()

	logPath := filepath.Join(runDirectory, "build.log")
	logFile, err := os.OpenFile(
		logPath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("create portal runner build log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	logWriter := &boundedWriter{destination: logFile, remaining: buildLogLimit}

	if err := writeStatus(options.Output, "image overlay create\n"); err != nil {
		return "", err
	}
	overlay := filepath.Join(runDirectory, manifest.Lane+".qcow2")
	if err := options.Commands.Run(
		buildContext,
		"qemu-img",
		[]string{
			"create",
			"-f", "qcow2",
			"-F", "qcow2",
			"-b", baseImage,
			overlay,
			strconv.Itoa(manifest.VM.DiskGiB) + "G",
		},
		nil,
		logWriter,
	); err != nil {
		return "", err
	}

	privateKey := filepath.Join(runDirectory, "build-ssh")
	if err := options.Commands.Run(
		buildContext,
		"ssh-keygen",
		[]string{"-q", "-t", "ed25519", "-N", "", "-f", privateKey},
		nil,
		logWriter,
	); err != nil {
		return "", err
	}
	publicKey, err := os.ReadFile(privateKey + ".pub")
	if err != nil {
		return "", fmt.Errorf("read ephemeral build public key: %w", err)
	}
	instanceID, err := randomIdentifier()
	if err != nil {
		return "", err
	}
	userData := filepath.Join(runDirectory, "user-data")
	metaData := filepath.Join(runDirectory, "meta-data")
	networkData := filepath.Join(runDirectory, "network-config")
	seedImage := filepath.Join(runDirectory, "seed.img")
	cloudConfig, err := buildCloudConfig(strings.TrimSpace(string(publicKey)))
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(
		userData,
		[]byte(cloudConfig),
		0o600,
	); err != nil {
		return "", fmt.Errorf("write portal runner build cloud config: %w", err)
	}
	if err := os.WriteFile(
		metaData,
		[]byte(
			"instance-id: "+instanceID+
				"\nlocal-hostname: robotgo-"+manifest.Lane+"-build\n",
		),
		0o600,
	); err != nil {
		return "", fmt.Errorf("write portal runner build metadata: %w", err)
	}
	if err := os.WriteFile(
		networkData,
		[]byte(runtimeNetworkConfig()),
		0o600,
	); err != nil {
		return "", fmt.Errorf("write portal runner build network config: %w", err)
	}
	if err := options.Commands.Run(
		buildContext,
		"cloud-localds",
		[]string{"-N", networkData, seedImage, userData, metaData},
		nil,
		logWriter,
	); err != nil {
		return "", err
	}

	pidFile := filepath.Join(runDirectory, "qemu.pid")
	serialLog := filepath.Join(runDirectory, "serial.log")
	serialFile, err := os.OpenFile(
		serialLog,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("create portal runner serial log: %w", err)
	}
	if err := serialFile.Close(); err != nil {
		return "", fmt.Errorf("close portal runner serial log: %w", err)
	}
	if err := writeStatus(options.Output, "image guest boot\n"); err != nil {
		return "", err
	}
	if err := options.Commands.Run(
		buildContext,
		"qemu-system-x86_64",
		buildQEMUArguments(
			manifest,
			overlay,
			seedImage,
			pidFile,
			serialLog,
			options.SSHPort,
			true,
		),
		nil,
		logWriter,
	); err != nil {
		return "", err
	}
	qemuPID, err := readPID(pidFile)
	if err != nil {
		return "", err
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = syscall.Kill(qemuPID, syscall.SIGKILL)
		}
	}()

	sshArguments := []string{
		"-i", privateKey,
		"-p", strconv.Itoa(options.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + filepath.Join(runDirectory, "known-hosts"),
		"-o", "ConnectTimeout=5",
	}
	if err := waitForSSH(
		buildContext,
		options.Commands,
		sshArguments,
		logWriter,
	); err != nil {
		return "", err
	}
	if err := writeStatus(options.Output, "image guest ready\n"); err != nil {
		return "", err
	}
	if err := options.Commands.Run(
		buildContext,
		"ssh",
		append(
			append([]string{}, sshArguments...),
			"root@127.0.0.1",
			"install -d -m 0700 /root/robotgo-runner-build",
		),
		nil,
		logWriter,
	); err != nil {
		return "", err
	}
	remoteRoot := "root@127.0.0.1:/root/robotgo-runner-build"
	scpArguments := append([]string{}, sshArguments...)
	scpArguments = replaceSSHPortFlag(scpArguments)
	scpArguments = append(
		scpArguments,
		"-r",
		filepath.Join(options.GuestFiles, "guest"),
		options.ManifestPath,
		remoteRoot,
	)
	if err := options.Commands.Run(
		buildContext,
		"scp",
		scpArguments,
		nil,
		logWriter,
	); err != nil {
		return "", err
	}

	installCommand := strings.Join([]string{
		"set -euo pipefail",
		"test -x /root/robotgo-runner-build/guest/install.sh",
		"/root/robotgo-runner-build/guest/install.sh /root/robotgo-runner-build/manifest.json",
		"test -f /var/lib/robotgo-runner/image-ready",
		"echo ROBOTGO_IMAGE_READY",
		"systemctl poweroff --no-wall",
	}, " && ")
	if err := writeStatus(options.Output, "image guest install\n"); err != nil {
		return "", err
	}
	var installOutput bytes.Buffer
	combinedOutput := io.MultiWriter(logWriter, &installOutput)
	if err := options.Commands.Run(
		buildContext,
		"ssh",
		append(sshArguments, "root@127.0.0.1", installCommand),
		nil,
		combinedOutput,
	); err != nil && !strings.Contains(installOutput.String(), "ROBOTGO_IMAGE_READY") {
		return "", err
	}
	if !strings.Contains(installOutput.String(), "ROBOTGO_IMAGE_READY") {
		return "", errors.New("portal runner guest did not confirm image completion")
	}
	if err := waitForProcessExit(buildContext, qemuPID, qemuStopTimeout); err != nil {
		return "", err
	}
	stopped = true

	if err := os.Chmod(overlay, 0o600); err != nil {
		return "", fmt.Errorf("protect portal runner image: %w", err)
	}
	if err := os.Rename(overlay, finalImage); err != nil {
		return "", fmt.Errorf("publish portal runner image: %w", err)
	}
	if err := writeAtomic(finalManifest, buildMetadata, 0o600); err != nil {
		_ = os.Remove(finalImage)
		return "", err
	}
	if err := removeStaleImages(
		imagesDirectory,
		manifest.Lane,
		finalImage,
		finalManifest,
	); err != nil {
		return "", err
	}
	if err := writeStatus(
		options.Output,
		"image ready id=%s reused=false\n",
		imageID[:16],
	); err != nil {
		return "", err
	}
	return finalImage, nil
}

func writeStatus(output io.Writer, format string, arguments ...any) error {
	if _, err := fmt.Fprintf(output, format, arguments...); err != nil {
		return errors.New("write portal runner status")
	}
	return nil
}

func removeStaleImages(
	directory,
	lane,
	currentImage,
	currentMetadata string,
) error {
	if lane != portalLaneGNOME && lane != portalLaneKDE {
		return errors.New("portal runner stale-image lane is invalid")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("list portal runner images: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		prefix := lane + "-"
		if !strings.HasPrefix(name, prefix) ||
			(!strings.HasSuffix(name, ".qcow2") &&
				!strings.HasSuffix(name, ".qcow2.build.json")) {
			continue
		}
		digest := strings.TrimPrefix(name, prefix)
		digest = strings.TrimSuffix(digest, ".build.json")
		digest = strings.TrimSuffix(digest, ".qcow2")
		decoded, decodeErr := hex.DecodeString(digest)
		if decodeErr != nil ||
			len(decoded) != sha256.Size ||
			hex.EncodeToString(decoded) != digest {
			return fmt.Errorf("portal runner image name %q is invalid", name)
		}
		path := filepath.Join(directory, name)
		if path == currentImage || path == currentMetadata {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect stale portal runner image: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("stale portal runner image is not a regular file")
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale portal runner image: %w", err)
		}
	}
	return nil
}

func secureHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          2,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) >= 3 ||
				len(previous) == 0 ||
				request.URL.Scheme != "https" ||
				request.URL.Hostname() != previous[0].URL.Hostname() {
				return errors.New("artifact redirect left pinned HTTPS origin")
			}
			return nil
		},
	}
}

func ensureArtifact(
	ctx context.Context,
	client *http.Client,
	artifact Artifact,
	path string,
) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("cached portal runner artifact is not a regular file")
		}
		return verifyFileDigest(path, artifact.SHA256)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect cached portal runner artifact: %w", err)
	}

	temporary, err := os.OpenFile(
		path+".partial",
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create portal runner artifact download: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return fmt.Errorf("create portal runner artifact request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download portal runner artifact: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("portal runner artifact source returned a non-success status")
	}
	if response.ContentLength < 0 || response.ContentLength > maximumArtifactBytes {
		return errors.New("portal runner artifact length is invalid")
	}
	hasher := sha256.New()
	written, err := io.Copy(
		io.MultiWriter(temporary, hasher),
		io.LimitReader(response.Body, maximumArtifactBytes+1),
	)
	if err != nil {
		return fmt.Errorf("store portal runner artifact: %w", err)
	}
	if written != response.ContentLength || written > maximumArtifactBytes {
		return errors.New("portal runner artifact length changed during download")
	}
	if hex.EncodeToString(hasher.Sum(nil)) != artifact.SHA256 {
		return errors.New("portal runner artifact SHA-256 mismatch")
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync portal runner artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close portal runner artifact: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish portal runner artifact: %w", err)
	}
	return nil
}

func verifyFileDigest(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect cached portal runner artifact: %w", err)
	}
	if !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 0 ||
		info.Size() > maximumArtifactBytes {
		return errors.New("cached portal runner artifact is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open cached portal runner artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, io.LimitReader(file, maximumArtifactBytes+1)); err != nil {
		return fmt.Errorf("hash cached portal runner artifact: %w", err)
	}
	if hex.EncodeToString(hasher.Sum(nil)) != expected {
		return errors.New("cached portal runner artifact SHA-256 mismatch")
	}
	return nil
}

func preparePersistentDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create portal runner persistent directory: %w", err)
	}
	return validatePrivateDirectory(path)
}

func validateGuestFiles(path string) error {
	if err := validateCleanAbsolutePath("portal runner guest files", path); err != nil {
		return err
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil || evaluated != path {
		return errors.New("portal runner guest files must be a real absolute directory")
	}
	expected := make(map[string]bool, len(guestImageFiles))
	for _, relative := range guestImageFiles {
		expected[filepath.FromSlash(relative)] = false
	}
	err = filepath.WalkDir(
		filepath.Join(path, "guest"),
		func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("inspect portal runner guest files: %w", walkErr)
			}
			if current == filepath.Join(path, "guest") {
				if !entry.IsDir() {
					return errors.New("portal runner guest path is not a directory")
				}
				return nil
			}
			relative, err := filepath.Rel(path, current)
			if err != nil {
				return fmt.Errorf("resolve portal runner guest file: %w", err)
			}
			if entry.IsDir() {
				return fmt.Errorf(
					"portal runner guest directory %q is unexpected",
					filepath.ToSlash(relative),
				)
			}
			info, err := entry.Info()
			if err != nil ||
				!info.Mode().IsRegular() ||
				info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf(
					"portal runner guest file %q is invalid",
					filepath.ToSlash(relative),
				)
			}
			if _, ok := expected[relative]; !ok {
				return fmt.Errorf(
					"portal runner guest file %q is not identity-bound",
					filepath.ToSlash(relative),
				)
			}
			expected[relative] = true
			return nil
		},
	)
	if err != nil {
		return err
	}
	for relative, found := range expected {
		if !found {
			return fmt.Errorf(
				"portal runner guest file %q is unavailable",
				filepath.ToSlash(relative),
			)
		}
	}
	return nil
}

func computeImageIdentity(
	manifestPath,
	repositoryRoot,
	guestFiles string,
) (string, []byte, []byte, error) {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf(
			"read portal runner manifest for image identity: %w",
			err,
		)
	}
	if len(manifestData) > maxManifestBytes {
		return "", nil, nil, errors.New("portal runner manifest exceeds size limit")
	}
	manifestDigest := sha256.Sum256(manifestData)
	inputPaths := append(
		[]string{"internal/portalrunner/image_linux.go"},
		guestImageFiles...,
	)
	inputs := make([]imageBuildInput, 0, len(inputPaths))
	hasher := sha256.New()
	hashIdentityField(hasher, []byte(imageIdentitySchema))
	hashIdentityField(hasher, manifestData)
	for _, relative := range inputPaths {
		var absolute string
		if strings.HasPrefix(relative, "guest/") {
			absolute = filepath.Join(guestFiles, relative)
		} else {
			absolute = filepath.Join(repositoryRoot, relative)
		}
		info, err := os.Lstat(absolute)
		if err != nil ||
			!info.Mode().IsRegular() ||
			info.Mode()&os.ModeSymlink != 0 {
			return "", nil, nil, fmt.Errorf(
				"portal runner image input %q is unavailable",
				relative,
			)
		}
		if info.Size() < 0 || info.Size() > maximumBuildInput {
			return "", nil, nil, fmt.Errorf(
				"portal runner image input %q exceeds size limit",
				relative,
			)
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return "", nil, nil, fmt.Errorf(
				"read portal runner image input %q: %w",
				relative,
				err,
			)
		}
		digest := sha256.Sum256(data)
		mode := uint32(info.Mode().Perm())
		hashIdentityField(hasher, []byte(relative))
		hashIdentityField(hasher, []byte(strconv.FormatUint(uint64(mode), 8)))
		hashIdentityField(hasher, data)
		inputs = append(inputs, imageBuildInput{
			Path:   relative,
			Mode:   mode,
			SHA256: hex.EncodeToString(digest[:]),
		})
	}
	imageID := hex.EncodeToString(hasher.Sum(nil))
	metadata, err := json.MarshalIndent(imageBuildMetadata{
		Schema:         imageIdentitySchema,
		ID:             imageID,
		ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
		Inputs:         inputs,
	}, "", "  ")
	if err != nil {
		return "", nil, nil, fmt.Errorf("encode portal runner image identity: %w", err)
	}
	metadata = append(metadata, '\n')
	return imageID, metadata, manifestData, nil
}

func hashIdentityField(destination hash.Hash, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func reusableImage(path, metadataPath string, metadata []byte) bool {
	imageInfo, imageErr := os.Lstat(path)
	storedMetadata, metadataErr := os.ReadFile(metadataPath)
	return imageErr == nil &&
		imageInfo.Mode().IsRegular() &&
		imageInfo.Mode()&os.ModeSymlink == 0 &&
		imageInfo.Mode().Perm()&0o077 == 0 &&
		metadataErr == nil &&
		bytes.Equal(storedMetadata, metadata)
}

func randomIdentifier() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate portal runner instance identity: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func buildCloudConfig(publicKey string) (string, error) {
	fields := strings.Fields(publicKey)
	if len(fields) < 2 ||
		!strings.HasPrefix(fields[0], "ssh-") ||
		fields[1] == "" {
		return "", errors.New("ephemeral SSH public key is invalid")
	}
	keyWithoutComment := fields[0] + " " + fields[1]
	return "#cloud-config\n" +
		"disable_root: false\n" +
		"no_ssh_fingerprints: true\n" +
		"ssh:\n" +
		"  emit_keys_to_console: false\n" +
		"ssh_pwauth: false\n" +
		"ssh_quiet_keygen: true\n" +
		"ssh_publish_hostkeys:\n" +
		"  enabled: false\n" +
		"users:\n" +
		"  - default\n" +
		"  - name: root\n" +
		"    ssh_authorized_keys:\n" +
		"      - " + keyWithoutComment + "\n", nil
}

func runtimeNetworkConfig() string {
	return "version: 2\n" +
		"ethernets:\n" +
		"  ens3:\n" +
		"    renderer: networkd\n" +
		"    dhcp4: true\n" +
		"    dhcp6: false\n"
}

func buildQEMUArguments(
	manifest Manifest,
	disk,
	seed,
	pidFile,
	serialLog string,
	sshPort int,
	headless bool,
) []string {
	arguments := []string{
		"-name", "robotgo-" + manifest.Lane,
		"-enable-kvm",
		"-cpu", "host",
		"-smp", strconv.Itoa(manifest.VM.CPUs),
		"-m", strconv.Itoa(manifest.VM.MemoryMiB),
		"-nodefaults",
		"-no-reboot",
		"-device", "virtio-rng-pci",
		"-drive", "file=" + disk + ",if=virtio,format=qcow2,cache=none",
		"-drive", "file=" + seed + ",if=virtio,format=raw,readonly=on",
		"-netdev",
		"user,id=network,hostfwd=tcp:127.0.0.1:" +
			strconv.Itoa(sshPort) + "-:22",
		"-device", "virtio-net-pci,netdev=network",
		"-serial", "file:" + serialLog,
		"-pidfile", pidFile,
	}
	if headless {
		arguments = append(
			arguments,
			"-daemonize",
			"-device",
			"bochs-display",
			"-display",
			"none",
		)
	} else {
		arguments = append(
			arguments,
			"-device",
			"virtio-vga",
			"-device",
			"qemu-xhci",
			"-device",
			"usb-kbd",
			"-device",
			"usb-tablet",
			"-display",
			"gtk,gl=off,show-cursor=on,grab-on-hover=on",
		)
	}
	return arguments
}

func replaceSSHPortFlag(arguments []string) []string {
	replaced := append([]string{}, arguments...)
	for index := range replaced {
		if replaced[index] == "-p" {
			replaced[index] = "-P"
			break
		}
	}
	return replaced
}

func waitForSSH(
	ctx context.Context,
	commands CommandExecutor,
	sshArguments []string,
	output io.Writer,
) error {
	deadline := time.Now().Add(sshReadyTimeout)
	for {
		probeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := commands.Run(
			probeContext,
			"ssh",
			append(append([]string{}, sshArguments...), "root@127.0.0.1", "true"),
			nil,
			output,
		)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("portal runner guest SSH did not become ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sshProbeInterval):
		}
	}
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read portal runner VM PID: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, errors.New("portal runner VM PID is invalid")
	}
	return pid, nil
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("probe portal runner VM process: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("portal runner VM did not stop within cleanup deadline")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.OpenFile(
		path+".partial",
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		mode,
	)
	if err != nil {
		return fmt.Errorf("create portal runner metadata: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write portal runner metadata: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync portal runner metadata: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close portal runner metadata: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish portal runner metadata: %w", err)
	}
	return nil
}

type boundedWriter struct {
	destination io.Writer
	remaining   int64
	mutex       sync.Mutex
}

func (writer *boundedWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if int64(len(data)) > writer.remaining {
		return 0, errors.New("portal runner log exceeded safety limit")
	}
	written, err := writer.destination.Write(data)
	writer.remaining -= int64(written)
	return written, err
}
