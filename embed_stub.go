//go:build !dashboard

// Package stonewall is the module root package. Without the `dashboard` build
// tag, no SPA is embedded (the build output may not exist in a pure-Go/CI
// context), and the /dashboard handler returns 503.
package stonewall

import "io/fs"

// DashboardFS returns nil when the dashboard is not embedded in this build.
func DashboardFS() fs.FS { return nil }
