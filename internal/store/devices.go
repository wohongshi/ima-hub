package store

// devices.go — 免登录设备 token（单人模式「记住本机」）。
// 原始 token 只在签发时返回一次，落盘仅存 sha256；按设备吊销，改密全清。

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"
)

// DeviceToken 免登录设备凭证：持有原始 token 可免密码登录。
type DeviceToken struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	Secret    string `json:"secret"` // sha256(原始 token) 十六进制
	CreatedAt int64  `json:"created_at"`
	LastUsed  int64  `json:"last_used,omitempty"`
}

func deviceSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// DeviceCreate 签发免登录设备 token，返回 id 与原始 token（仅此一次可见）。
func (s *Store) DeviceCreate(userID, name string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.d.Devices == nil {
		s.d.Devices = map[string]DeviceToken{}
	}
	if name == "" {
		name = "未命名设备"
	}
	raw := RandomToken()
	id := "dev_" + RandomToken()[:12]
	s.d.Devices[id] = DeviceToken{
		ID: id, UserID: userID, Name: name,
		Secret: deviceSecret(raw), CreatedAt: time.Now().Unix(),
	}
	return id, raw, s.saveLocked()
}

// DeviceList 某用户的设备（含 Secret，对外输出前自行剥除）。
func (s *Store) DeviceList(userID string) []DeviceToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []DeviceToken{}
	for _, d := range s.d.Devices {
		if d.UserID == userID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// DeviceDelete 吊销设备（仅属主可删）。
func (s *Store) DeviceDelete(userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.d.Devices[id]
	if !ok || d.UserID != userID {
		return ErrNotFound
	}
	delete(s.d.Devices, id)
	return s.saveLocked()
}

// DeviceLogin 免登录：按原始 token 找设备、更新最后使用时间，返回属主用户。
func (s *Store) DeviceLogin(raw string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sec := deviceSecret(raw)
	foundID := ""
	var found DeviceToken
	for id, d := range s.d.Devices {
		if d.Secret == sec {
			foundID, found = id, d
			break
		}
	}
	if foundID == "" {
		return nil, ErrNotFound
	}
	var user *User
	for i := range s.d.Users {
		if s.d.Users[i].ID == found.UserID {
			user = &s.d.Users[i]
			break
		}
	}
	if user == nil {
		return nil, ErrNotFound
	}
	// 最后使用时间 best-effort 落盘，失败不挡登录
	found.LastUsed = time.Now().Unix()
	s.d.Devices[foundID] = found
	_ = s.saveLocked()
	return user, nil
}

// RevokeUserDevices 吊销某用户全部设备（改密时让旧设备失效）。
func (s *Store) RevokeUserDevices(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, d := range s.d.Devices {
		if d.UserID == userID {
			delete(s.d.Devices, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveLocked()
}
