package kube

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type nodeTransport struct {
	overviewTransport
	pods       []unstructured.Unstructured
	evicted    []string
	evictErr   map[string]error
	patchBody  []byte
	nodeExists bool
}

func (f *nodeTransport) Get(_ context.Context, gvr schema.GroupVersionResource, _, _ string) (*unstructured.Unstructured, error) {
	if gvr.Resource != "nodes" || !f.nodeExists {
		return nil, apierrors.NewNotFound(gvr.GroupResource(), "node")
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"metadata": map[string]any{"name": "worker-1"},
	}}, nil
}

func (f *nodeTransport) List(_ context.Context, gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
	if gvr.Resource != "pods" {
		return &unstructured.UnstructuredList{}, nil
	}
	return &unstructured.UnstructuredList{Items: f.pods}, nil
}

func (f *nodeTransport) Patch(_ context.Context, gvr schema.GroupVersionResource, _, _ string, _ types.PatchType, data []byte, _ PatchOptions) (*unstructured.Unstructured, error) {
	f.patchBody = data
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Node",
		"metadata": map[string]any{"name": "worker-1", "resourceVersion": "2"},
		"spec":     map[string]any{"unschedulable": true},
	}}, nil
}

func (f *nodeTransport) Evict(_ context.Context, namespace, name string, _ *int64) error {
	if err := f.evictErr[namespace+"/"+name]; err != nil {
		return err
	}
	f.evicted = append(f.evicted, namespace+"/"+name)
	return nil
}

func drainPod(namespace, name string, mutate func(map[string]any)) unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"status":   map[string]any{"phase": "Running"},
	}
	if mutate != nil {
		mutate(obj)
	}
	return unstructured.Unstructured{Object: obj}
}

func TestDrainNodeFiltersAndEvicts(t *testing.T) {
	controller := true
	transport := &nodeTransport{
		nodeExists: true,
		evictErr: map[string]error{
			"prod/db-0": apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "db-0",
				errors.New("Cannot evict pod as it would violate the pod's disruption budget")),
		},
		pods: []unstructured.Unstructured{
			drainPod("prod", "web-0", func(o map[string]any) {
				o["metadata"].(map[string]any)["ownerReferences"] = []any{
					map[string]any{"kind": "ReplicaSet", "name": "web", "controller": controller},
				}
			}),
			drainPod("prod", "db-0", func(o map[string]any) {
				o["metadata"].(map[string]any)["ownerReferences"] = []any{
					map[string]any{"kind": "StatefulSet", "name": "db", "controller": controller},
				}
			}),
			drainPod("prod", "ds-0", func(o map[string]any) {
				o["metadata"].(map[string]any)["ownerReferences"] = []any{
					map[string]any{"kind": "DaemonSet", "name": "agent", "controller": controller},
				}
			}),
			drainPod("prod", "loose", func(o map[string]any) {
				o["metadata"].(map[string]any)["annotations"] = map[string]any{}
			}),
			drainPod("prod", "data-0", func(o map[string]any) {
				o["metadata"].(map[string]any)["ownerReferences"] = []any{
					map[string]any{"kind": "StatefulSet", "name": "data", "controller": controller},
				}
				o["spec"] = map[string]any{"volumes": []any{
					map[string]any{"name": "data", "emptyDir": map[string]any{}},
				}}
			}),
			drainPod("prod", "mirror", func(o map[string]any) {
				o["metadata"].(map[string]any)["annotations"] = map[string]any{
					"kubernetes.io/config.mirror": "abc",
				}
			}),
		},
	}

	report, err := DrainNode(context.Background(), transport, "worker-1", DrainOptions{
		Cordon:           true,
		IgnoreDaemonSets: true,
		Timeout:          30 * time.Second,
	})
	if err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if !report.Cordoned {
		t.Fatal("expected cordon")
	}
	if report.Evicted != 1 || report.Failed != 1 || report.Skipped != 2 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Results) != 6 {
		t.Fatalf("results = %d, want 6", len(report.Results))
	}
	var sawPDB, sawUnmanaged, sawLocalStorage bool
	for _, item := range report.Results {
		switch item.Code {
		case "PDB":
			sawPDB = true
		case "UNMANAGED":
			sawUnmanaged = true
		case "LOCAL_STORAGE":
			sawLocalStorage = true
		}
	}
	if !sawPDB || !sawUnmanaged || !sawLocalStorage {
		t.Fatalf("missing classifications: %+v", report.Results)
	}
	if len(transport.evicted) != 1 || transport.evicted[0] != "prod/web-0" {
		t.Fatalf("evicted = %v", transport.evicted)
	}
}

func TestDrainNodeForceAndDeleteLocalData(t *testing.T) {
	transport := &nodeTransport{
		nodeExists: true,
		pods: []unstructured.Unstructured{
			drainPod("prod", "loose", nil),
			drainPod("prod", "data-0", func(o map[string]any) {
				o["spec"] = map[string]any{"volumes": []any{
					map[string]any{"name": "data", "emptyDir": map[string]any{}},
				}}
			}),
		},
	}
	report, err := DrainNode(context.Background(), transport, "worker-1", DrainOptions{
		Force:              true,
		DeleteEmptyDirData: true,
		IgnoreDaemonSets:   true,
	})
	if err != nil {
		t.Fatalf("DrainNode: %v", err)
	}
	if report.Evicted != 2 || report.Blocked != 0 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestSetNodeSchedulablePatch(t *testing.T) {
	transport := &nodeTransport{nodeExists: true}
	if _, err := SetNodeSchedulable(context.Background(), transport, "worker-1", false); err != nil {
		t.Fatalf("SetNodeSchedulable: %v", err)
	}
	var patch map[string]any
	if err := json.Unmarshal(transport.patchBody, &patch); err != nil {
		t.Fatalf("patch: %v", err)
	}
	spec, _ := patch["spec"].(map[string]any)
	if spec["unschedulable"] != true {
		t.Fatalf("patch = %s", transport.patchBody)
	}
}
