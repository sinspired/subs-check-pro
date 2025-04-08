package app

import "embed"

//go:embed templates/*
var configFS embed.FS
