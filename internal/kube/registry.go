package kube

import "sync"

// Registry 维护集群名到 Transport 的映射，按注册顺序返回。
// 开发模式下只注册一个本地集群；生产模式由数据库中的集群登记驱动（后续任务）。
type Registry struct {
	mu    sync.RWMutex
	items map[string]Transport
	order []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]Transport)}
}

// Add 注册集群；同名覆盖且保持原有顺序。不发起网络请求。
func (r *Registry) Add(t Transport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.items[name]; !exists {
		r.order = append(r.order, name)
	}
	r.items[name] = t
}

// Remove 移除集群；不存在时返回 false。
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[name]; !ok {
		return false
	}
	delete(r.items, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	return true
}

// Get 按名称查找集群。
func (r *Registry) Get(name string) (Transport, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[name]
	return t, ok
}

// Names 按注册顺序返回集群名。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// All 按注册顺序返回全部集群。
func (r *Registry) All() []Transport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Transport, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.items[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Len 返回已注册集群数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.items)
}
