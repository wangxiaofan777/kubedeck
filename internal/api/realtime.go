package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

const (
	maxSubscriptionsPerConn = 20
	maxConnsPerUser         = 5
	wsWriteQueue            = 256
	wsPingPeriod            = 30 * time.Second
	wsReadDeadline          = 90 * time.Second
	wsPermissionCheck       = 30 * time.Second
)

type streamRequest struct {
	Type            string `json:"type"`
	ID              string `json:"id"`
	Group           string `json:"group"`
	Version         string `json:"version"`
	Resource        string `json:"resource"`
	Namespace       string `json:"namespace"`
	LabelSelector   string `json:"labelSelector"`
	ResourceVersion string `json:"resourceVersion"`
}

type resyncNotice struct {
	ID     string
	Reason string
}

type streamConn struct {
	h        *handlers
	identity auth.Identity
	cluster  string
	ws       *websocket.Conn
	out      chan []byte
	resync   chan resyncNotice
	ctx      context.Context
	cancel   context.CancelFunc
	writeMu  sync.Mutex

	mu   sync.Mutex
	subs map[string]*subscription
}

type subscription struct {
	ID        string
	Namespace string
	Cancel    context.CancelFunc
}

// stream 建立多路复用 WebSocket 连接。
func (h *handlers) stream(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	if !h.acquireWS(id.UserID) {
		WriteError(r.Context(), w, NewError(CodeRateLimited, "实时连接数已达上限，请关闭其他页面或稍后重试"))
		return
	}
	release := func() { h.releaseWS(id.UserID) }

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkWSOrigin,
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		release()
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	sc := &streamConn{
		h:        h,
		identity: id,
		cluster:  chi.URLParam(r, "cluster"),
		ws:       ws,
		out:      make(chan []byte, wsWriteQueue),
		resync:   make(chan resyncNotice, 8),
		ctx:      ctx,
		cancel:   cancel,
		subs:     make(map[string]*subscription),
	}
	defer func() {
		cancel()
		sc.closeAll()
		_ = ws.Close()
		release()
	}()

	go sc.writeLoop()
	sc.readLoop()
}

func (h *handlers) checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 非浏览器客户端
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range h.allowedOrigins {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

func (h *handlers) acquireWS(userID int64) bool {
	h.wsMu.Lock()
	defer h.wsMu.Unlock()
	if h.wsCount[userID] >= maxConnsPerUser {
		return false
	}
	h.wsCount[userID]++
	return true
}

func (h *handlers) releaseWS(userID int64) {
	h.wsMu.Lock()
	defer h.wsMu.Unlock()
	if h.wsCount[userID] <= 1 {
		delete(h.wsCount, userID)
		return
	}
	h.wsCount[userID]--
}

func (c *streamConn) readLoop() {
	c.ws.SetReadLimit(1 << 20)
	_ = c.ws.SetReadDeadline(time.Now().Add(wsReadDeadline))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})
	for {
		var req streamRequest
		if err := c.ws.ReadJSON(&req); err != nil {
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(wsReadDeadline))
		switch req.Type {
		case "ping":
			c.send(map[string]any{"type": "pong", "time": time.Now().UTC()})
		case "subscribe":
			c.subscribe(req)
		case "unsubscribe":
			c.unsubscribe(req.ID)
		default:
			c.send(map[string]any{"type": "error", "id": req.ID, "code": "INVALID_ARGUMENT", "message": "未知的帧类型"})
		}
	}
}

func (c *streamConn) writeLoop() {
	ping := time.NewTicker(wsPingPeriod)
	permission := time.NewTicker(wsPermissionCheck)
	defer ping.Stop()
	defer permission.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg := <-c.out:
			if err := c.write(websocket.TextMessage, msg); err != nil {
				c.cancel()
				return
			}
		case notice := <-c.resync:
			b, _ := json.Marshal(map[string]any{
				"type": "resync", "id": notice.ID, "reason": notice.Reason,
			})
			if err := c.write(websocket.TextMessage, b); err != nil {
				c.cancel()
				return
			}
		case <-ping.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				c.cancel()
				return
			}
		case <-permission.C:
			c.checkPermissions()
		}
	}
}

func (c *streamConn) write(messageType int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(messageType, payload)
}

func (c *streamConn) send(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case c.out <- b:
		return true
	default:
		return false
	}
}

func (c *streamConn) subscribe(req streamRequest) {
	if req.ID == "" || len(req.ID) > 64 || req.Version == "" || req.Resource == "" {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "INVALID_ARGUMENT", "message": "订阅缺少 id/version/resource"})
		return
	}
	if len(req.LabelSelector) > 1024 || len(req.ResourceVersion) > 128 {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "INVALID_ARGUMENT", "message": "选择器或资源版本过长"})
		return
	}
	group := req.Group
	if group == "_" {
		group = ""
	}
	gvr := schema.GroupVersionResource{Group: group, Version: req.Version, Resource: req.Resource}

	allowed, err := c.h.clusterAccess(c.ctx, c.identity, c.cluster, req.Namespace)
	if err != nil || !allowed {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "FORBIDDEN", "message": "无权订阅该命名空间"})
		return
	}
	cluster, ok := c.h.cluster(c.cluster)
	if !ok {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "NOT_FOUND", "message": "集群不存在"})
		return
	}
	if info := cluster.Info(c.ctx); info.Status == kube.StatusUnreachable {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "CLUSTER_UNREACHABLE", "message": "目标集群不可达"})
		return
	}
	transport, err := cluster.TransportFor(c.identity.UserRef())
	if err != nil {
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "INTERNAL", "message": "无法建立集群连接"})
		return
	}

	c.mu.Lock()
	if len(c.subs) >= maxSubscriptionsPerConn {
		c.mu.Unlock()
		c.send(map[string]any{"type": "error", "id": req.ID, "code": "RATE_LIMITED", "message": "订阅数量已达上限"})
		return
	}
	if old, exists := c.subs[req.ID]; exists {
		old.Cancel()
	}
	subCtx, cancel := context.WithCancel(c.ctx)
	sub := &subscription{ID: req.ID, Namespace: req.Namespace, Cancel: cancel}
	c.subs[req.ID] = sub
	c.mu.Unlock()

	go c.watchLoop(subCtx, sub, transport, gvr, req)
}

func (c *streamConn) unsubscribe(id string) {
	c.mu.Lock()
	sub, ok := c.subs[id]
	if ok {
		delete(c.subs, id)
	}
	c.mu.Unlock()
	if ok {
		sub.Cancel()
	}
}

func (c *streamConn) closeAll() {
	c.mu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for _, sub := range c.subs {
		subs = append(subs, sub)
	}
	c.subs = make(map[string]*subscription)
	c.mu.Unlock()
	for _, sub := range subs {
		sub.Cancel()
	}
}

func (c *streamConn) watchLoop(ctx context.Context, sub *subscription, t kube.Transport, gvr schema.GroupVersionResource, req streamRequest) {
	defer func() {
		c.mu.Lock()
		if current, ok := c.subs[req.ID]; ok && current == sub {
			delete(c.subs, req.ID)
		}
		c.mu.Unlock()
	}()

	watcher, err := t.Watch(ctx, gvr, req.Namespace, kube.WatchOptions{
		ResourceVersion: req.ResourceVersion,
		LabelSelector:   req.LabelSelector,
	})
	if err != nil {
		c.watchError(req.ID, err)
		return
	}
	defer watcher.Stop()

	if !c.send(map[string]any{
		"type": "subscribed", "id": req.ID, "resourceVersion": req.ResourceVersion,
	}) {
		c.requestResync(req.ID, "slow consumer")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				c.requestResync(req.ID, "watch closed")
				return
			}
			if event.Type == watch.Error {
				c.watchError(req.ID, apierrors.FromObject(event.Object))
				return
			}
			if event.Type == watch.Bookmark {
				continue
			}
			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if !c.send(map[string]any{
				"type": "event", "id": req.ID, "eventType": string(event.Type), "object": summarize(obj),
			}) {
				c.requestResync(req.ID, "slow consumer")
				return
			}
		}
	}
}

func (c *streamConn) watchError(id string, err error) {
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) {
		c.requestResync(id, "resource version expired")
		return
	}
	code := "INTERNAL"
	switch {
	case apierrors.IsForbidden(err):
		code = "FORBIDDEN"
	case apierrors.IsUnauthorized(err):
		code = "UNAUTHENTICATED"
	case apierrors.IsNotFound(err):
		code = "NOT_FOUND"
	}
	c.send(map[string]any{"type": "error", "id": id, "code": code, "message": "订阅已终止"})
	_ = err
}

func (c *streamConn) requestResync(id, reason string) {
	select {
	case c.resync <- resyncNotice{ID: id, Reason: reason}:
	default:
	}
}

// checkPermissions 周期性复核平台绑定，权限回收后 60 秒内终止订阅。
func (c *streamConn) checkPermissions() {
	c.mu.Lock()
	type item struct{ id, namespace string }
	items := make([]item, 0, len(c.subs))
	for id, sub := range c.subs {
		items = append(items, item{id: id, namespace: sub.Namespace})
	}
	c.mu.Unlock()
	for _, it := range items {
		allowed, err := c.h.clusterAccess(c.ctx, c.identity, c.cluster, it.namespace)
		if err != nil || !allowed {
			c.unsubscribe(it.id)
			c.send(map[string]any{"type": "error", "id": it.id, "code": "FORBIDDEN", "message": "权限已被回收，订阅已终止"})
		}
	}
}

func summarize(obj *unstructured.Unstructured) map[string]any {
	meta, _ := obj.Object["metadata"].(map[string]any)
	out := map[string]any{
		"apiVersion": obj.GetAPIVersion(),
		"kind":       obj.GetKind(),
		"metadata": map[string]any{
			"name":              obj.GetName(),
			"namespace":         obj.GetNamespace(),
			"uid":               meta["uid"],
			"resourceVersion":   meta["resourceVersion"],
			"creationTimestamp": meta["creationTimestamp"],
			"labels":            meta["labels"],
			"ownerReferences":   meta["ownerReferences"],
		},
	}
	if status, ok := obj.Object["status"].(map[string]any); ok {
		out["status"] = status
	}
	return out
}
