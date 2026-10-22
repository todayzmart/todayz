// Package buildinfo holds the version stamped at build time:
//
//	go build -ldflags "-X github.com/todayz/backend/internal/platform/buildinfo.Version=... \
//	                   -X github.com/todayz/backend/internal/platform/buildinfo.Commit=... \
//	                   -X github.com/todayz/backend/internal/platform/buildinfo.BuildTime=... \
//	                   -X github.com/todayz/backend/internal/platform/buildinfo.Modified=true|false"
//
// Without ldflags it falls back to the VCS info Go embeds, then to "dev".
// (Go only stamps VCS info from a .git directory, so in a git worktree nested
// inside another checkout the embedded info is the outer checkout's; make
// backend-bin passes the ldflags for that reason.)
package buildinfo

import (
	"runtime/debug"
	"sync"
)

// Set by -ldflags at build time.
var (
	Version   = ""
	Commit    = ""
	BuildTime = ""
	Modified  = "" // "true" when built from a tree with uncommitted changes
)

// Info is what GET /version returns. It leaves out the Go version (T6): a
// toolchain version tells an attacker which stdlib CVEs apply.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	Dirty     bool   `json:"dirty"`
}

var (
	once sync.Once
	info Info
)

// Get returns the build info.
func Get() Info {
	once.Do(func() {
		info = Info{Version: Version, Commit: Commit, BuildTime: BuildTime, Dirty: Modified == "true"}
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					if info.Commit == "" {
						info.Commit = s.Value
					}
				case "vcs.time":
					if info.BuildTime == "" {
						info.BuildTime = s.Value
					}
				case "vcs.modified":
					if Modified == "" {
						info.Dirty = s.Value == "true"
					}
				}
			}
		}
		if info.Version == "" {
			info.Version = "dev"
		}
		if info.Commit == "" {
			info.Commit = "unknown"
		}
	})
	return info
}
