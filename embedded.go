package main

import "embed"

//go:embed resources/injections/*/*.js
//go:embed resources/sentinel_extension/*
//go:embed resources/icons/*
//go:embed resources/rcedit.exe
//go:embed resources/version-x64.dll
//go:embed resources/version-arm64.dll
//go:embed resources/verified_versions.json
var EmbeddedFS embed.FS
