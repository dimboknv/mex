package web

import "embed"

//go:embed index.html app.js styles.css
var StaticFiles embed.FS
