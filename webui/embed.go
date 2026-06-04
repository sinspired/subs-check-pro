// Package webui 负责嵌入前端资源（HTML 模板和静态文件）。
package webui

import "embed"

//go:embed templates/*
var TemplatesFS embed.FS

//go:embed static
var StaticFS embed.FS
