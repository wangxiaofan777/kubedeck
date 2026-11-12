// Package api 承载 HTTP 接口层：路由、handler、请求/响应模型。
package api

import (
	"context"
	"errors"
	"net"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// ErrorCode 是稳定错误码，取值见 k8s-console-design.md 附录 E。
type ErrorCode string

const (
	CodeUnauthenticated    ErrorCode = "UNAUTHENTICATED"
	CodeForbidden          ErrorCode = "FORBIDDEN"
	CodeNotFound           ErrorCode = "NOT_FOUND"
	CodeConflict           ErrorCode = "CONFLICT"
	CodeInvalidArgument    ErrorCode = "INVALID_ARGUMENT"
	CodeUnprocessable      ErrorCode = "UNPROCESSABLE"
	CodeClusterUnreachable ErrorCode = "CLUSTER_UNREACHABLE"
	CodeClusterDegraded    ErrorCode = "CLUSTER_DEGRADED"
	CodeUpstreamTimeout    ErrorCode = "UPSTREAM_TIMEOUT"
	CodeRateLimited        ErrorCode = "RATE_LIMITED"
	CodePayloadTooLarge    ErrorCode = "PAYLOAD_TOO_LARGE"
	CodeUnsupported        ErrorCode = "UNSUPPORTED"
	CodeInternal           ErrorCode = "INTERNAL"
)

// HTTPStatus 返回错误码对应的 HTTP 状态码。
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeInvalidArgument, CodeUnsupported:
		return http.StatusBadRequest
	case CodeUnprocessable:
		return http.StatusUnprocessableEntity
	case CodeClusterUnreachable:
		return http.StatusServiceUnavailable
	case CodeClusterDegraded:
		// 集群降级时无法完成本次读取，按依赖不可用处理。
		return http.StatusServiceUnavailable
	case CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}

// Error 是统一错误响应体 {code, message, details, requestId}。
type Error struct {
	Code      ErrorCode      `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}

// Error 实现 error 接口。
func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// NewError 构造统一错误。
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// FromUpstream 把 apiserver 的 Status 映射为稳定错误码；未知原因归为 INTERNAL，
// 原始 message 保留在 details.upstream 供排查。
func FromUpstream(err error) *Error {
	if err == nil {
		return NewError(CodeInternal, "nil upstream error")
	}

	var code ErrorCode
	switch {
	case apierrors.IsUnauthorized(err):
		code = CodeUnauthenticated
	case apierrors.IsForbidden(err):
		code = CodeForbidden
	case apierrors.IsNotFound(err):
		code = CodeNotFound
	case apierrors.IsConflict(err):
		code = CodeConflict
	case apierrors.IsInvalid(err):
		code = CodeUnprocessable
	case apierrors.IsBadRequest(err):
		code = CodeInvalidArgument
	case apierrors.IsTooManyRequests(err):
		code = CodeRateLimited
	case apierrors.IsRequestEntityTooLargeError(err):
		code = CodePayloadTooLarge
	case apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		code = CodeUpstreamTimeout
	case apierrors.IsServiceUnavailable(err):
		code = CodeClusterUnreachable
	default:
		// 网络层错误（连接被拒、DNS 失败、拨号超时）同样属于集群不可达。
		var netErr net.Error
		if errors.As(err, &netErr) {
			code = CodeClusterUnreachable
		} else {
			code = CodeInternal
		}
	}

	e := NewError(code, userMessage(code))
	e.Details = map[string]any{"upstream": err.Error()}
	if status, ok := err.(apierrors.APIStatus); ok {
		if s := status.Status(); s.Reason != "" {
			e.Details["upstreamReason"] = string(s.Reason)
		}
	}
	return e
}

// FromPlatformError 把平台仓储/领域的错误映射为稳定错误码。
func FromPlatformError(err error) *Error {
	switch {
	case err == nil:
		return NewError(CodeInternal, "平台内部错误")
	case errors.Is(err, domain.ErrNotFound):
		return NewError(CodeNotFound, "资源不存在")
	case errors.Is(err, domain.ErrConflict):
		return NewError(CodeConflict, "资源已存在或状态冲突")
	case errors.Is(err, domain.ErrInvalid):
		return NewError(CodeInvalidArgument, "请求参数不合法")
	default:
		return NewError(CodeInternal, "平台内部错误")
	}
}

// userMessage 为每个错误码提供面向用户的中文提示，避免把 upstream 原文直接暴露给前端。
func userMessage(code ErrorCode) string {
	switch code {
	case CodeUnauthenticated:
		return "登录状态无效或已过期"
	case CodeForbidden:
		return "权限不足，操作被拒绝"
	case CodeNotFound:
		return "资源不存在"
	case CodeConflict:
		return "资源已被修改，请刷新后重试"
	case CodeInvalidArgument:
		return "请求参数不合法"
	case CodeUnprocessable:
		return "对象校验失败"
	case CodeClusterUnreachable:
		return "目标集群不可达"
	case CodeUpstreamTimeout:
		return "目标集群响应超时"
	case CodeRateLimited:
		return "请求过于频繁，请稍后重试"
	case CodePayloadTooLarge:
		return "请求内容超过大小上限"
	default:
		return "平台内部错误"
	}
}

// 以下为 requestId 的上下文传递，1.4 的中间件负责写入。

type ctxKey string

const requestIDKey ctxKey = "requestId"

// WithRequestID 把请求 ID 写入 context。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom 从 context 读取请求 ID；不存在时返回空串。
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// WriteError 输出统一错误响应。
func WriteError(ctx context.Context, w http.ResponseWriter, e *Error) {
	if e.RequestID == "" {
		e.RequestID = RequestIDFrom(ctx)
	}
	writeJSON(w, e.Code.HTTPStatus(), e)
}
