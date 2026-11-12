package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/pmezard/go-difflib/difflib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/kube"
)

const maxYAMLBytes = 1 << 20

// diffResource 展示提交前后的 Diff，并提示不可变字段。
func (h *handlers) diffResource(w http.ResponseWriter, r *http.Request) {
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

	obj, body, ok := decodeYAMLBody(w, r)
	if !ok {
		return
	}
	if !validateObjectIdentity(w, r.Context(), obj, gvr, name, namespace) {
		return
	}
	current, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	diff := unifiedDiff(sanitizeYAML(current), string(body))
	warnings := immutableWarnings(current, obj)
	writeJSON(w, http.StatusOK, map[string]any{
		"diff":                     diff,
		"immutableFields":          warnings,
		"currentResourceVersion":   current.GetResourceVersion(),
		"submittedResourceVersion": obj.GetResourceVersion(),
	})
}

// updateResource 提交 YAML；要求 resourceVersion，冲突时返回 409 与最新版本。
func (h *handlers) updateResource(w http.ResponseWriter, r *http.Request) {
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
	obj, _, ok := decodeYAMLBody(w, r)
	if !ok {
		return
	}
	if !validateObjectIdentity(w, r.Context(), obj, gvr, name, namespace) {
		return
	}
	if obj.GetResourceVersion() == "" {
		WriteError(r.Context(), w, NewError(CodeUnprocessable, "提交内容必须包含 metadata.resourceVersion"))
		return
	}

	current, err := t.Get(r.Context(), gvr, namespace, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	if current.GetResourceVersion() != obj.GetResourceVersion() {
		writeConflict(w, r.Context(), current.GetResourceVersion())
		return
	}
	if warns := immutableWarnings(current, obj); len(warns) > 0 {
		e := NewError(CodeUnprocessable, strings.Join(warns, "；"))
		e.Details = map[string]any{"immutableFields": warns}
		WriteError(r.Context(), w, e)
		return
	}

	dryRun := []string(nil)
	if r.URL.Query().Get("dryRun") == "All" {
		dryRun = []string{metav1.DryRunAll}
	}
	updated, err := t.Update(r.Context(), gvr, namespace, obj, dryRun)
	if err != nil {
		if apierrors.IsConflict(err) {
			latest, getErr := t.Get(r.Context(), gvr, namespace, name)
			if getErr == nil {
				writeConflict(w, r.Context(), latest.GetResourceVersion())
				return
			}
		}
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	if r.URL.Query().Get("format") == "yaml" {
		writeYAML(w, renderYAML(updated))
		return
	}
	writeJSON(w, http.StatusOK, updated.Object)
}

func writeConflict(w http.ResponseWriter, ctx context.Context, latest string) {
	e := NewError(CodeConflict, "对象已被修改，请刷新后重试")
	e.Details = map[string]any{"latestResourceVersion": latest}
	WriteError(ctx, w, e)
}

// deleteResource 删除单个对象，支持传播策略、dryRun 与前置条件。
func (h *handlers) deleteResource(w http.ResponseWriter, r *http.Request) {
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
	opts, err := parseDeleteOptions(r)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}
	if err := t.Delete(r.Context(), gvr, r.URL.Query().Get("namespace"), name, opts); err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type deleteItemResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// batchDelete 逐个删除并汇总结果；单项失败不会中断其余对象。
func (h *handlers) batchDelete(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	gvr, ok := h.validGVR(w, r, cluster)
	if !ok {
		return
	}
	var body struct {
		Names             []string `json:"names"`
		PropagationPolicy string   `json:"propagationPolicy"`
		DryRun            bool     `json:"dryRun"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Names) == 0 || len(body.Names) > 100 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "names 数量必须在 1-100 之间"))
		return
	}
	propagation, err := parsePropagation(body.PropagationPolicy)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}
	namespace := r.URL.Query().Get("namespace")
	dryRun := []string(nil)
	if body.DryRun {
		dryRun = []string{metav1.DryRunAll}
	}

	results := make([]deleteItemResult, 0, len(body.Names))
	succeeded := 0
	for _, name := range body.Names {
		if !validObjectName(w, r.Context(), name) {
			results = append(results, deleteItemResult{Name: name, Success: false, Code: string(CodeInvalidArgument), Message: "对象名不合法"})
			continue
		}
		err := t.Delete(r.Context(), gvr, namespace, name, kube.DeleteOptions{
			Propagation: propagation, DryRun: dryRun,
		})
		if err != nil {
			e := FromUpstream(err)
			results = append(results, deleteItemResult{Name: name, Success: false, Code: string(e.Code), Message: e.Message})
			continue
		}
		succeeded++
		results = append(results, deleteItemResult{Name: name, Success: true})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results, "succeeded": succeeded, "failed": len(body.Names) - succeeded,
	})
}

func parseDeleteOptions(r *http.Request) (kube.DeleteOptions, error) {
	q := r.URL.Query()
	propagation, err := parsePropagation(q.Get("propagationPolicy"))
	if err != nil {
		return kube.DeleteOptions{}, err
	}
	opts := kube.DeleteOptions{
		Propagation:     propagation,
		UID:             q.Get("uid"),
		ResourceVersion: q.Get("resourceVersion"),
	}
	if q.Get("dryRun") == "All" {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	return opts, nil
}

func parsePropagation(raw string) (metav1.DeletionPropagation, error) {
	switch strings.ToLower(raw) {
	case "", "background":
		return metav1.DeletePropagationBackground, nil
	case "foreground":
		return metav1.DeletePropagationForeground, nil
	case "orphan":
		return metav1.DeletePropagationOrphan, nil
	default:
		return "", errors.New("propagationPolicy 必须是 Background、Foreground 或 Orphan")
	}
}

// decodeYAMLBody 读取并解析 YAML，限制 1 MB。
func decodeYAMLBody(w http.ResponseWriter, r *http.Request) (*unstructured.Unstructured, []byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxYAMLBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			WriteError(r.Context(), w, NewError(CodePayloadTooLarge, "YAML 超过 1 MB 上限"))
			return nil, nil, false
		}
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "读取请求体失败"))
		return nil, nil, false
	}
	obj, err := parseYAMLObject(body)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeUnprocessable, "YAML 解析失败："+err.Error()))
		return nil, nil, false
	}
	return obj, body, true
}

func parseYAMLObject(body []byte) (*unstructured.Unstructured, error) {
	jsonBytes, err := yaml.YAMLToJSON(body)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(jsonBytes))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	convertNumbers(raw)
	if len(raw) == 0 {
		return nil, errors.New("empty document")
	}
	return &unstructured.Unstructured{Object: raw}, nil
}

func convertNumbers(v any) {
	switch value := v.(type) {
	case map[string]any:
		for k, item := range value {
			if n, ok := item.(json.Number); ok {
				value[k] = numberValue(n)
				continue
			}
			convertNumbers(item)
		}
	case []any:
		for i, item := range value {
			if n, ok := item.(json.Number); ok {
				value[i] = numberValue(n)
				continue
			}
			convertNumbers(item)
		}
	}
}

func numberValue(n json.Number) any {
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return f
	}
	return n.String()
}

// validateObjectIdentity 校验路径与对象身份一致，防止把对象写到别的资源下。
func validateObjectIdentity(w http.ResponseWriter, ctx context.Context, obj *unstructured.Unstructured, gvr schema.GroupVersionResource, name, namespace string) bool {
	gvk := obj.GroupVersionKind()
	if gvk.Group != gvr.Group || gvk.Version != gvr.Version {
		WriteError(ctx, w, NewError(CodeUnprocessable, "apiVersion 与请求路径不一致"))
		return false
	}
	if obj.GetName() != name {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.name 与请求路径不一致"))
		return false
	}
	objNS := obj.GetNamespace()
	if objNS != "" && objNS != namespace {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.namespace 与请求参数不一致"))
		return false
	}
	return true
}

func sanitizeYAML(obj *unstructured.Unstructured) string {
	clean := obj.DeepCopy()
	if meta, ok := clean.Object["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
	}
	body, err := yaml.Marshal(clean.Object)
	if err != nil {
		return ""
	}
	return string(body)
}

func unifiedDiff(before, after string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(before), B: difflib.SplitLines(after),
		FromFile: "current", ToFile: "submitted", Context: 3,
	})
	if err != nil {
		return ""
	}
	return diff
}

// immutableWarnings 在提交前提示已知不可变字段，避免提交后才收到服务端错误。
func immutableWarnings(current, submitted *unstructured.Unstructured) []string {
	var warnings []string
	if current.GetKind() == "Deployment" {
		oldSel, _, _ := unstructured.NestedMap(current.Object, "spec", "selector")
		newSel, _, _ := unstructured.NestedMap(submitted.Object, "spec", "selector")
		if !reflect.DeepEqual(oldSel, newSel) {
			warnings = append(warnings, "Deployment 的 spec.selector 不可变，请新建工作负载后迁移流量")
		}
	}
	return warnings
}
