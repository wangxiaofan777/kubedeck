package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler 提供单页应用静态资源：文件存在则直接返回，否则回退到 index.html。
// API 路径不参与回退，未匹配时返回统一 404。
func SPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if p == "api" || strings.HasPrefix(p, "api/") {
			WriteError(r.Context(), w, NewError(CodeNotFound, "资源不存在"))
			return
		}

		if p != "" {
			if _, err := fs.Stat(fsys, p); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// 前端路由（/clusters/xxx 等）回退到 SPA 入口。
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
