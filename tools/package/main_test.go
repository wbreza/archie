package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }

func TestArchiveContentAndDeterminism(t *testing.T) {
	docs, err := loadDocumentation(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	entries := append([]entry{{"archie", []byte("binary"), 0o755}}, docs...)
	for _, windows := range []bool{true, false} {
		var first, second bytes.Buffer
		if err := archive(&first, windows, entries); err != nil {
			t.Fatal(err)
		}
		if err := archive(&second, windows, entries); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("archive is not deterministic")
		}
		if windows {
			z, err := zip.NewReader(bytes.NewReader(first.Bytes()), int64(first.Len()))
			if err != nil || len(z.File) != len(entries) {
				t.Fatalf("invalid zip: %v", err)
			}
			for i, file := range z.File {
				r, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(r)
				r.Close()
				if err != nil || file.Name != entries[i].name || !bytes.Equal(b, entries[i].data) || int64(file.Mode().Perm()) != entries[i].mode {
					t.Fatalf("wrong zip member: %+v %v", file, err)
				}
			}
		} else {
			gz, err := gzip.NewReader(bytes.NewReader(first.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			r := tar.NewReader(gz)
			for _, item := range entries {
				h, err := r.Next()
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(r)
				if err != nil || h.Name != item.name || h.Mode != item.mode || !bytes.Equal(b, item.data) {
					t.Fatalf("wrong tar member: %+v %v", h, err)
				}
			}
			if _, err := r.Next(); err != io.EOF {
				t.Fatal("unexpected tar data")
			}
			gz.Close()
		}
		if err := archive(brokenWriter{}, windows, entries); err == nil {
			t.Fatal("archive writer error ignored")
		}
	}
}

func TestBundledDocumentation(t *testing.T) {
	root := filepath.Join("..", "..")
	docs, err := loadDocumentation(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"README.md", "CONTRIBUTING.md",
		"docs/getting-started.md", "docs/usage.md",
		"docs/contract-v1.md", "docs/build-and-measure.md", "docs/versioning.md",
		"schemas/record.schema.json", "schemas/root.schema.json", "schemas/response.schema.json",
	}
	if len(docs) != len(want) {
		t.Fatalf("got %d bundled documents, want %d", len(docs), len(want))
	}
	for i, name := range want {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if docs[i].name != name || !bytes.Equal(docs[i].data, data) || docs[i].mode != 0o644 {
			t.Fatalf("wrong bundled document %q", name)
		}
	}
	if _, err := loadDocumentation(filepath.Join(root, "README.md")); err == nil {
		t.Fatal("missing documentation must fail")
	}
}

func TestBuildInputsAndEnvironment(t *testing.T) {
	for _, v := range []string{"", "../escape", "v1.0.0", "1.0.0 -X malicious", "1.0.0/extra"} {
		if err := build(v, "."); err == nil {
			t.Fatalf("invalid version accepted: %q", v)
		}
	}
	if err := build("1.0.0", ""); err == nil {
		t.Fatal("missing output accepted")
	}
	if err := build("1.0.0", "directory-that-does-not-exist"); err == nil {
		t.Fatal("missing output directory accepted")
	}
	for _, key := range []string{"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS"} {
		t.Setenv(key, "unwanted")
	}
	env := buildEnv(target{"linux", "arm64"})
	for _, want := range []string{"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS="} {
		count := 0
		key, _, _ := strings.Cut(want, "=")
		for _, item := range env {
			if strings.HasPrefix(strings.ToUpper(item), key+"=") {
				count++
				if item != want {
					t.Fatalf("unexpected build setting %q", item)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s count %d", key, count)
		}
	}
}
