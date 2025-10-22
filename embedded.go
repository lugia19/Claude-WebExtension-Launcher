package main

import "embed"

//go:embed resources/injections/*/*.js
//go:embed resources/icons/*
//go:embed resources/rcedit.exe
//go:embed resources/verified_versions.json
var EmbeddedFS embed.FS
