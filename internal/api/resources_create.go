package api

import (
	"context"
	"net/http"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubedeck/kubedeck/internal/kube"
)

// createResource 创建任意已发现资源；命名空间与对象身份在提交前校验。
func (h *handlers) createResource(w http.ResponseWriter, r *http.Request) {
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}
	res, ok := h.validAPIResource(w, r, cluster)
	if !ok {
		return
	}
	if !resourceSupports(res, "create") {
		WriteError(r.Context(), w, NewError(CodeUnsupported, "该资源类型不支持创建"))
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if !validCreateNamespace(w, r, res, namespace) {
		return
	}

	obj, _, ok := decodeYAMLBody(w, r)
	if !ok {
		return
	}
	if obj.GetResourceVersion() != "" {
		WriteError(r.Context(), w, NewError(CodeUnprocessable, "创建请求不得包含 metadata.resourceVersion"))
		return
	}
	if !validateCreateIdentity(w, r.Context(), obj, res, namespace) {
		return
	}

	created, err := t.Create(r.Context(), res.GVR(), namespace, obj, dryRunOption(r))
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	if r.URL.Query().Get("format") == "yaml" {
		writeYAMLStatus(w, http.StatusCreated, renderYAML(created))
		return
	}
	writeJSON(w, http.StatusCreated, created.Object)
}

// resourceSupports 判断发现结果是否允许某动词；Verbs 为空时交由上游兜底。
func resourceSupports(res kube.APIResource, verb string) bool {
	if len(res.Verbs) == 0 {
		return true
	}
	return slices.Contains(res.Verbs, verb)
}

// validCreateNamespace 校验命名空间是否与资源作用域匹配。
func validCreateNamespace(w http.ResponseWriter, r *http.Request, res kube.APIResource, namespace string) bool {
	if res.Namespaced && namespace == "" {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "命名空间级资源必须指定 namespace"))
		return false
	}
	if !res.Namespaced && namespace != "" {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "集群级资源不能指定 namespace"))
		return false
	}
	return true
}

// validateCreateIdentity 校验创建请求体与路径/发现结果一致。
func validateCreateIdentity(
	w http.ResponseWriter,
	ctx context.Context,
	obj *unstructured.Unstructured,
	res kube.APIResource,
	namespace string,
) bool {
	gvr := res.GVR()
	gvk := obj.GroupVersionKind()
	if gvk.Group != gvr.Group || gvk.Version != gvr.Version {
		WriteError(ctx, w, NewError(CodeUnprocessable, "apiVersion 与请求路径不一致"))
		return false
	}
	if obj.GetKind() == "" {
		WriteError(ctx, w, NewError(CodeUnprocessable, "kind 不能为空"))
		return false
	}
	// discovery 未提供 Kind 时退化为只校验 apiVersion，避免误拒。
	if res.Kind != "" && obj.GetKind() != res.Kind {
		WriteError(ctx, w, NewError(CodeUnprocessable, "kind 与请求资源类型不一致"))
		return false
	}
	name := obj.GetName()
	if name == "" {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.name 不能为空"))
		return false
	}
	if !isValidObjectName(name) {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.name 不合法"))
		return false
	}
	if res.Namespaced && obj.GetNamespace() != "" && obj.GetNamespace() != namespace {
		WriteError(ctx, w, NewError(CodeUnprocessable, "metadata.namespace 与请求参数不一致"))
		return false
	}
	if !res.Namespaced && obj.GetNamespace() != "" {
		WriteError(ctx, w, NewError(CodeUnprocessable, "集群级资源不能包含 metadata.namespace"))
		return false
	}
	return true
}

// dryRunOption 把查询参数转换为 apiserver 的 dry-run 选项。
func dryRunOption(r *http.Request) []string {
	if r.URL.Query().Get("dryRun") == "All" {
		return []string{metav1.DryRunAll}
	}
	return nil
}
