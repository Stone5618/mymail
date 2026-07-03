// Package web 通过 //go:embed 将前端构建产物嵌入 Go 二进制，实现单文件部署。
//
// 开发环境下 web/dist/ 仅含 .gitkeep 占位文件，编译通过但无实际静态资源；
// 生产构建时（Dockerfile Stage 1）将 Vue 构建产物复制到 web/dist/，编译期自动嵌入。
package web

import "embed"

// DistFS 嵌入前端构建产物 dist/ 目录。
//
// 使用 all: 前缀以包含 .gitkeep 等隐藏文件，确保开发环境下 dist/ 无实际资源时编译仍通过。
// 调用方通过 fs.Sub(DistFS, "dist") 获取根目录视图，再用 http.FileServer 服务。
//
//go:embed all:dist
var DistFS embed.FS
