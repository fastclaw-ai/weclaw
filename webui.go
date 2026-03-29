package main

import "embed"

//go:embed all:web/out
var webUIFS embed.FS
