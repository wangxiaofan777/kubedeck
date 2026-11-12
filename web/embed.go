// Package web 提供嵌入的前端静态资源。
package web

import (
	"embed"
	"io/fs"
)

// dist 由 `make web` 生成并经 go:embed 打进二进制；dist 目录由 .gitkeep 保证存在。
//
//go:embed all:dist
var dist embed.FS

// Dist 返回前端构建产物（web/dist）。
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
