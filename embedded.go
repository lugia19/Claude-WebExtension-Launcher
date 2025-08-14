package main

import "embed"

//go:embed resources/injections/0.12.55/*.js
//go:embed resources/icons/*
//go:embed resources/rcedit.exe
var EmbeddedFS embed.FS
