//go:build linux

// Fixed static entrypoint: sanitize before any dynamic loader or Python startup.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Offline builder seals only driverSHA256; identity and paths are not options.
const ownerUID = 7051508
const ownerGID = 1004
const ownerHome = "/home/izoone_workers"
const driverPath = ownerHome + "/.local/libexec/robotgo-ci.py"

var driverSHA256 string

func refuse() {
	var id [16]byte
	_, _ = rand.Read(id[:])
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status": "failed", "task": nil, "commit": nil,
		"duration_seconds": 0, "run_id": hex.EncodeToString(id[:]),
	})
	os.Exit(1)
}

func main() {
	if os.Getuid() != ownerUID || os.Geteuid() != ownerUID || os.Getgid() != ownerGID || len(driverSHA256) != 64 {
		refuse()
	}
	for parent := filepath.Dir(driverPath); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			refuse()
		}
		metadata := info.Sys().(*syscall.Stat_t)
		if metadata.Uid != 0 && metadata.Uid != ownerUID {
			refuse()
		}
		if parent == "/" {
			break
		}
	}
	// Retain the verified descriptor through exec; path replacement cannot swap
	// the driver between its digest check and Python opening the same bytes.
	fd, err := syscall.Open(driverPath, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		refuse()
	}
	file := os.NewFile(uintptr(fd), driverPath)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0700 || info.Size() > 1024*1024 {
		refuse()
	}
	metadata := info.Sys().(*syscall.Stat_t)
	if metadata.Uid != ownerUID || metadata.Nlink != 1 {
		refuse()
	}
	content, err := os.ReadFile("/proc/self/fd/" + strconv.Itoa(fd))
	if err != nil {
		refuse()
	}
	hash := sha256.Sum256(content)
	if hex.EncodeToString(hash[:]) != driverSHA256 {
		refuse()
	}
	env := []string{
		"HOME=" + ownerHome, "PATH=/usr/local/bin:/usr/bin:/bin",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"XDG_CONFIG_HOME=" + filepath.Join(ownerHome, ".config"),
		"XDG_RUNTIME_DIR=/run/user/7051508",
	}
	args := append([]string{"/usr/bin/python3", "-I", "/proc/self/fd/" + strconv.Itoa(fd)}, os.Args[1:]...)
	if syscall.Exec("/usr/bin/python3", args, env) != nil {
		refuse()
	}
}
