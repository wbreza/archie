// Command package builds local archives only. It never releases or publishes.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type target struct{ os, arch string }
type entry struct {
	name string
	data []byte
	mode int64
}

var targets = []target{
	{"windows", "amd64"}, {"windows", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?$`)

func main() {
	version := flag.String("version", "", "required build version, e.g. 0.1.0-dev")
	out := flag.String("out", "", "required existing parent directory for new archie_VERSION output")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional argument")
		os.Exit(2)
	}
	if err := build(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(version, parent string) error {
	if !versionPattern.MatchString(version) || parent == "" {
		return fmt.Errorf("provide -version MAJOR.MINOR.PATCH[-SUFFIX] and -out EXISTING_DIRECTORY")
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("output parent must already exist")
	}
	var docs []entry
	for _, name := range []string{"README.md", "docs/contract-v1.md", "docs/build-and-measure.md", "docs/versioning.md", "schemas/record.schema.json", "schemas/root.schema.json", "schemas/response.schema.json"} {
		b, err := os.ReadFile(filepath.FromSlash(name))
		if err != nil {
			return fmt.Errorf("run from the Archie repository root: %w", err)
		}
		docs = append(docs, entry{name, b, 0o644})
	}
	out := filepath.Join(parent, "archie_"+version)
	if err := os.Mkdir(out, 0o755); err != nil {
		return fmt.Errorf("create new output directory (existing output is never replaced): %w", err)
	}
	stage, err := os.MkdirTemp(out, ".build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // Only this invocation's uniquely created child.
	var sums strings.Builder
	for _, platform := range targets {
		binary := "archie"
		extension := ".tar.gz"
		if platform.os == "windows" {
			binary += ".exe"
			extension = ".zip"
		}
		binaryPath := filepath.Join(stage, binary)
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false",
			"-ldflags", "-s -w -X github.com/wbreza/archie/internal/query.BuildVersion="+version,
			"-o", binaryPath, "./cmd/archie")
		cmd.Env = buildEnv(platform)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s/%s: %w\n%s", platform.os, platform.arch, err, output)
		}
		b, err := os.ReadFile(binaryPath)
		if err != nil {
			return err
		}
		entries := append([]entry{{binary, b, 0o755}}, docs...)
		name := fmt.Sprintf("archie_%s_%s_%s%s", version, platform.os, platform.arch, extension)
		file, err := os.OpenFile(filepath.Join(out, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		hash := sha256.New()
		err = archive(io.MultiWriter(file, hash), platform.os == "windows", entries)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(&sums, "%x  %s\n", hash.Sum(nil), name)
		fmt.Println(filepath.Join(out, name))
	}
	checksums, err := os.OpenFile(filepath.Join(out, "SHA256SUMS"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.WriteString(checksums, sums.String())
	closeErr := checksums.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func buildEnv(platform target) []string {
	env := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS":
			continue
		}
		env = append(env, item)
	}
	return append(env, "GOOS="+platform.os, "GOARCH="+platform.arch, "CGO_ENABLED=0", "GOFLAGS=")
}

func archive(w io.Writer, windows bool, entries []entry) error {
	if windows {
		z := zip.NewWriter(w)
		for _, item := range entries {
			header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
			header.SetMode(os.FileMode(item.mode))
			header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
			f, err := z.CreateHeader(header)
			if err != nil {
				return err
			}
			if _, err := f.Write(item.data); err != nil {
				return err
			}
		}
		return z.Close()
	}
	gz := gzip.NewWriter(w)
	t := tar.NewWriter(gz)
	for _, item := range entries {
		if err := t.WriteHeader(&tar.Header{Name: item.name, Mode: item.mode, Size: int64(len(item.data)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		if _, err := t.Write(item.data); err != nil {
			return err
		}
	}
	if err := t.Close(); err != nil {
		return err
	}
	return gz.Close()
}
