package main

import "embed"

//go:embed resources/injections/*/*.js
//go:embed resources/icons/*
//go:embed resources/rcedit.exe
var EmbeddedFS embed.FS
