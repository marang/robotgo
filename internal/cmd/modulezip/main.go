// Command modulezip prepares immutable module artifacts from a committed Git ZIP.
// It has no publication or release-qualification authority.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	modzip "golang.org/x/mod/zip"
)

const identity = "go.izoone.com/robotgo"

type sourceFile struct{ file *zip.File }

func (f sourceFile) Path() string                 { return f.file.Name }
func (f sourceFile) Lstat() (os.FileInfo, error)  { return f.file.FileInfo(), nil }
func (f sourceFile) Open() (io.ReadCloser, error) { return f.file.Open() }

func packageModule(input io.Reader, version, commitTime, output string) (err error) {
	if semver.Canonical(version) != version || semver.Major(version) != "v1" {
		return fmt.Errorf("expected canonical v1 version")
	}
	timestamp, err := time.Parse(time.RFC3339, commitTime)
	if err != nil {
		return fmt.Errorf("commit time: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(input, (128<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 128<<20 {
		return fmt.Errorf("source archive exceeds bound")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	var files []modzip.File
	var manifest []byte
	var versionSource []byte
	for _, file := range archive.File {
		if strings.HasSuffix(file.Name, "/") {
			continue
		}
		files = append(files, sourceFile{file})
		if file.Name == "go.mod" || file.Name == "version.go" {
			stream, openErr := file.Open()
			if openErr != nil {
				return openErr
			}
			body, readErr := io.ReadAll(io.LimitReader(stream, modzip.MaxGoMod+1))
			closeErr := stream.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if file.Name == "go.mod" {
				manifest = body
			} else {
				versionSource = body
			}
		}
	}
	parsed, err := modfile.Parse("go.mod", manifest, nil)
	if err != nil {
		return err
	}
	if parsed.Module == nil || parsed.Module.Mod.Path != identity {
		return fmt.Errorf("wrong module identity")
	}
	if err := checkSourceVersion(versionSource, version); err != nil {
		return err
	}
	if err = os.Mkdir(output, 0755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(output)
		}
	}()
	zipPath := filepath.Join(output, "go-module.zip")
	zipFile, err := os.OpenFile(zipPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	m := module.Version{Path: identity, Version: version}
	err = modzip.Create(zipFile, m, files)
	closeErr := zipFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err = modzip.CheckZip(m, zipPath); err != nil {
		return err
	}
	info, err := json.Marshal(struct {
		Version string
		Time    string
	}{version, timestamp.UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(output, "go-module.info"), append(info, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "go-module.mod"), manifest, 0644)
}

func checkSourceVersion(source []byte, version string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "version.go", source, 0)
	if err != nil {
		return fmt.Errorf("source version: %w", err)
	}
	if f.Name.Name != "robotgo" {
		return fmt.Errorf("wrong version.go package")
	}
	for _, decl := range f.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value := spec.(*ast.ValueSpec)
			if len(value.Names) != 1 || value.Names[0].Name != "Version" || len(value.Values) != 1 {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				break
			}
			got, err := strconv.Unquote(literal.Value)
			if err == nil && got == version {
				return nil
			}
		}
	}
	return fmt.Errorf("version.go does not declare Version = %q", version)
}

func main() {
	version := flag.String("version", "", "canonical v1 release version")
	commitTime := flag.String("time", "", "exact source commit RFC3339 time")
	output := flag.String("out", "", "new artifact directory (must not exist)")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(2)
	}
	if err := packageModule(os.Stdin, *version, *commitTime, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
