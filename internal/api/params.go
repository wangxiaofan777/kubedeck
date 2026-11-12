package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	defaultListLimit = 200
	maxListLimit     = 500
)

// gvrFromPath 解析路径中的 {group}/{version}/{resource}；group 用 "_" 表示 core 组。
func gvrFromPath(r *http.Request) (schema.GroupVersionResource, bool) {
	group := chi.URLParam(r, "group")
	version := chi.URLParam(r, "version")
	resource := chi.URLParam(r, "resource")
	if group == "_" {
		group = ""
	}
	if version == "" || resource == "" {
		return schema.GroupVersionResource{}, false
	}
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, true
}

// parseLimit 解析列表分页大小；空值使用默认值，越界返回可读错误。
func parseLimit(raw string) (int64, error) {
	if raw == "" {
		return defaultListLimit, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || n > maxListLimit {
		return 0, fmt.Errorf("limit 必须在 1-%d 之间", maxListLimit)
	}
	return n, nil
}
