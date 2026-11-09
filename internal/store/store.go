// Package store 持久化用户、会话与服务设置（JSON 文件，原子写入）。
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// IMAAccount 一个 ima 开放平台账号（成员自有或团队公共，聚合为容量池）。
type IMAAccount struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Owner      string `json:"owner,omitempty"` // 归属成员用户名（成员自有/缓存账号用）
	ClientID   string `json:"client_id"`
	APIKey     string `json:"api_key"`
	UploadKBID string `json:"upload_kb_id,omitempty"` // 自动分配上传时的默认目标知识库
	CreatedAt  int64  `json:"created_at"`
}

type User struct {
	ID          string       `json:"id"`
	Username    string       `json:"username"`
	PassHash    string       `json:"pass_hash"`
	Role        string       `json:"role"` // admin | user
	Disabled    bool         `json:"disabled"`
	CreatedAt   int64        `json:"created_at"`
	IMAAccounts []IMAAccount `json:"ima_accounts,omitempty"`
	MCPToken    string       `json:"mcp_token,omitempty"`

	// 累计上传统计（经 ImaHub 上传）
	Uploads int64 `json:"uploads,omitempty"`
	Bytes   int64 `json:"bytes,omitempty"`

	// 旧版单账号字段，仅用于迁移，勿直接使用。
	LegacyIMAClientID string `json:"ima_client_id,omitempty"`
	LegacyIMAAPIKey   string `json:"ima_api_key,omitempty"`
}

// Activity 近期添加记录（upload/import/scan_new/note）。
type Activity struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // upload | import | note
	Title       string `json:"title"`
	IA          string `json:"ia"`
	AccountName string `json:"account_name"`
	KBName      string `json:"kb_name"`
	MediaID     string `json:"media_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	At          int64  `json:"at"`
	UserID      string `json:"user_id"`
	By          string `json:"by"` // 操作人用户名（团队共享展示）
}

// Task 首页任务计划条目（团队共享，全员可见可操作）。
type Task struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"` // 创建人 id（兼容保留）
	By        string `json:"by"`      // 创建人用户名
	Title     string `json:"title"`
	Note      string `json:"note"`
	Status    string `json:"status"`             // todo | doing | done
	Due       any    `json:"due"`                // 兼容日期字符串（2026-11-07）/Unix 秒/""（无）
	GroupID   string `json:"group_id,omitempty"` // 群组共享（空=本地/团队）
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Event 日程事件（团队共享，全员可增删改）。
type Event struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"` // 创建人 id
	By        string `json:"by"`      // 创建人用户名
	Title     string `json:"title"`
	StartAt   int64  `json:"start_at"` // Unix 秒
	EndAt     int64  `json:"end_at"`   // 0 = 无结束时间
	AllDay    bool   `json:"all_day"`
	Location  string `json:"location"`
	Note      string `json:"note"`
	GroupID   string `json:"group_id,omitempty"` // 群组共享（空=本地/团队）
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Announcement 团队公告（管理员发布，全员可见）。
type Announcement struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	By        string `json:"by"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// UploadQueueItem 离线/失败上传队列条目（文件存 data/queue/，恢复后自动续传）。
type UploadQueueItem struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id,omitempty"` // 归属用户（续传记账用；对外视图剥掉）
	Name      string `json:"name"`              // 上传文件名
	Path      string `json:"path,omitempty"`    // 队列目录内文件绝对路径（绝不外发）
	Size      int64  `json:"size"`
	KBID      string `json:"kb_id"`
	FolderID  string `json:"folder_id,omitempty"`
	IA        string `json:"ia,omitempty"` // 目标账号（空=自动分配）
	Status    string `json:"status"`       // pending | failed
	Error     string `json:"error,omitempty"`
	Attempts  int    `json:"attempts"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type Session struct {
	UserID    string `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type Settings struct {
	SiteName           string `json:"site_name"`
	AllowRegister      bool   `json:"allow_register"`
	DefaultIMAClientID string `json:"default_ima_client_id"`
	DefaultIMAAPIKey   string `json:"default_ima_api_key"`
	// 知识库问答（agent 查询）内置模型：OpenAI 兼容端点，管理员配置
	AskBaseURL string `json:"ask_base_url"`
	AskAPIKey  string `json:"ask_api_key"`
	AskModel   string `json:"ask_model"`
	// 客户端模式（client）本地状态：服务端地址、会话、上次同步时间
	ClientServerURL    string `json:"client_server_url,omitempty"`
	ClientSession      string `json:"client_session,omitempty"`
	ClientSessionUser  string `json:"client_session_user,omitempty"`
	ClientLastSync     int64  `json:"client_last_sync,omitempty"`
	ClientActivitySync int64  `json:"client_activity_sync,omitempty"` // 活动流水增量游标（已推送的最大 At）
	// 公网发现 + 中继（imahub discover 服务地址）；PublicAddr 为可选对外地址（公网映射后手填）
	DiscoverURL string `json:"discover_url,omitempty"`
	PublicAddr  string `json:"public_addr,omitempty"`
}

type data struct {
	Users    []User             `json:"users"`
	Settings Settings           `json:"settings"`
	Sessions map[string]Session `json:"sessions"`
	NextID   int                `json:"next_id"`
	// TeamAccounts 团队共享 ima 账号池（知识库数据全团队互通）。
	TeamAccounts  []IMAAccount   `json:"team_accounts,omitempty"`
	Activities    []Activity     `json:"activities,omitempty"`
	Tasks         []Task         `json:"tasks,omitempty"`
	Events        []Event        `json:"events,omitempty"`
	Announcements []Announcement `json:"announcements,omitempty"`
	// Tombstones 任务/日程删除墓碑（"task:<id>"/"event:<id>" → 删除时间），离线同步 LWW 用。
	Tombstones map[string]int64 `json:"tombstones,omitempty"`
	// UploadQueue 离线/失败上传队列（文件在 data/queue/）。
	UploadQueue []UploadQueueItem `json:"upload_queue,omitempty"`
	// AccessRequests P2P 访问申请制共享（按 ID 收敛）。
	AccessRequests map[string]AccessRequest `json:"access_requests,omitempty"`
	// Groups 群组：对等节点任务/日程共享圈（按 ID LWW 收敛）。
	Groups map[string]Group `json:"groups,omitempty"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	d    data
}

var ErrNotFound = errors.New("not found")

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "data.json")}
	s.d.Sessions = map[string]Session{}
	s.d.AccessRequests = map[string]AccessRequest{}
	s.d.NextID = 1
	s.d.Settings = Settings{SiteName: "ImaHub", AllowRegister: false}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(raw, &s.d); err != nil {
			return nil, err
		}
		if s.d.Sessions == nil {
			s.d.Sessions = map[string]Session{}
		}
		if s.d.AccessRequests == nil {
			s.d.AccessRequests = map[string]AccessRequest{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// 清理过期会话
	now := time.Now().Unix()
	for k, v := range s.d.Sessions {
		if v.ExpiresAt < now {
			delete(s.d.Sessions, k)
		}
	}
	// 旧版单账号 → 多账号迁移
	migrated := false
	for i := range s.d.Users {
		u := &s.d.Users[i]
		if len(u.IMAAccounts) == 0 && u.LegacyIMAClientID != "" {
			u.IMAAccounts = []IMAAccount{{
				ID:        RandomToken()[:12],
				Name:      "默认账号",
				ClientID:  u.LegacyIMAClientID,
				APIKey:    u.LegacyIMAAPIKey,
				CreatedAt: time.Now().Unix(),
			}}
			u.LegacyIMAClientID = ""
			u.LegacyIMAAPIKey = ""
			migrated = true
		}
	}
	// 团队化迁移：各用户的 ima 账号并入团队共享账号池（知识库数据全团队互通）
	for i := range s.d.Users {
		u := &s.d.Users[i]
		for _, a := range u.IMAAccounts {
			dup := false
			for _, t := range s.d.TeamAccounts {
				if t.ClientID == a.ClientID && t.APIKey == a.APIKey {
					dup = true
					break
				}
			}
			if !dup {
				a.ID = "t" + RandomToken()[:11]
				s.d.TeamAccounts = append(s.d.TeamAccounts, a)
			}
		}
		if len(u.IMAAccounts) > 0 {
			u.IMAAccounts = nil
			migrated = true
		}
	}
	if migrated {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(&s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Save 供外部在批量修改后调用；内部修改请用各方法。
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// --- users ---

func (s *Store) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.d.Users)
}

func (s *Store) FindUserByName(name string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.d.Users {
		if s.d.Users[i].Username == name {
			u := s.d.Users[i]
			return &u, true
		}
	}
	return nil, false
}

func (s *Store) FindUserByID(id string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.d.Users {
		if s.d.Users[i].ID == id {
			u := s.d.Users[i]
			return &u, true
		}
	}
	return nil, false
}

func (s *Store) FindUserByMCPToken(tok string) (*User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.d.Users {
		if tok != "" && s.d.Users[i].MCPToken == tok {
			u := s.d.Users[i]
			return &u, true
		}
	}
	return nil, false
}

func (s *Store) ListUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, len(s.d.Users))
	copy(out, s.d.Users)
	return out
}

func (s *Store) AddUser(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].Username == u.Username {
			return errors.New("用户名已存在")
		}
	}
	u.ID = itoaID(s.d.NextID)
	s.d.NextID++
	if u.CreatedAt == 0 {
		u.CreatedAt = time.Now().Unix()
	}
	s.d.Users = append(s.d.Users, *u)
	return s.saveLocked()
}

// UpdateUser 按 ID 更新用户，fn 内修改副本。
func (s *Store) UpdateUser(id string, fn func(*User) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].ID == id {
			cp := s.d.Users[i]
			if err := fn(&cp); err != nil {
				return err
			}
			s.d.Users[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].ID == id {
			s.d.Users = append(s.d.Users[:i], s.d.Users[i+1:]...)
			for k, v := range s.d.Sessions {
				if v.UserID == id {
					delete(s.d.Sessions, k)
				}
			}
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// --- sessions ---

const sessionTTL = 30 * 24 * time.Hour

func (s *Store) CreateSession(userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := randomToken()
	now := time.Now()
	// 顺手清理过期会话，防长期运行会话表只增不减
	for k, v := range s.d.Sessions {
		if v.ExpiresAt < now.Unix() {
			delete(s.d.Sessions, k)
		}
	}
	s.d.Sessions[tok] = Session{UserID: userID, CreatedAt: now.Unix(), ExpiresAt: now.Add(sessionTTL).Unix()}
	return tok, s.saveLocked()
}

func (s *Store) GetSession(tok string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.d.Sessions[tok]
	if !ok || sess.ExpiresAt < time.Now().Unix() {
		return nil, false
	}
	return &sess, true
}

func (s *Store) DeleteSession(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Sessions, tok)
	return s.saveLocked()
}

// RevokeUserSessionsExcept 吊销某用户除 keepTok 外的全部会话（改密后让旧会话失效）。
func (s *Store) RevokeUserSessionsExcept(userID, keepTok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.d.Sessions {
		if v.UserID == userID && k != keepTok {
			delete(s.d.Sessions, k)
		}
	}
	return s.saveLocked()
}

// SetupAdmin 首次初始化：仅当无任何用户时创建首个用户（并发下防双管理员）。
func (s *Store) SetupAdmin(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.d.Users) > 0 {
		return errors.New("已完成初始化")
	}
	for i := range s.d.Users {
		if s.d.Users[i].Username == u.Username {
			return errors.New("用户名已存在")
		}
	}
	u.ID = itoaID(s.d.NextID)
	s.d.NextID++
	if u.CreatedAt == 0 {
		u.CreatedAt = time.Now().Unix()
	}
	s.d.Users = append(s.d.Users, *u)
	return s.saveLocked()
}

// --- settings ---

func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.d.Settings
}

func (s *Store) UpdateSettings(fn func(*Settings) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.d.Settings
	if err := fn(&cp); err != nil {
		return err
	}
	s.d.Settings = cp
	return s.saveLocked()
}

// --- activities ---

const activityCap = 500

func (s *Store) AddActivity(a Activity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.ID == "" {
		a.ID = RandomToken()[:12]
	}
	if a.At == 0 {
		a.At = time.Now().Unix()
	}
	for _, x := range s.d.Activities {
		if x.ID == a.ID {
			return // 幂等：离线同步重放不重复计
		}
	}
	s.d.Activities = append(s.d.Activities, a)
	if len(s.d.Activities) > activityCap {
		s.d.Activities = s.d.Activities[len(s.d.Activities)-activityCap:]
	}
	_ = s.saveLocked()
}

// ListActivities 返回团队最近活动（新的在前，全员可见）。
func (s *Store) ListActivities(limit int) []Activity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Activity{}
	for i := len(s.d.Activities) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.d.Activities[i])
	}
	return out
}

// BumpUpload 累计上传计数。
func (s *Store) BumpUpload(userID string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Users {
		if s.d.Users[i].ID == userID {
			s.d.Users[i].Uploads++
			s.d.Users[i].Bytes += bytes
			_ = s.saveLocked()
			return
		}
	}
}

// --- tasks ---

// ListTasks 团队共享任务（全员可见）。
func (s *Store) ListTasks() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Task{}
	out = append(out, s.d.Tasks...)
	return out
}

func (s *Store) AddTask(t Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = RandomToken()[:12]
	now := time.Now().Unix()
	if t.CreatedAt == 0 {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	s.d.Tasks = append(s.d.Tasks, t)
	return t, s.saveLocked()
}

// UpdateTask 团队共享：任何成员可更新任意任务。
func (s *Store) UpdateTask(id string, fn func(*Task) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Tasks {
		if s.d.Tasks[i].ID == id {
			cp := s.d.Tasks[i]
			if err := fn(&cp); err != nil {
				return err
			}
			cp.UpdatedAt = BumpTS(cp.UpdatedAt, time.Now().Unix())
			s.d.Tasks[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// DeleteTask 团队共享：任何成员可删除任意任务。
func (s *Store) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Tasks {
		if s.d.Tasks[i].ID == id {
			s.d.Tasks = append(s.d.Tasks[:i], s.d.Tasks[i+1:]...)
			s.setTombstoneLocked("task:"+id, time.Now().Unix())
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// BumpTS 本地变更时间戳单调递增：同秒连改不被 LWW 严格小于吞掉。
func BumpTS(cur, now int64) int64 {
	if now <= cur {
		return cur + 1
	}
	return now
}

// --- 离线同步（客户端 LWW 合并） ---

func (s *Store) setTombstoneLocked(key string, ts int64) {
	if s.d.Tombstones == nil {
		s.d.Tombstones = map[string]int64{}
	}
	if ts > s.d.Tombstones[key] {
		s.d.Tombstones[key] = ts
	}
}

// TombstonesCopy 返回删除墓碑副本。
func (s *Store) TombstonesCopy() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int64, len(s.d.Tombstones))
	for k, v := range s.d.Tombstones {
		out[k] = v
	}
	return out
}

// SyncMerge 批量 LWW 合并（客户端离线改动回传）：UpdatedAt 决胜，删除墓碑压过更旧的更新。
// 返回 (采纳任务数, 采纳日程数)。
func (s *Store) SyncMerge(tasks []Task, events []Event, tombs map[string]int64) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, ts := range tombs {
		s.setTombstoneLocked(k, ts)
	}
	// 墓碑清理：本地副本比删除旧的直接删掉（离线端删除同步进来时生效）
	for i := 0; i < len(s.d.Tasks); {
		if ts, ok := s.d.Tombstones["task:"+s.d.Tasks[i].ID]; ok && ts >= s.d.Tasks[i].UpdatedAt {
			s.d.Tasks = append(s.d.Tasks[:i], s.d.Tasks[i+1:]...)
			continue
		}
		i++
	}
	for i := 0; i < len(s.d.Events); {
		if ts, ok := s.d.Tombstones["event:"+s.d.Events[i].ID]; ok && ts >= s.d.Events[i].UpdatedAt {
			s.d.Events = append(s.d.Events[:i], s.d.Events[i+1:]...)
			continue
		}
		i++
	}
	// 公告删除同步（服务端删除的公告经墓碑下发清理）
	for i := 0; i < len(s.d.Announcements); {
		if ts, ok := s.d.Tombstones["ann:"+s.d.Announcements[i].ID]; ok && ts >= s.d.Announcements[i].UpdatedAt {
			s.d.Announcements = append(s.d.Announcements[:i], s.d.Announcements[i+1:]...)
			continue
		}
		i++
	}
	adoptT := 0
	for _, t := range tasks {
		if t.ID == "" {
			t.ID = RandomToken()[:12]
		}
		if ts, ok := s.d.Tombstones["task:"+t.ID]; ok && ts >= t.UpdatedAt {
			continue
		}
		found := false
		for i := range s.d.Tasks {
			if s.d.Tasks[i].ID == t.ID {
				found = true
				if s.d.Tasks[i].UpdatedAt < t.UpdatedAt {
					s.d.Tasks[i] = t
					adoptT++
				}
				break
			}
		}
		if !found {
			s.d.Tasks = append(s.d.Tasks, t)
			adoptT++
		}
	}
	adoptE := 0
	for _, e := range events {
		if e.ID == "" {
			e.ID = RandomToken()[:12]
		}
		if ts, ok := s.d.Tombstones["event:"+e.ID]; ok && ts >= e.UpdatedAt {
			continue
		}
		found := false
		for i := range s.d.Events {
			if s.d.Events[i].ID == e.ID {
				found = true
				if s.d.Events[i].UpdatedAt < e.UpdatedAt {
					s.d.Events[i] = e
					adoptE++
				}
				break
			}
		}
		if !found {
			s.d.Events = append(s.d.Events, e)
			adoptE++
		}
	}
	_ = s.saveLocked()
	return adoptT, adoptE
}

// ReplaceTasks 整表替换任务（客户端同步拉取后以合并结果为准）。
func (s *Store) ReplaceTasks(in []Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Tasks = append([]Task(nil), in...)
	_ = s.saveLocked()
}

// ReplaceEvents 整表替换日程。
func (s *Store) ReplaceEvents(in []Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Events = append([]Event(nil), in...)
	_ = s.saveLocked()
}

// ReplaceAnnouncements 整表替换公告（客户端缓存）。
func (s *Store) ReplaceAnnouncements(in []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Announcements = append([]Announcement(nil), in...)
	_ = s.saveLocked()
}

// ReplaceTeamAccounts 整表替换团队账号缓存（客户端同步拉取的分发 key）。
func (s *Store) ReplaceTeamAccounts(in []IMAAccount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.TeamAccounts = append([]IMAAccount(nil), in...)
	_ = s.saveLocked()
}

// OwnedAccount 账号 + 归属（聚合视图 / 密钥分发用）。
type OwnedAccount struct {
	IMAAccount
	Owner   string `json:"owner"`
	OwnerID string `json:"owner_id"`
	Source  string `json:"source"` // user | team | default
}

// AllIMAAccounts 全部可用账号：成员自有 + 团队公共池 + 服务端默认。
func (s *Store) AllIMAAccounts() []OwnedAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []OwnedAccount{}
	for i := range s.d.Users {
		u := &s.d.Users[i]
		for j := range u.IMAAccounts {
			a := u.IMAAccounts[j]
			a.Owner = u.Username
			out = append(out, OwnedAccount{IMAAccount: a, Owner: u.Username, OwnerID: u.ID, Source: "user"})
		}
	}
	for j := range s.d.TeamAccounts {
		a := s.d.TeamAccounts[j]
		if a.Owner == "" {
			a.Owner = "团队公共"
		}
		out = append(out, OwnedAccount{IMAAccount: a, Owner: a.Owner, Source: "team"})
	}
	if s.d.Settings.DefaultIMAClientID != "" && s.d.Settings.DefaultIMAAPIKey != "" {
		out = append(out, OwnedAccount{
			IMAAccount: IMAAccount{ID: "default", Name: "服务端默认账号",
				ClientID: s.d.Settings.DefaultIMAClientID, APIKey: s.d.Settings.DefaultIMAAPIKey},
			Owner: "服务端默认", Source: "default",
		})
	}
	return out
}

// --- 上传队列 ---

// AddQueueItem 入队（调用方负责把文件放进队列目录）。落盘失败必须让调用方知道。
func (s *Store) AddQueueItem(it UploadQueueItem) (UploadQueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if it.ID == "" {
		it.ID = "q" + RandomToken()[:11]
	}
	now := time.Now().Unix()
	if it.CreatedAt == 0 {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	s.d.UploadQueue = append(s.d.UploadQueue, it)
	return it, s.saveLocked()
}

// ListQueue 队列快照（含全部状态）。
func (s *Store) ListQueue() []UploadQueueItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UploadQueueItem, len(s.d.UploadQueue))
	copy(out, s.d.UploadQueue)
	return out
}

// UpdateQueueItem 更新队列条目（副本提交）。
func (s *Store) UpdateQueueItem(id string, fn func(*UploadQueueItem) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.UploadQueue {
		if s.d.UploadQueue[i].ID == id {
			cp := s.d.UploadQueue[i]
			if err := fn(&cp); err != nil {
				return err
			}
			cp.UpdatedAt = BumpTS(cp.UpdatedAt, time.Now().Unix())
			s.d.UploadQueue[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// DeleteQueueItem 出队（调用方负责删文件）。
func (s *Store) DeleteQueueItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.UploadQueue {
		if s.d.UploadQueue[i].ID == id {
			s.d.UploadQueue = append(s.d.UploadQueue[:i], s.d.UploadQueue[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// MergeAnnouncements 公告按 ID 合并（只进不退，取较新）。
func (s *Store) MergeAnnouncements(in []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, a := range in {
		found := false
		for i := range s.d.Announcements {
			if s.d.Announcements[i].ID == a.ID {
				found = true
				if s.d.Announcements[i].UpdatedAt < a.UpdatedAt {
					s.d.Announcements[i] = a
					changed = true
				}
				break
			}
		}
		if !found {
			s.d.Announcements = append(s.d.Announcements, a)
			changed = true
		}
	}
	if changed {
		_ = s.saveLocked()
	}
}

// --- 团队共享账号池 ---

// GetTeamAccounts 团队共享 ima 账号池。
func (s *Store) GetTeamAccounts() []IMAAccount {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]IMAAccount, len(s.d.TeamAccounts))
	copy(out, s.d.TeamAccounts)
	return out
}

func (s *Store) AddTeamAccount(a IMAAccount) (IMAAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.d.TeamAccounts {
		if t.Name == a.Name {
			return IMAAccount{}, errors.New("已有同名 ima 账号：" + a.Name)
		}
	}
	if a.ID == "" {
		a.ID = "t" + RandomToken()[:11]
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = time.Now().Unix()
	}
	s.d.TeamAccounts = append(s.d.TeamAccounts, a)
	return a, s.saveLocked()
}

func (s *Store) UpdateTeamAccount(id string, fn func(*IMAAccount) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.TeamAccounts {
		if s.d.TeamAccounts[i].ID == id {
			cp := s.d.TeamAccounts[i]
			if err := fn(&cp); err != nil {
				return err
			}
			for j, t := range s.d.TeamAccounts {
				if j != i && t.Name == cp.Name {
					return errors.New("已有同名 ima 账号：" + cp.Name)
				}
			}
			s.d.TeamAccounts[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteTeamAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.TeamAccounts {
		if s.d.TeamAccounts[i].ID == id {
			s.d.TeamAccounts = append(s.d.TeamAccounts[:i], s.d.TeamAccounts[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// --- 日程事件（团队共享） ---

// ListEvents 团队日程（按开始时间升序）。
func (s *Store) ListEvents() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Event{}
	out = append(out, s.d.Events...)
	sort.Slice(out, func(i, j int) bool { return out[i].StartAt < out[j].StartAt })
	return out
}

func (s *Store) AddEvent(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = RandomToken()[:12]
	now := time.Now().Unix()
	if e.CreatedAt == 0 {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	s.d.Events = append(s.d.Events, e)
	return e, s.saveLocked()
}

// UpdateEvent 团队共享：任何成员可更新任意日程。
func (s *Store) UpdateEvent(id string, fn func(*Event) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Events {
		if s.d.Events[i].ID == id {
			cp := s.d.Events[i]
			if err := fn(&cp); err != nil {
				return err
			}
			cp.UpdatedAt = BumpTS(cp.UpdatedAt, time.Now().Unix())
			s.d.Events[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// DeleteEvent 团队共享：任何成员可删除任意日程。
func (s *Store) DeleteEvent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Events {
		if s.d.Events[i].ID == id {
			s.d.Events = append(s.d.Events[:i], s.d.Events[i+1:]...)
			s.setTombstoneLocked("event:"+id, time.Now().Unix())
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// --- 团队公告 ---

// ListAnnouncements 最新公告在前。
func (s *Store) ListAnnouncements(limit int) []Announcement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Announcement{}
	for i := len(s.d.Announcements) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.d.Announcements[i])
	}
	return out
}

func (s *Store) AddAnnouncement(a Announcement) (Announcement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = RandomToken()[:12]
	now := time.Now().Unix()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	s.d.Announcements = append(s.d.Announcements, a)
	return a, s.saveLocked()
}

func (s *Store) UpdateAnnouncement(id string, fn func(*Announcement) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Announcements {
		if s.d.Announcements[i].ID == id {
			cp := s.d.Announcements[i]
			if err := fn(&cp); err != nil {
				return err
			}
			cp.UpdatedAt = BumpTS(cp.UpdatedAt, time.Now().Unix())
			s.d.Announcements[i] = cp
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) DeleteAnnouncement(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.d.Announcements {
		if s.d.Announcements[i].ID == id {
			s.d.Announcements = append(s.d.Announcements[:i], s.d.Announcements[i+1:]...)
			s.setTombstoneLocked("ann:"+id, time.Now().Unix())
			return s.saveLocked()
		}
	}
	return ErrNotFound
}
