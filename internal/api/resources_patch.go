package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/kube"
)

const (
	mergePatchMediaType      = "application/merge-patch+json"
	strategicMergeMediaType  = "application/strategic-merge-patch+json"
	applyPatchMediaType      = "application/apply-patch+yaml"
	defaultPatchFieldManager = "kubedeck"
)

// patchResource 根据请求媒体类型执行合并补丁、策略合并补丁或 Server-Side Apply。
func (h *handlers) patchResource(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	res, ok := h.validAPIResource(w, r, cluster)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	patchType, ok := parsePatchMediaType(w, r)
	if !ok {
		return
	}
	fieldManager, force, ok := parsePatchOptions(w, r, patchType)
	if !ok {
		return
	}

	body, jsonBody, ok := readPatchBody(w, r)
	if !ok {
		return
	}
	if !validatePatchIdentity(w, r.Context(), jsonBody, res, name, namespace, patchType) {
		return
	}

	data := body
	if patchType != types.ApplyPatchType {
		// 合并类补丁的媒体类型要求 JSON，允许调用方提交 YAML 后由平台规范化。
		data = jsonBody
	}
	updated, err := t.Patch(r.Context(), res.GVR(), namespace, name, patchType, data, kube.PatchOptions{
		DryRun:       dryRunOption(r),
		FieldManager: fieldManager,
		Force:        force,
	})
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	if r.URL.Query().Get("format") == "yaml" {
		writeYAML(w, renderYAML(updated))
		return
	}
	writeJSON(w, http.StatusOK, updated.Object)
}

// parsePatchMediaType 把 Content-Type 映射为补丁类型；不默认猜测类型。
func parsePatchMediaType(w http.ResponseWriter, r *http.Request) (types.PatchType, bool) {
	raw := r.Header.Get("Content-Type")
	if raw == "" {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "缺少 Content-Type"))
		return "", false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "Content-Type 不合法"))
		return "", false
	}
	switch mediaType {
	case mergePatchMediaType:
		return types.MergePatchType, true
	case strategicMergeMediaType:
		return types.StrategicMergePatchType, true
	case applyPatchMediaType:
		return types.ApplyPatchType, true
	default:
		WriteError(r.Context(), w, NewError(CodeUnsupported,
			"仅支持 application/merge-patch+json、application/strategic-merge-patch+json 与 application/apply-patch+yaml"))
		return "", false
	}
}

// parsePatchOptions 解析 fieldManager/force，并校验它们与补丁类型的组合。
func parsePatchOptions(
	w http.ResponseWriter,
	r *http.Request,
	patchType types.PatchType,
) (fieldManager string, force bool, ok bool) {
	q := r.URL.Query()
	fieldManager = q.Get("fieldManager")
	if patchType == types.ApplyPatchType && fieldManager == "" {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "Server-Side Apply 必须提供 fieldManager"))
		return "", false, false
	}
	if patchType != types.ApplyPatchType && fieldManager == "" {
		fieldManager = defaultPatchFieldManager
	}

	switch q.Get("force") {
	case "":
		force = false
	case "true":
		force = true
	case "false":
		force = false
	default:
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "force 必须是 true 或 false"))
		return "", false, false
	}
	if force && patchType != types.ApplyPatchType {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "force 仅适用于 Server-Side Apply"))
		return "", false, false
	}
	return fieldManager, force, true
}

// readPatchBody 读取补丁请求体（上限 1 MB），同时返回原始 YAML 与规范化 JSON。
func readPatchBody(w http.ResponseWriter, r *http.Request) (body, jsonBody []byte, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxYAMLBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			WriteError(r.Context(), w, NewError(CodePayloadTooLarge, "补丁超过 1 MB 上限"))
			return nil, nil, false
		}
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "读取请求体失败"))
		return nil, nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "补丁请求体不能为空"))
		return nil, nil, false
	}
	jsonBody, err = yaml.YAMLToJSON(body)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeUnprocessable, "补丁解析失败："+err.Error()))
		return nil, nil, false
	}
	return body, jsonBody, true
}

// validatePatchIdentity 校验补丁中携带的身份字段与路径一致；Apply 必须是完整对象。
func validatePatchIdentity(
	w http.ResponseWriter,
	ctx context.Context,
	jsonBody []byte,
	res kube.APIResource,
	name, namespace string,
	patchType types.PatchType,
) bool {
	obj, err := unstructuredFromJSON(jsonBody)
	if err != nil {
		WriteError(ctx, w, NewError(CodeUnprocessable, "补丁解析失败："+err.Error()))
		return false
	}
	gvr := res.GVR()
	gvk := obj.GroupVersionKind()
	gvkPresent := gvk.Group != "" || gvk.Version != "" || gvk.Kind != ""

	if patchType == types.ApplyPatchType {
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			WriteError(ctx, w, NewError(CodeUnprocessable, "Server-Side Apply 必须包含 apiVersion 与 kind"))
			return false
		}
		if obj.GetName() == "" {
			WriteError(ctx, w, NewError(CodeUnprocessable, "Server-Side Apply 必须包含 metadata.name"))
			return false
		}
	}

	if gvkPresent {
		if gvk.Group != gvr.Group || gvk.Version != gvr.Version {
			WriteError(ctx, w, NewError(CodeUnprocessable, "apiVersion 与请求路径不一致"))
			return false
		}
		if res.Kind != "" && gvk.Kind != "" && gvk.Kind != res.Kind {
			WriteError(ctx, w, NewError(CodeUnprocessable, "kind 与请求资源类型不一致"))
			return false
		}
	}
	if obj.GetName() != "" && obj.GetName() != name {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.name 与请求路径不一致"))
		return false
	}
	if obj.GetNamespace() != "" && obj.GetNamespace() != namespace {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.namespace 与请求参数不一致"))
		return false
	}
	if !res.Namespaced && obj.GetNamespace() != "" {
		WriteError(ctx, w, NewError(CodeUnprocessable, "集群级资源不能包含 metadata.namespace"))
		return false
	}
	return true
}

// unstructuredFromJSON 解析补丁 JSON；数值保持 json.Number，避免精度丢失。
func unstructuredFromJSON(body []byte) (*unstructured.Unstructured, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	convertNumbers(raw)
	return &unstructured.Unstructured{Object: raw}, nil
}
