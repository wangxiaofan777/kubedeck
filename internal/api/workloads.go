package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

var (
	scaleable   = map[string]bool{"deployments": true, "statefulsets": true, "replicasets": true}
	restartable = map[string]bool{"deployments": true, "statefulsets": true, "daemonsets": true}
)

type workloadOperation struct {
	ID             string
	Type           string
	Actor          int64
	Cluster        string
	Group          string
	Version        string
	Resource       string
	Namespace      string
	Name           string
	TargetReplicas int64
	StartedAt      time.Time
}

type operationStore struct {
	mu    sync.Mutex
	items map[string]workloadOperation
}

func newOperationStore() *operationStore {
	return &operationStore{items: make(map[string]workloadOperation)}
}

func (s *operationStore) put(op workloadOperation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-time.Hour)
	for id, item := range s.items {
		if item.StartedAt.Before(cutoff) {
			delete(s.items, id)
		}
	}
	s.items[op.ID] = op
}

func (s *operationStore) get(id string) (workloadOperation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, ok := s.items[id]
	return op, ok
}

type idempotentResponse struct {
	Status  int
	Body    map[string]any
	Expires time.Time
}

type idempotencyStore struct {
	mu    sync.Mutex
	items map[string]idempotentResponse
}

func newIdempotencyStore() *idempotencyStore {
	return &idempotencyStore{items: make(map[string]idempotentResponse)}
}

func (s *idempotencyStore) get(key string) (idempotentResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[key]
	if !ok || time.Now().After(item.Expires) {
		delete(s.items, key)
		return idempotentResponse{}, false
	}
	return item, true
}

func (s *idempotencyStore) put(key string, item idempotentResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.items {
		if now.After(v.Expires) {
			delete(s.items, k)
		}
	}
	item.Expires = now.Add(24 * time.Hour)
	s.items[key] = item
}

// scaleWorkload 通过 scale 子资源调整副本数。
func (h *handlers) scaleWorkload(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Group != "apps" || !scaleable[gvr.Resource] {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "该资源不支持伸缩，可操作 deployments/statefulsets/replicasets"))
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	var body struct {
		Replicas int64 `json:"replicas"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Replicas < 0 || body.Replicas > math.MaxInt32 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "replicas 必须在 0 到 2147483647 之间"))
		return
	}
	namespace := r.URL.Query().Get("namespace")
	updated, err := t.Scale(r.Context(), gvr, namespace, name, int32(body.Replicas))
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	op := h.newOperation(r.Context(), chi.URLParam(r, "cluster"), "scale", gvr, namespace, name, body.Replicas)
	writeJSON(w, http.StatusOK, map[string]any{
		"operationId":    op.ID,
		"targetReplicas": body.Replicas,
		"workload":       updated.Object,
	})
}

// restartWorkload 通过更新 Pod 模板注解触发滚动重启；相同幂等键只生效一次。
func (h *handlers) restartWorkload(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Group != "apps" || !restartable[gvr.Resource] {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "该资源不支持重启，可操作 deployments/statefulsets/daemonsets"))
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	idemKey := ""
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		idemKey = strings.Join([]string{
			strconv.FormatInt(id.UserID, 10), gvr.String(), namespace, name, key,
		}, "\x00")
		if cached, ok := h.idem.get(idemKey); ok {
			writeJSON(w, cached.Status, cached.Body)
			return
		}
	}

	current, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	annotations, _, _ := unstructured.NestedStringMap(current.Object,
		"spec", "template", "metadata", "annotations")
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["kubedeck.io/restartedAt"] = now
	if err := unstructured.SetNestedStringMap(current.Object, annotations,
		"spec", "template", "metadata", "annotations"); err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "无法写入重启标记"))
		return
	}
	updated, err := t.Update(r.Context(), gvr, namespace, current, nil)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	op := h.newOperation(r.Context(), chi.URLParam(r, "cluster"), "restart", gvr, namespace, name, replicasOf(updated))
	resp := map[string]any{
		"operationId":     op.ID,
		"restartedAt":     now,
		"resourceVersion": updated.GetResourceVersion(),
	}
	if idemKey != "" {
		h.idem.put(idemKey, idempotentResponse{Status: http.StatusOK, Body: resp})
	}
	writeJSON(w, http.StatusOK, resp)
}

// workloadHistory 返回 Deployment 的历史 revision。
func (h *handlers) workloadHistory(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Resource != "deployments" {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "仅 Deployment 支持版本历史"))
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	deployment, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	revisions, err := h.deploymentRevisions(r.Context(), t, deployment, namespace)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currentRevision": currentRevision(deployment),
		"revisions":       revisions,
	})
}

// rollbackDiff 展示当前模板与目标 revision 的差异。
func (h *handlers) rollbackDiff(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Resource != "deployments" {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "仅 Deployment 支持回滚"))
		return
	}
	revision, err := strconv.Atoi(r.URL.Query().Get("revision"))
	if err != nil || revision <= 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "revision 必须是正整数"))
		return
	}
	namespace := r.URL.Query().Get("namespace")
	deployment, err := t.Get(r.Context(), gvr, namespace, chi.URLParam(r, "name"))
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	rs, err := h.findRevision(r.Context(), t, deployment, namespace, revision)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	currentTemplate, _, _ := unstructured.NestedMap(deployment.Object, "spec", "template")
	targetTemplate, _, _ := unstructured.NestedMap(rs.Object, "spec", "template")
	currentYAML, _ := yaml.Marshal(currentTemplate)
	targetYAML, _ := yaml.Marshal(targetTemplate)
	writeJSON(w, http.StatusOK, map[string]any{
		"revision": revision,
		"diff":     unifiedDiff(string(currentYAML), string(targetYAML)),
		"images":   templateImages(rs),
	})
}

// rollbackWorkload 将 Pod 模板恢复为目标 revision 的内容，由控制器生成新 revision。
func (h *handlers) rollbackWorkload(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Resource != "deployments" {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "仅 Deployment 支持回滚"))
		return
	}
	var body struct {
		Revision int `json:"revision"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Revision <= 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "revision 必须是正整数"))
		return
	}
	namespace := r.URL.Query().Get("namespace")
	name := chi.URLParam(r, "name")
	deployment, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	rs, err := h.findRevision(r.Context(), t, deployment, namespace, body.Revision)
	if err != nil {
		if errors.Is(err, errRevisionUnavailable) {
			e := NewError(CodeUnprocessable, "目标 revision 已被清理，无法回滚")
			e.Details = map[string]any{"revision": body.Revision}
			WriteError(r.Context(), w, e)
			return
		}
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	tmpl, found, err := unstructured.NestedMap(rs.Object, "spec", "template")
	if err != nil || !found {
		WriteError(r.Context(), w, NewError(CodeUnprocessable, "目标 revision 缺少 Pod 模板"))
		return
	}
	if err := unstructured.SetNestedMap(deployment.Object, tmpl, "spec", "template"); err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "无法恢复 Pod 模板"))
		return
	}
	updated, err := t.Update(r.Context(), gvr, namespace, deployment, nil)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deployment":      updated.Object,
		"rolledBackTo":    body.Revision,
		"currentRevision": currentRevision(updated),
	})
}

// pauseWorkload 暂停滚动更新。
func (h *handlers) pauseWorkload(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, true)
}

// resumeWorkload 恢复滚动更新。
func (h *handlers) resumeWorkload(w http.ResponseWriter, r *http.Request) {
	h.setPaused(w, r, false)
}

func (h *handlers) setPaused(w http.ResponseWriter, r *http.Request, paused bool) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	if gvr.Resource != "deployments" {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "仅 Deployment 支持暂停/恢复滚动更新"))
		return
	}
	namespace := r.URL.Query().Get("namespace")
	name := chi.URLParam(r, "name")
	deployment, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	if err := unstructured.SetNestedField(deployment.Object, paused, "spec", "paused"); err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "无法修改暂停状态"))
		return
	}
	updated, err := t.Update(r.Context(), gvr, namespace, deployment, nil)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": paused, "deployment": updated.Object})
}

// evictPod 通过 Eviction 子资源驱逐 Pod；PDB 阻止时返回可读原因。
func (h *handlers) evictPod(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) || !validObjectName(w, r.Context(), namespace) {
		return
	}
	var body struct {
		GracePeriodSeconds *int64 `json:"gracePeriodSeconds"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &body) {
		return
	}
	podGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	pod, err := t.Get(r.Context(), podGVR, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	if err := t.Evict(r.Context(), namespace, name, body.GracePeriodSeconds); err != nil {
		if apierrors.IsTooManyRequests(err) {
			details := map[string]any{"pod": name}
			if pdbName, allowed, found := h.findBlockingPDB(r.Context(), t, namespace, pod.GetLabels()); found {
				details["pdbName"] = pdbName
				details["allowedDisruptions"] = allowed
			}
			e := NewError(CodeRateLimited, "驱逐被 PodDisruptionBudget 阻止")
			e.Details = details
			WriteError(r.Context(), w, e)
			return
		}
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getOperation 返回工作负载操作的收敛进度。
func (h *handlers) getOperation(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	op, ok := h.ops.get(chi.URLParam(r, "id"))
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "操作不存在或已过期"))
		return
	}
	if op.Actor != id.UserID && !id.PlatformAdmin {
		WriteError(r.Context(), w, NewError(CodeForbidden, "无权查看该操作"))
		return
	}

	t, _, err := h.transportFor(r.Context(), id, op.Cluster, op.Namespace)
	if err != nil {
		status, message := transportError(err)
		WriteError(r.Context(), w, NewError(status, message))
		return
	}
	gvr := schema.GroupVersionResource{Group: op.Group, Version: op.Version, Resource: op.Resource}
	obj, err := t.Get(r.Context(), gvr, op.Namespace, op.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			WriteError(r.Context(), w, NewError(CodeNotFound, "工作负载已被删除，操作无法继续"))
			return
		}
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, operationProgress(op, obj))
}

func (h *handlers) newOperation(ctx context.Context, cluster, kind string, gvr schema.GroupVersionResource, namespace, name string, target int64) workloadOperation {
	op := workloadOperation{
		ID:             uuid.NewString(),
		Type:           kind,
		Cluster:        cluster,
		Group:          gvr.Group,
		Version:        gvr.Version,
		Resource:       gvr.Resource,
		Namespace:      namespace,
		Name:           name,
		TargetReplicas: target,
		StartedAt:      time.Now().UTC(),
	}
	if id, ok := auth.IdentityFrom(ctx); ok {
		op.Actor = id.UserID
	}
	h.ops.put(op)
	return op
}

var errRevisionUnavailable = errors.New("api: revision unavailable")

func (h *handlers) deploymentRevisions(ctx context.Context, t kube.Transport, deployment *unstructured.Unstructured, namespace string) ([]map[string]any, error) {
	rsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	list, err := t.List(ctx, rsGVR, namespace, kube.ListOptions{Limit: 500})
	if err != nil {
		return nil, err
	}
	ownerUID := string(deployment.GetUID())
	out := make([]map[string]any, 0)
	for i := range list.Items {
		rs := &list.Items[i]
		if !ownedBy(rs, ownerUID) {
			continue
		}
		rev := revisionOf(rs)
		if rev == 0 {
			continue
		}
		out = append(out, map[string]any{
			"revision":      rev,
			"name":          rs.GetName(),
			"images":        templateImages(rs),
			"replicas":      nestedInt(rs.Object, "status", "replicas"),
			"readyReplicas": nestedInt(rs.Object, "status", "readyReplicas"),
			"createdAt":     rs.GetCreationTimestamp().Time,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["revision"].(int) > out[j]["revision"].(int)
	})
	return out, nil
}

func (h *handlers) findRevision(ctx context.Context, t kube.Transport, deployment *unstructured.Unstructured, namespace string, revision int) (*unstructured.Unstructured, error) {
	rsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	list, err := t.List(ctx, rsGVR, namespace, kube.ListOptions{Limit: 500})
	if err != nil {
		return nil, err
	}
	ownerUID := string(deployment.GetUID())
	for i := range list.Items {
		rs := &list.Items[i]
		if ownedBy(rs, ownerUID) && revisionOf(rs) == revision {
			return rs, nil
		}
	}
	return nil, errRevisionUnavailable
}

func ownedBy(obj *unstructured.Unstructured, uid string) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if string(ref.UID) == uid {
			return true
		}
	}
	return false
}

func revisionOf(rs *unstructured.Unstructured) int {
	rev, _ := strconv.Atoi(rs.GetAnnotations()["deployment.kubernetes.io/revision"])
	return rev
}

func currentRevision(deployment *unstructured.Unstructured) int {
	rev, _ := strconv.Atoi(deployment.GetAnnotations()["deployment.kubernetes.io/revision"])
	return rev
}

func templateImages(obj *unstructured.Unstructured) []string {
	containers, found, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if !found {
		return nil
	}
	images := make([]string, 0, len(containers))
	for _, c := range containers {
		if m, ok := c.(map[string]any); ok {
			if img, ok := m["image"].(string); ok {
				images = append(images, img)
			}
		}
	}
	return images
}

func replicasOf(obj *unstructured.Unstructured) int64 {
	if n, found, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas"); found {
		return n
	}
	return 0
}

func nestedInt(obj map[string]any, fields ...string) int64 {
	n, _, _ := unstructured.NestedInt64(obj, fields...)
	return n
}

// findBlockingPDB 找到阻止驱逐的 PDB。匹配仅覆盖 matchLabels，足以定位常见场景。
func (h *handlers) findBlockingPDB(ctx context.Context, t kube.Transport, namespace string, podLabels map[string]string) (string, int64, bool) {
	pdbGVR := schema.GroupVersionResource{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"}
	list, err := t.List(ctx, pdbGVR, namespace, kube.ListOptions{Limit: 200})
	if err != nil {
		return "", 0, false
	}
	for i := range list.Items {
		pdb := &list.Items[i]
		selector, found, _ := unstructured.NestedStringMap(pdb.Object, "spec", "selector", "matchLabels")
		if !found || !labelsMatch(selector, podLabels) {
			continue
		}
		allowed := nestedInt(pdb.Object, "status", "disruptionsAllowed")
		return pdb.GetName(), allowed, true
	}
	return "", 0, false
}

func labelsMatch(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func operationProgress(op workloadOperation, obj *unstructured.Unstructured) map[string]any {
	specReplicas := replicasOf(obj)
	ready := nestedInt(obj.Object, "status", "readyReplicas")
	updated := nestedInt(obj.Object, "status", "updatedReplicas")
	available := nestedInt(obj.Object, "status", "availableReplicas")
	observed := nestedInt(obj.Object, "status", "observedGeneration")
	generation := obj.GetGeneration()
	elapsed := time.Since(op.StartedAt)

	target := op.TargetReplicas
	if op.Type == "restart" {
		target = specReplicas
	}

	concluded := false
	success := false
	message := ""
	switch op.Type {
	case "scale":
		if specReplicas == target && ready >= target {
			concluded, success = true, true
		}
	case "restart":
		if observed >= generation && updated >= specReplicas && ready >= specReplicas {
			concluded, success = true, true
		}
	}
	if !concluded && elapsed > 5*time.Minute {
		concluded = true
		message = "操作超时，请检查工作负载事件"
	}
	return map[string]any{
		"id":                op.ID,
		"type":              op.Type,
		"cluster":           op.Cluster,
		"namespace":         op.Namespace,
		"name":              op.Name,
		"resource":          op.Resource,
		"targetReplicas":    target,
		"currentReplicas":   specReplicas,
		"readyReplicas":     ready,
		"updatedReplicas":   updated,
		"availableReplicas": available,
		"startedAt":         op.StartedAt,
		"elapsedMs":         elapsed.Milliseconds(),
		"concluded":         concluded,
		"success":           success,
		"message":           message,
	}
}
