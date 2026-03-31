package assets

import "embed"

//go:embed index.html index.css checkin-pow.js
var Files embed.FS
