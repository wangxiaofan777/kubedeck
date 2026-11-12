// Package webui 把前端构建产物嵌入服务端二进制。
//
// 产物由 web/ 下的 Vite 构建到本目录的 dist/（见 web/vite.config.ts）。
// dist 中仅 .gitkeep 入库，保证全新检出时 go:embed 有匹配文件。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Dist 返回前端静态资源根目录。
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// dist 由编译期 embed 保证存在，失败属于不可恢复的构建问题。
		panic(err)
	}
	return sub
}
