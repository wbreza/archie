package query

import (
	"archive/zip"
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/wbreza/archie/contract"
	"github.com/wbreza/archie/internal/testutil"
)

func TestResolveVersion(t *testing.T) {
	for _, tc := range []struct {
		name, override, module, want string
	}{
		{name: "missing-version", want: "0.1.0-dev"},
		{name: "local-source", module: "(devel)", want: "0.1.0-dev"},
		{name: "installed-tag", module: "v0.1.0", want: "v0.1.0"},
		{name: "prerelease", module: "v0.2.0-rc.1", want: "v0.2.0-rc.1"},
		{name: "pseudo-version", module: "v0.0.0-20260909195313-b3e4bff0ea2a", want: "v0.0.0-20260909195313-b3e4bff0ea2a"},
		{name: "override-installed", override: "0.2.0-custom", module: "v0.1.0", want: "0.2.0-custom"},
		{name: "override-local", override: "0.1.0", module: "(devel)", want: "0.1.0"},
		{name: "explicit-development-override", override: "0.1.0-dev", module: "v0.1.0", want: "0.1.0-dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.module}}
			if got := resolveVersion(tc.override, info); got != tc.want {
				t.Fatalf("version = %q, want %q", got, tc.want)
			}
		})
	}
	if got := resolveVersion("", nil); got != "0.1.0-dev" {
		t.Fatalf("missing build info = %q", got)
	}
	if got := resolveVersion("0.1.0", nil); got != "0.1.0" {
		t.Fatalf("override without build info = %q", got)
	}
}

func TestDevelopmentVersionResponse(t *testing.T) {
	raw, code := Run(Options{Command: contract.VersionCommand})
	var response contract.Response[contract.VersionData]
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if code != contract.ExitOK || response.APIVersion != "1" || response.Command != contract.VersionCommand ||
		response.Status != contract.OK || response.Data == nil || response.Data.Version != "0.1.0-dev" {
		t.Fatalf("development version response: exit %d %s", code, raw)
	}
}

func TestGoInstallVersion(t *testing.T) {
	root, err := os.MkdirTemp(testutil.ScratchBase(), "version-install-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	source, err := os.ReadFile("version.go")
	if err != nil {
		t.Fatal(err)
	}
	const module = "github.com/wbreza/archie"
	// Exercise the real provider with Go's installed-module metadata, entirely
	// offline. This fixture is not evidence that any remote tag is published.
	files := map[string][]byte{
		"go.mod":                    []byte("module " + module + "\n\ngo 1.25.0\n"),
		"internal/query/version.go": source,
		"internal/query/export.go":  []byte("package query\nfunc VersionForTest() string { return version() }\n"),
		"cmd/archie/main.go": []byte("package main\nimport (\"fmt\"; \"" + module +
			"/internal/query\")\nfunc main() { fmt.Print(query.VersionForTest()) }\n"),
	}
	proxy := filepath.Join(root, "proxy")
	versionDir := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "list"), []byte("v0.1.0\nv0.2.0-rc.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v0.1.0", "v0.2.0-rc.1", "v0.0.0-20260909195313-b3e4bff0ea2a"} {
		var archive bytes.Buffer
		z := zip.NewWriter(&archive)
		for name, content := range files {
			w, err := z.Create(module + "@" + version + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(content); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		info, err := json.Marshal(map[string]string{"Version": version, "Time": "2026-09-09T19:53:13Z"})
		if err != nil {
			t.Fatal(err)
		}
		for suffix, content := range map[string][]byte{".mod": files["go.mod"], ".info": info, ".zip": archive.Bytes()} {
			if err := os.WriteFile(filepath.Join(versionDir, version+suffix), content, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	proxyPath := filepath.ToSlash(proxy)
	if !strings.HasPrefix(proxyPath, "/") {
		proxyPath = "/" + proxyPath
	}
	t.Setenv("GOPROXY", (&url.URL{Scheme: "file", Path: proxyPath}).String())
	t.Setenv("GONOPROXY", "none")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", filepath.Join(root, "modules"))
	t.Setenv("GOBIN", filepath.Join(root, "bin"))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOOS", runtime.GOOS)
	t.Setenv("GOARCH", runtime.GOARCH)
	t.Setenv("CGO_ENABLED", "0")
	for _, tc := range []struct{ version, override string }{
		{version: "v0.1.0"},
		{version: "v0.2.0-rc.1"},
		{version: "v0.0.0-20260909195313-b3e4bff0ea2a"},
		{version: "v0.1.0", override: "0.1.0-dev"},
	} {
		t.Run(tc.version+"/"+tc.override, func(t *testing.T) {
			args := []string{"install"}
			want := tc.version
			if tc.override != "" {
				args = append(args, "-ldflags", "-X "+module+"/internal/query.BuildVersion="+tc.override)
				want = tc.override
			}
			cmd := exec.Command("go", append(args, module+"/cmd/archie@"+tc.version)...)
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("offline fixture install: %v\n%s", err, out)
			}
			exe := filepath.Join(root, "bin", "archie")
			if runtime.GOOS == "windows" {
				exe += ".exe"
			}
			info, err := buildinfo.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			if info.Main.Path != module || info.Main.Version != tc.version {
				t.Fatalf("installed module = %+v", info.Main)
			}
			run := exec.Command(exe)
			run.Dir = root
			out, err := run.CombinedOutput()
			if err != nil || string(out) != want {
				t.Fatalf("version = %q, error %v; want %q", out, err, want)
			}
		})
	}
}
