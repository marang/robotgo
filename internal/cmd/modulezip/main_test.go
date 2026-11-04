package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func archiveFixture(t *testing.T, modulePath string, extra map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	files := map[string]string{
		"go.mod":     "module " + modulePath + "\n\ngo 1.26.10\n",
		"version.go": "package robotgo\nconst Version = \"v1.0.4\"\n",
	}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestPackageModuleArtifacts(t *testing.T) {
	data := archiveFixture(t, identity, nil)
	root := t.TempDir()
	for _, name := range []string{"first", "retry"} {
		if err := packageModule(bytes.NewReader(data), "v1.0.4", "2026-11-04T12:00:00+01:00", filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"go-module.info", "go-module.mod", "go-module.zip"} {
		a, err := os.ReadFile(filepath.Join(root, "first", name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, "retry", name))
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("non-deterministic %s: %v", name, err)
		}
	}
	infoBytes, err := os.ReadFile(filepath.Join(root, "first", "go-module.info"))
	if err != nil {
		t.Fatal(err)
	}
	var info struct{ Version, Time string }
	if err := json.Unmarshal(infoBytes, &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.0.4" || info.Time != "2026-11-04T11:00:00Z" {
		t.Fatalf("unexpected info: %+v", info)
	}
	archive, err := zip.OpenReader(filepath.Join(root, "first", "go-module.zip"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := archive.Close(); err != nil {
			t.Errorf("close module archive: %v", err)
		}
	})
	if len(archive.File) != 2 {
		t.Fatalf("unexpected file count: %d", len(archive.File))
	}
	for _, file := range archive.File {
		if file.Name != identity+"@v1.0.4/go.mod" && file.Name != identity+"@v1.0.4/version.go" {
			t.Fatalf("unexpected ZIP member: %s", file.Name)
		}
	}
	if err := packageModule(bytes.NewReader(data), "v1.0.4", info.Time, filepath.Join(root, "first")); err == nil {
		t.Fatal("overwrote existing artifact directory")
	}
}

func TestPackageModuleRejectsInvalidSource(t *testing.T) {
	for _, tc := range []struct {
		name, version, timestamp, module string
		extra                            map[string]string
	}{
		{name: "identity", version: "v1.0.4", timestamp: "2026-11-04T11:00:00Z", module: "example.invalid/wrong"},
		{name: "major", version: "v2.0.0", timestamp: "2026-11-04T11:00:00Z", module: identity},
		{name: "source version", version: "v1.0.5", timestamp: "2026-11-04T11:00:00Z", module: identity},
		{name: "short version", version: "v1.0", timestamp: "2026-11-04T11:00:00Z", module: identity},
		{name: "time", version: "v1.0.4", timestamp: "yesterday", module: identity},
		{name: "traversal", version: "v1.0.4", timestamp: "2026-11-04T11:00:00Z", module: identity, extra: map[string]string{"../escape.go": "package escape"}},
		{name: "case collision", version: "v1.0.4", timestamp: "2026-11-04T11:00:00Z", module: identity, extra: map[string]string{"Version.go": "package robotgo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "artifacts")
			data := archiveFixture(t, tc.module, tc.extra)
			if err := packageModule(bytes.NewReader(data), tc.version, tc.timestamp, out); err == nil {
				t.Fatal("accepted invalid release input")
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("partial output remains: %v", err)
			}
		})
	}
}
