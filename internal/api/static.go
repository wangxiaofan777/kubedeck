package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler 提供单页应用静态资源：文件存在则直接返回；前端路由回退到 index.html。
// API 路径不参与回退；非 GET/HEAD 与非 HTML 请求也不会拿到页面。
func SPAHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if p == "api" || strings.HasPrefix(p, "api/") {
			WriteError(r.Context(), w, NewError(CodeNotFound, "资源不存在"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if p != "" {
			if _, err := fs.Stat(fsys, p); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// 只有浏览器导航（Accept 含 text/html）才回退到 SPA 入口。
		if !acceptsHTML(r.Header.Get("Accept")) {
			WriteError(r.Context(), w, NewError(CodeNotFound, "资源不存在"))
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func acceptsHTML(accept string) bool {
	for _, part := range strings.Split(accept, ",") {
		mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			return true
		}
	}
	return false
}
