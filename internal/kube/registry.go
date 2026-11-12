package kube

import "sync"

// Registry 维护集群名到 ClusterAccess 的映射，按注册顺序返回。
type Registry struct {
	mu    sync.RWMutex
	items map[string]ClusterAccess
	order []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]ClusterAccess)}
}

// Add 注册集群；同名覆盖且保持原有顺序。不发起网络请求。
func (r *Registry) Add(c ClusterAccess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := c.Name()
	if _, exists := r.items[name]; !exists {
		r.order = append(r.order, name)
	}
	r.items[name] = c
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
func (r *Registry) Get(name string) (ClusterAccess, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.items[name]
	return c, ok
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
func (r *Registry) All() []ClusterAccess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ClusterAccess, 0, len(r.order))
	for _, name := range r.order {
		if c, ok := r.items[name]; ok {
			out = append(out, c)
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
