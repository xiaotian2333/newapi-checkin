package assets

import "embed"

//go:embed index.html index.css checkin-pow.js checkin-captcha.js checkin-app.js
var Files embed.FS
