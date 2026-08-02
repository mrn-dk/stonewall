// Package stonewall is the module root package. It embeds the built dashboard
// SPA (produced by `npm run build` in dashboard/) so the single `stonewall`
// binary serves the operator UI with no separate static host. The embed is
// gated by the `dashboard` build tag: with it, the real build is embedded;
// without it (the default for pure-Go `go test ./...`), an empty filesystem is
// provided and the /dashboard handler returns 503.
//
// Build with the dashboard embedded:  make build   (runs npm then go build -tags dashboard)
// Build/test without the dashboard:   go test ./...   (no tag needed)

//go:build dashboard

package stonewall

import (
	"embed"
	"io/fs"
)

//go:embed all:dashboard/build
var dashboardBuild embed.FS

// DashboardFS returns the embedded SvelteKit build as a filesystem rooted at
// the build directory (index.html, _app/...). Returns nil if not embedded.
func DashboardFS() fs.FS {
	sub, err := fs.Sub(dashboardBuild, "dashboard/build")
	if err != nil {
		return nil
	}
	return sub
}
