package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubedeck/kubedeck/internal/kube"
)

const (
	defaultLogTail = 500
	maxLogTail     = 5000
	maxLogBytes    = 5 << 20
	maxEventCount  = 500
)

// podLog 读取或跟随 Pod 容器日志。
func (h *handlers) podLog(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) || !validObjectName(w, r.Context(), namespace) {
		return
	}

	opts, err := parseLogOptions(r)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}

	// 容器名缺失且 Pod 有多个容器时，返回可选列表，避免让用户猜。
	containers := h.podContainers(r.Context(), t, namespace, name)
	if opts.Container == "" && len(containers) > 1 {
		e := NewError(CodeInvalidArgument, "Pod 包含多个容器，请指定 container")
		e.Details = map[string]any{"containers": containers}
		WriteError(r.Context(), w, e)
		return
	}
	if opts.Container == "" && len(containers) == 1 {
		opts.Container = containers[0]
	}

	stream, err := t.Logs(r.Context(), namespace, name, opts)
	if err != nil {
		writeLogError(w, r, err, containers)
		return
	}
	defer func() { _ = stream.Close() }()

	if opts.Follow {
		streamLog(w, r, stream)
		return
	}

	body, err := io.ReadAll(io.LimitReader(stream, maxLogBytes+1))
	if err != nil && !errors.Is(err, io.EOF) {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	// kubelet 在上一实例日志不可用时会以 200 + 提示文本返回，需要显式转换。
	if strings.HasPrefix(string(body), "unable to retrieve container logs for ") {
		e := NewError(CodeNotFound, "上一实例日志不可用（容器日志可能已被轮转）")
		e.Details = map[string]any{"containers": containers, "previous": opts.Previous}
		WriteError(r.Context(), w, e)
		return
	}
	truncated := len(body) > maxLogBytes
	if truncated {
		body = body[:maxLogBytes]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Log-Limit-Bytes", strconv.Itoa(maxLogBytes))
	if truncated {
		w.Header().Set("X-Log-Truncated", "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func parseLogOptions(r *http.Request) (kube.LogOptions, error) {
	q := r.URL.Query()
	tail := int64(defaultLogTail)
	if raw := q.Get("tailLines"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 || n > maxLogTail {
			return kube.LogOptions{}, errors.New("tailLines 必须在 0-" + strconv.Itoa(maxLogTail) + " 之间")
		}
		tail = n
	}
	opts := kube.LogOptions{
		Container:  q.Get("container"),
		TailLines:  &tail,
		Previous:   q.Get("previous") == "true",
		Follow:     q.Get("follow") == "true",
		Timestamps: q.Get("timestamps") != "false",
	}
	if raw := q.Get("sinceSeconds"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return kube.LogOptions{}, errors.New("sinceSeconds 必须是非负整数")
		}
		opts.SinceSeconds = &n
	}
	if raw := q.Get("sinceTime"); raw != "" {
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			return kube.LogOptions{}, errors.New("sinceTime 必须是 RFC3339 时间")
		}
		opts.SinceTime = raw
	}
	return opts, nil
}

// streamLog 以 chunked 文本流持续输出；客户端断开时随 ctx 结束。
func streamLog(w http.ResponseWriter, r *http.Request, stream io.Reader) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Trailer", "X-Log-Truncated")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	buf := make([]byte, 32<<10)
	written := 0
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			written += n
			if flusher != nil {
				flusher.Flush()
			}
			if written >= maxLogBytes {
				w.Header().Set("X-Log-Truncated", "true")
				return
			}
		}
		if err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

func (h *handlers) podContainers(ctx context.Context, t kube.Transport, namespace, name string) []string {
	pod, err := t.Get(ctx, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, namespace, name)
	if err != nil {
		return nil
	}
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	out := make([]string, 0, len(containers))
	for _, c := range containers {
		if m, ok := c.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

func writeLogError(w http.ResponseWriter, r *http.Request, err error, containers []string) {
	msg := err.Error()
	lower := strings.ToLower(msg)
	if apierrors.IsBadRequest(err) || apierrors.IsNotFound(err) {
		switch {
		case strings.Contains(lower, "waiting to start"), strings.Contains(lower, "not available"),
			strings.Contains(lower, "previous terminated container"):
			e := NewError(CodeNotFound, "容器尚未启动或上一实例日志不可用")
			e.Details = map[string]any{"containers": containers}
			WriteError(r.Context(), w, e)
			return
		case strings.Contains(lower, "a container name must be specified"):
			e := NewError(CodeInvalidArgument, "Pod 包含多个容器，请指定 container")
			e.Details = map[string]any{"containers": containers}
			WriteError(r.Context(), w, e)
			return
		}
	}
	WriteError(r.Context(), w, FromUpstream(err))
}

// listEvents 返回按最后发生时间倒序的事件，并聚合重复事件。
func (h *handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	namespace := requestNamespace(r)
	q := r.URL.Query()

	var selectors []string
	if kind := q.Get("kind"); kind != "" {
		selectors = append(selectors, "involvedObject.kind="+kind)
	}
	if name := q.Get("name"); name != "" {
		selectors = append(selectors, "involvedObject.name="+name)
	}
	since := time.Time{}
	if raw := q.Get("sinceSeconds"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "sinceSeconds 必须是非负整数"))
			return
		}
		since = time.Now().Add(-time.Duration(n) * time.Second)
	}
	if raw := q.Get("sinceTime"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "sinceTime 必须是 RFC3339 时间"))
			return
		}
		since = parsed
	}
	eventType := q.Get("type")

	list, err := t.List(r.Context(), schema.GroupVersionResource{Version: "v1", Resource: "events"}, namespace, kube.ListOptions{
		Limit:         maxEventCount,
		FieldSelector: strings.Join(selectors, ","),
	})
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	type aggregate struct {
		item  map[string]any
		count int64
		last  string
	}
	byKey := make(map[string]*aggregate)
	for i := range list.Items {
		obj := &list.Items[i]
		if eventType != "" && nestedString(obj.Object, "type") != eventType {
			continue
		}
		last := eventLastTimestamp(obj.Object)
		if !since.IsZero() && last != "" {
			if ts, err := time.Parse(time.RFC3339, last); err == nil && ts.Before(since) {
				continue
			}
		}
		uid := nestedString(obj.Object, "involvedObject", "uid")
		key := strings.Join([]string{
			nestedString(obj.Object, "type"), nestedString(obj.Object, "reason"),
			nestedString(obj.Object, "message"), uid,
		}, "\x00")
		count := nestedInt(obj.Object, "count")
		if count == 0 {
			count = nestedInt(obj.Object, "series", "count")
		}
		if count == 0 {
			count = 1
		}
		if existing, ok := byKey[key]; ok {
			if count > existing.count {
				existing.count = count
			}
			if last > existing.last {
				existing.last = last
			}
			continue
		}
		byKey[key] = &aggregate{
			item: map[string]any{
				"type":           nestedString(obj.Object, "type"),
				"reason":         nestedString(obj.Object, "reason"),
				"message":        nestedString(obj.Object, "message"),
				"count":          count,
				"lastTimestamp":  last,
				"firstTimestamp": nestedString(obj.Object, "firstTimestamp"),
				"involvedObject": nestedMap(obj.Object, "involvedObject"),
				"source":         nestedMap(obj.Object, "source"),
				"metadata":       obj.Object["metadata"],
			},
			count: count,
			last:  last,
		}
	}

	items := make([]map[string]any, 0, len(byKey))
	for _, a := range byKey {
		a.item["count"] = a.count
		a.item["lastTimestamp"] = a.last
		items = append(items, a.item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return lastTimestamp(items[i]) > lastTimestamp(items[j])
	})

	resp := map[string]any{"items": items}
	if len(items) == 0 && !since.IsZero() && time.Since(since) > time.Hour {
		resp["staleWarning"] = "查询范围可能早于集群事件保留窗口，结果可能不完整"
	}
	writeJSON(w, http.StatusOK, resp)
}

func eventLastTimestamp(obj map[string]any) string {
	for _, key := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		if v := nestedString(obj, key); v != "" {
			return v
		}
	}
	return ""
}

func nestedString(obj map[string]any, fields ...string) string {
	v, _, _ := unstructured.NestedString(obj, fields...)
	return v
}

func nestedMap(obj map[string]any, fields ...string) map[string]any {
	v, _, _ := unstructured.NestedMap(obj, fields...)
	return v
}
