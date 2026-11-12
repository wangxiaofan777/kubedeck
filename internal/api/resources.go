package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/kube"
)

// listResponse 是资源列表响应。
type listResponse struct {
	Items     []map[string]any `json:"items"`
	Continue  string           `json:"continue,omitempty"`
	Remaining *int64           `json:"remainingItemCount,omitempty"`
}

// apiResources 返回资源目录；部分 API 组不可用时仍返回其余资源。
func (h *handlers) apiResources(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	cluster, ok := h.accessibleCluster(w, r, id)
	if !ok {
		return
	}
	discovery, err := cluster.Discovery(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, discovery)
}

// listResources 列出任意 GVR 的资源。
func (h *handlers) listResources(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}

	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}

	list, err := t.List(r.Context(), gvr, r.URL.Query().Get("namespace"), kube.ListOptions{
		Limit:         limit,
		Continue:      r.URL.Query().Get("continue"),
		LabelSelector: r.URL.Query().Get("labelSelector"),
		FieldSelector: r.URL.Query().Get("fieldSelector"),
	})
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	items := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		items = append(items, list.Items[i].Object)
	}
	sortItems(items, r.URL.Query().Get("sortBy"), r.URL.Query().Get("order"))

	writeJSON(w, http.StatusOK, listResponse{
		Items:     items,
		Continue:  list.GetContinue(),
		Remaining: list.GetRemainingItemCount(),
	})
}

// getResource 读取单个资源；?format=yaml 返回 YAML 文本。
func (h *handlers) getResource(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if !validObjectName(w, r.Context(), chi.URLParam(r, "name")) {
		return
	}

	obj, err := t.Get(r.Context(), gvr, r.URL.Query().Get("namespace"), chi.URLParam(r, "name"))
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	if r.URL.Query().Get("format") == "yaml" {
		writeYAML(w, renderYAML(obj))
		return
	}
	writeJSON(w, http.StatusOK, obj.Object)
}

// getResourceDetail 返回对象、Owner 引用与最近事件。
func (h *handlers) getResourceDetail(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	obj, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	resp := map[string]any{
		"object":          obj.Object,
		"ownerReferences": obj.GetOwnerReferences(),
		"events":          []map[string]any{},
	}
	if uid := string(obj.GetUID()); uid != "" && namespace != "" {
		eventsGVR := resourceGVR("", "v1", "events")
		list, err := t.List(r.Context(), eventsGVR, namespace, kube.ListOptions{
			Limit:         100,
			FieldSelector: "involvedObject.uid=" + uid,
		})
		if err == nil {
			events := make([]map[string]any, 0, len(list.Items))
			for i := range list.Items {
				events = append(events, list.Items[i].Object)
			}
			sort.SliceStable(events, func(i, j int) bool {
				return lastTimestamp(events[i]) > lastTimestamp(events[j])
			})
			resp["events"] = events
		} else {
			resp["eventsError"] = upstreamReason(err)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// renderYAML 移除 managedFields 后输出 YAML；不修改缓存中的对象。
func renderYAML(obj *unstructured.Unstructured) []byte {
	clean := obj.DeepCopy()
	if meta, ok := clean.Object["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
	}
	body, err := yaml.Marshal(clean.Object)
	if err != nil {
		return []byte("")
	}
	return body
}

func lastTimestamp(event map[string]any) string {
	for _, key := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		if v, ok := event[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func sortItems(items []map[string]any, sortBy, order string) {
	if sortBy != "name" && sortBy != "creationTimestamp" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		return itemSortKey(items[i], sortBy) < itemSortKey(items[j], sortBy)
	})
	if strings.EqualFold(order, "desc") {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
}

func itemSortKey(item map[string]any, field string) string {
	meta, _ := item["metadata"].(map[string]any)
	v, _ := meta[field].(string)
	return v
}
