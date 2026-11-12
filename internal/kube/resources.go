package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
)

const discoveryTTL = 5 * time.Minute

// APIResource 是资源目录中的一项。
type APIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
}

// GVR 返回资源定位三元组。
func (r APIResource) GVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

// DiscoveryResult 是资源发现结果。FailedGroups 非空时其余资源仍可用。
type DiscoveryResult struct {
	Resources    []APIResource `json:"resources"`
	FailedGroups []string      `json:"failedGroups,omitempty"`
	FetchedAt    time.Time     `json:"fetchedAt"`
}

// CachedDiscovery 返回 5 分钟内的发现结果，避免每次列表都打 apiserver。
func (c *DirectCluster) Discovery(ctx context.Context) (DiscoveryResult, error) {
	c.discMu.Lock()
	if !c.discAt.IsZero() && time.Since(c.discAt) < discoveryTTL {
		cached := c.discCache
		c.discMu.Unlock()
		return cached, nil
	}
	c.discMu.Unlock()

	result, err := discoverResources(ctx, c.disc)
	if err != nil {
		return DiscoveryResult{}, err
	}
	c.discMu.Lock()
	c.discCache = result
	c.discAt = time.Now()
	c.discMu.Unlock()
	return result, nil
}

func discoverResources(ctx context.Context, disc discovery.DiscoveryInterface) (DiscoveryResult, error) {
	lists, err := disc.ServerPreferredResources()
	var failed []string
	if err != nil {
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return DiscoveryResult{}, fmt.Errorf("discover api resources: %w", err)
		}
		var groupErr *discovery.ErrGroupDiscoveryFailed
		if errors.As(err, &groupErr) {
			for gv := range groupErr.Groups {
				failed = append(failed, gv.String())
			}
			sort.Strings(failed)
		}
	}

	resources := make([]APIResource, 0, len(lists)*2)
	for _, list := range lists {
		if list == nil || list.GroupVersion == "" {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") {
				continue // 子资源不进入资源目录
			}
			if !hasVerb(r.Verbs, "list") && !hasVerb(r.Verbs, "get") {
				continue
			}
			resources = append(resources, APIResource{
				Group: gv.Group, Version: gv.Version, Resource: r.Name, Kind: r.Kind,
				Namespaced: r.Namespaced, Verbs: r.Verbs,
			})
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Group != resources[j].Group {
			return resources[i].Group < resources[j].Group
		}
		if resources[i].Resource != resources[j].Resource {
			return resources[i].Resource < resources[j].Resource
		}
		return resources[i].Version < resources[j].Version
	})
	return DiscoveryResult{Resources: resources, FailedGroups: failed, FetchedAt: time.Now().UTC()}, nil
}

func hasVerb(verbs []string, want string) bool {
	for _, v := range verbs {
		if v == want {
			return true
		}
	}
	return false
}

// Create 创建对象；dryRun 非空时只做服务端校验。
func (t *DirectTransport) Create(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, dryRun []string) (*unstructured.Unstructured, error) {
	return t.dynamic.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{
		DryRun: dryRun, FieldManager: "kubedeck",
	})
}

// Update 更新对象。对象携带的 resourceVersion 由 apiserver 做并发校验。
func (t *DirectTransport) Update(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, dryRun []string) (*unstructured.Unstructured, error) {
	return t.dynamic.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{
		DryRun: dryRun, FieldManager: "kubedeck",
	})
}

// DeleteOptions 是删除参数。
type DeleteOptions struct {
	Propagation     metav1.DeletionPropagation
	DryRun          []string
	UID             string
	ResourceVersion string
}

// Delete 删除单个对象，支持传播策略与 UID 前置条件。
func (t *DirectTransport) Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, opts DeleteOptions) error {
	prop := opts.Propagation
	del := metav1.DeleteOptions{PropagationPolicy: &prop, DryRun: opts.DryRun}
	if opts.UID != "" || opts.ResourceVersion != "" {
		del.Preconditions = &metav1.Preconditions{}
		if opts.UID != "" {
			uid := types.UID(opts.UID)
			del.Preconditions.UID = &uid
		}
		if opts.ResourceVersion != "" {
			rv := opts.ResourceVersion
			del.Preconditions.ResourceVersion = &rv
		}
	}
	return t.dynamic.Resource(gvr).Namespace(namespace).Delete(ctx, name, del)
}

// Scale 通过 scale 子资源调整副本数，返回调整后的工作负载对象。
func (t *DirectTransport) Scale(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, replicas int32) (*unstructured.Unstructured, error) {
	scale := map[string]any{
		"apiVersion": "autoscaling/v1",
		"kind":       "Scale",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec":       map[string]any{"replicas": int64(replicas)},
	}
	body, err := json.Marshal(scale)
	if err != nil {
		return nil, err
	}
	path := []string{"apis", gvr.Group, gvr.Version, "namespaces", namespace, gvr.Resource, name, "scale"}
	if gvr.Group == "" {
		path = []string{"api", gvr.Version, "namespaces", namespace, gvr.Resource, name, "scale"}
	}
	_, err = t.rest.Put().
		AbsPath(path...).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		Body(body).
		Do(ctx).
		Raw()
	if err != nil {
		return nil, err
	}
	return t.dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// Evict 通过 pods/eviction 子资源驱逐 Pod；PDB 阻止时返回 apiserver 的 429 错误。
func (t *DirectTransport) Evict(ctx context.Context, namespace, name string, gracePeriodSeconds *int64) error {
	eviction := map[string]any{
		"apiVersion": "policy/v1",
		"kind":       "Eviction",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
	}
	if gracePeriodSeconds != nil {
		eviction["deleteOptions"] = map[string]any{"gracePeriodSeconds": *gracePeriodSeconds}
	}
	body, err := json.Marshal(eviction)
	if err != nil {
		return err
	}
	_, err = t.rest.Post().
		AbsPath("api", "v1", "namespaces", namespace, "pods", name, "eviction").
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		Body(body).
		Do(ctx).
		Raw()
	return err
}
