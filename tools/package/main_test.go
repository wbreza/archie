package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken output") }

func TestArchiveContentAndDeterminism(t *testing.T) {
	entries := []entry{{"archie", []byte("binary"), 0o755}, {"docs/contract.md", []byte("contract"), 0o644}}
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
				if err != nil || file.Name != entries[i].name || !bytes.Equal(b, entries[i].data) || file.Mode().Perm() != 0o755 && i == 0 {
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
