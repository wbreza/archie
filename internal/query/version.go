package query

import "runtime/debug"

// BuildVersion overrides module build information when set using go build -ldflags -X.
var BuildVersion string

func version() string {
	info, _ := debug.ReadBuildInfo()
	return resolveVersion(BuildVersion, info)
}

func resolveVersion(override string, info *debug.BuildInfo) string {
	if override != "" {
		return override
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "0.1.0-dev"
}
