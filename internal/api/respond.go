package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	writeYAMLStatus(w, http.StatusOK, body)
}

// writeYAMLStatus 输出指定状态码的 YAML 文本响应。
func writeYAMLStatus(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writePlain 输出纯文本响应（健康检查等运维端点）。
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// maxJSONBody 是普通 JSON 请求体上限，避免超大请求耗尽内存。
const maxJSONBody = 1 << 20

// decodeJSON 解析 JSON 请求体；失败时直接写统一错误响应并返回 false。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			WriteError(r.Context(), w, NewError(CodePayloadTooLarge, "请求体超过 1 MB 上限"))
			return false
		}
		if errors.Is(err, io.EOF) {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "请求体不能为空"))
			return false
		}
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "请求体不是合法的 JSON"))
		return false
	}
	return true
}
