package frontendembed

import "embed"

// DistFS embeds the frontend build output so the single Go binary can serve it.
//
//go:embed all:frontend/dist
var DistFS embed.FS
