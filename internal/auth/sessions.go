package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemorySessions 是进程内会话存储。会话 Cookie 只携带随机 ID，身份与访问令牌
// 都留在服务端（design D6：会话不入 PostgreSQL）。单副本部署直接可用；
// 多副本部署需要共享存储（Redis），该扩展点由 SessionStore 接口保留。
type MemorySessions struct {
	mu      sync.Mutex
	items   map[string]Session
	maxUser int
	ttl     time.Duration
	now     func() time.Time
	nextID  func() (string, error)
}

// NewMemorySessions 创建会话存储。maxPerUser <= 0 时默认 5，ttl <= 0 时默认 8 小时。
func NewMemorySessions(maxPerUser int, ttl time.Duration) *MemorySessions {
	if maxPerUser <= 0 {
		maxPerUser = 5
	}
	if ttl <= 0 {
		ttl = 8 * time.Hour
	}
	return &MemorySessions{
		items:   make(map[string]Session),
		maxUser: maxPerUser,
		ttl:     ttl,
		now:     time.Now,
		nextID:  randomID,
	}
}

// Create 建立会话；同一用户超过上限时使最早建立的会话失效。
func (m *MemorySessions) Create(id Identity, accessToken string) (Session, error) {
	sid, err := m.nextID()
	if err != nil {
		return Session{}, err
	}
	now := m.now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.deleteExpiredLocked(now)
	if err := m.enforceLimitLocked(id.UserID, m.maxUser-1); err != nil {
		return Session{}, err
	}
	s := Session{
		ID:            sid,
		Identity:      id,
		AccessToken:   accessToken,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(m.ttl).Unix(),
		LastValidated: now.Unix(),
	}
	m.items[sid] = s
	return s, nil
}

// Get 读取有效会话；不存在或过期返回 false。
func (m *MemorySessions) Get(sid string) (Session, bool) {
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.items[sid]
	if !ok {
		return Session{}, false
	}
	if now.Unix() >= s.ExpiresAt {
		delete(m.items, sid)
		return Session{}, false
	}
	return s, true
}

// Delete 使单个会话立即失效（主动登出）。
func (m *MemorySessions) Delete(sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, sid)
}

// DeleteUser 使某个用户的全部会话失效。
func (m *MemorySessions) DeleteUser(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for sid, s := range m.items {
		if s.Identity.UserID == userID {
			delete(m.items, sid)
		}
	}
}

// List 返回会话快照，供 IdP 撤销检测定时扫描。
func (m *MemorySessions) List() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, 0, len(m.items))
	for _, s := range m.items {
		out = append(out, s)
	}
	return out
}

// MarkValidated 记录一次成功的 IdP 校验时间。
func (m *MemorySessions) MarkValidated(sid string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.items[sid]
	if !ok {
		return
	}
	s.LastValidated = at.UTC().Unix()
	m.items[sid] = s
}

// Cleanup 清理过期会话，返回清理数量。
func (m *MemorySessions) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := len(m.items)
	m.deleteExpiredLocked(m.now().UTC())
	return before - len(m.items)
}

func (m *MemorySessions) deleteExpiredLocked(now time.Time) {
	for sid, s := range m.items {
		if now.Unix() >= s.ExpiresAt {
			delete(m.items, sid)
		}
	}
}

// enforceLimitLocked 保留最多 keep 个会话，优先删除最早建立的。
func (m *MemorySessions) enforceLimitLocked(userID int64, keep int) error {
	var own []Session
	for _, s := range m.items {
		if s.Identity.UserID == userID {
			own = append(own, s)
		}
	}
	if len(own) <= keep {
		return nil
	}
	sort.Slice(own, func(i, j int) bool { return own[i].IssuedAt < own[j].IssuedAt })
	for i := 0; i < len(own)-keep; i++ {
		delete(m.items, own[i].ID)
	}
	return nil
}

func randomID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
