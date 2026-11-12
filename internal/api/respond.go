package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeJSON 先把响应编码到缓冲区，再写状态码与内容。
// 这样编码失败时仍能返回完整的错误响应，而不是 200 + 半截 JSON。
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		slog.Error("encode response", "err", err)
		http.Error(w, `{"code":"INTERNAL","message":"响应编码失败"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// writeYAML 输出 YAML 文本响应。
func writeYAML(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// writePlain 输出纯文本响应（健康检查等运维端点）。
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
