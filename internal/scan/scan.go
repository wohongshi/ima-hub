// Package scan 知识库扫描引擎：遍历容量池全部账号/知识库，产出统计与快照。
package scan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"imahub/internal/ima"
	"imahub/internal/store"
)

const (
	maxFilesPerKB = 5000
	maxDepth      = 10
	maxNewFiles   = 50
	maxFirstSeen  = 100000
)

type AccountStat struct {
	ID      string         `json:"id"`
	UserID  string         `json:"user_id"`
	Name    string         `json:"name"`
	KBs     int            `json:"kbs"`
	Files   int            `json:"files"`
	Folders int            `json:"folders"`
	Types   map[string]int `json:"types,omitempty"` // 扩展名 -> 数量
	Error   string         `json:"error,omitempty"`
}

type TypeStat struct {
	Type  string  `json:"type"`
	Label string  `json:"label"`
	Count int     `json:"count"`
	Pct   float64 `json:"pct"`
}

type NewFile struct {
	MediaID     string `json:"media_id"`
	Title       string `json:"title"`
	AccountID   string `json:"ia"`
	AccountName string `json:"account_name"`
	KBName      string `json:"kb_name"`
	FirstSeen   int64  `json:"first_seen"`
	UserID      string `json:"user_id"`
}

type Snapshot struct {
	ScannedAt int64         `json:"scanned_at"`
	Accounts  []AccountStat `json:"accounts"`
	ByType    []TypeStat    `json:"by_type"`
	NewFiles  []NewFile     `json:"new_files"`
	Files     int           `json:"files"`
	Folders   int           `json:"folders"`
	KBs       int           `json:"kbs"`
}

type Status struct {
	Status     string `json:"status"` // idle | running | done | error
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	Error      string `json:"error,omitempty"`
	LastScanAt int64  `json:"last_scan_at"`
}

type snapshotFile struct {
	Snap      Snapshot         `json:"snap"`
	FirstSeen map[string]int64 `json:"first_seen"`
}

type Scanner struct {
	Store   *store.Store
	BaseURL string
	path    string

	mu        sync.Mutex
	running   bool
	done      int
	total     int
	status    string
	lastErr   string
	snap      Snapshot
	firstSeen map[string]int64
}

func Open(dataDir string, st *store.Store, baseURL string) *Scanner {
	s := &Scanner{
		Store:     st,
		BaseURL:   baseURL,
		path:      filepath.Join(dataDir, "scan.json"),
		status:    "idle",
		firstSeen: map[string]int64{},
	}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		var sf snapshotFile
		if json.Unmarshal(raw, &sf) == nil {
			s.snap = sf.Snap
			if sf.FirstSeen != nil {
				s.firstSeen = sf.FirstSeen
			}
		}
	}
	return s
}

func (s *Scanner) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Status:     s.status,
		Done:       s.done,
		Total:      s.total,
		Error:      s.lastErr,
		LastScanAt: s.snap.ScannedAt,
	}
}

func (s *Scanner) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap
}

// Start 启动一次全量扫描（单飞）；已在跑返回 false。
func (s *Scanner) Start() bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.status = "running"
	s.done = 0
	s.total = 0
	s.lastErr = ""
	s.mu.Unlock()
	go s.run()
	return true
}

// Stale 快照是否超过 maxAge。
func (s *Scanner) Stale(maxAge time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap.ScannedAt == 0 {
		return true
	}
	return time.Since(time.Unix(s.snap.ScannedAt, 0)) > maxAge
}

type scanAccount struct {
	ID     string
	UserID string
	Name   string
	Client *ima.Client
}

func (s *Scanner) run() {
	defer func() {
		s.mu.Lock()
		s.running = false
		if s.status == "running" {
			s.status = "done"
		}
		s.mu.Unlock()
	}()

	// 收集团队共享账号池（知识库数据全团队互通）；空时回落服务端默认凭证
	accs := []scanAccount{}
	for _, a := range s.Store.GetTeamAccounts() {
		accs = append(accs, scanAccount{
			ID: a.ID, Name: a.Name,
			Client: ima.New(a.ClientID, a.APIKey, s.BaseURL),
		})
	}
	if len(accs) == 0 {
		st := s.Store.GetSettings()
		if st.DefaultIMAClientID != "" && st.DefaultIMAAPIKey != "" {
			accs = append(accs, scanAccount{
				ID: "default", Name: "服务端默认账号",
				Client: ima.New(st.DefaultIMAClientID, st.DefaultIMAAPIKey, s.BaseURL),
			})
		}
	}

	s.mu.Lock()
	s.total = len(accs)
	s.mu.Unlock()

	now := time.Now().Unix()
	snap := Snapshot{ScannedAt: now}
	typeCount := map[string]int{}
	newFiles := []NewFile{}
	firstSeen := map[string]int64{}

	for _, acc := range accs {
		st := AccountStat{ID: acc.ID, UserID: acc.UserID, Name: acc.Name}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		kbs, err := acc.Client.SearchKnowledgeBase(ctx, "", "", 50)
		if err != nil {
			st.Error = err.Error()
			cancel()
			snap.Accounts = append(snap.Accounts, st)
			s.mu.Lock()
			s.done++
			s.mu.Unlock()
			continue
		}
		st.KBs = len(kbs.InfoList)
		accTypes := map[string]int{}
		for _, kb := range kbs.InfoList {
			files, folders := s.walkKB(ctx, acc.Client, kb.ID, kb.Name, acc, accTypes, &newFiles, firstSeen, now)
			st.Files += files
			st.Folders += folders
		}
		st.Types = accTypes
		for t, c := range accTypes {
			typeCount[t] += c
		}
		cancel()
		snap.Accounts = append(snap.Accounts, st)
		s.mu.Lock()
		s.done++
		s.mu.Unlock()
	}

	for _, a := range snap.Accounts {
		snap.KBs += a.KBs
		snap.Files += a.Files
		snap.Folders += a.Folders
	}
	total := snap.Files
	for t, c := range typeCount {
		pct := 0.0
		if total > 0 {
			pct = float64(c) / float64(total)
		}
		snap.ByType = append(snap.ByType, TypeStat{Type: t, Label: TypeLabel(t), Count: c, Pct: pct})
	}
	sortByType(snap.ByType)
	if len(newFiles) > maxNewFiles {
		newFiles = newFiles[:maxNewFiles]
	}
	snap.NewFiles = newFiles

	// 合并 firstSeen（保旧保新，限量）
	for k, v := range s.firstSeen {
		if _, ok := firstSeen[k]; !ok {
			firstSeen[k] = v
		}
	}
	if len(firstSeen) > maxFirstSeen {
		// 丢弃最旧的一半（近似：不排序，直接截断新表）
		trimmed := map[string]int64{}
		i := 0
		for k, v := range firstSeen {
			if i >= maxFirstSeen {
				break
			}
			trimmed[k] = v
			i++
		}
		firstSeen = trimmed
	}

	s.mu.Lock()
	s.snap = snap
	s.firstSeen = firstSeen
	s.status = "done"
	s.mu.Unlock()
	s.persist()
}

func (s *Scanner) persist() {
	s.mu.Lock()
	sf := snapshotFile{Snap: s.snap, FirstSeen: s.firstSeen}
	s.mu.Unlock()
	raw, err := json.Marshal(sf)
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// walkKB 递归遍历一个知识库，返回 (files, folders)。
func (s *Scanner) walkKB(ctx context.Context, c *ima.Client, kbID, kbName string, acc scanAccount,
	typeCount map[string]int, newFiles *[]NewFile, firstSeen map[string]int64, now int64) (int, int) {

	files, folders := 0, 0
	queue := []struct {
		folder string
		depth  int
	}{{folder: kbID, depth: 0}}
	count := 0

	for len(queue) > 0 && count < maxFilesPerKB {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth > maxDepth {
			continue
		}
		cursor := ""
		for page := 0; page < 100; page++ {
			rsp, err := c.GetKnowledgeList(ctx, kbID, cur.folder, cursor, 50)
			if err != nil {
				break
			}
			for _, f := range rsp.Folders {
				folders++
				queue = append(queue, struct {
					folder string
					depth  int
				}{folder: f.FolderID, depth: cur.depth + 1})
			}
			for _, f := range rsp.Files {
				files++
				count++
				ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(f.Title), "."))
				if ext == "" {
					ext = "other"
				}
				typeCount[ext]++
				_, known := firstSeen[f.MediaID]
				if !known {
					firstSeen[f.MediaID] = now
					if s.knownBefore(f.MediaID) {
						// 旧快照里有，只是 firstSeen 被裁剪过 —— 不算新增
					} else {
						*newFiles = append(*newFiles, NewFile{
							MediaID: f.MediaID, Title: f.Title,
							AccountID: acc.ID, AccountName: acc.Name,
							KBName: kbName, FirstSeen: now, UserID: acc.UserID,
						})
					}
				}
				if count >= maxFilesPerKB {
					break
				}
			}
			if rsp.IsEnd || rsp.NextCursor == "" {
				break
			}
			cursor = rsp.NextCursor
		}
	}
	return files, folders
}

func (s *Scanner) knownBefore(mediaID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.firstSeen[mediaID]
	return ok
}

// TypeLabel 扩展名 → 中文类型名。
func TypeLabel(t string) string {
	switch t {
	case "pdf":
		return "PDF"
	case "doc", "docx":
		return "Word"
	case "ppt", "pptx":
		return "PPT"
	case "xls", "xlsx", "csv":
		return "Excel"
	case "md", "markdown":
		return "Markdown"
	case "png", "jpg", "jpeg", "webp":
		return "图片"
	case "txt":
		return "文本"
	case "xmind":
		return "XMind"
	case "mp3", "m4a", "wav", "aac":
		return "音频"
	case "html", "htm":
		return "网页"
	case "epub":
		return "EPUB"
	case "other":
		return "其他"
	}
	return strings.ToUpper(t)
}

func sortByType(ts []TypeStat) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Count > ts[j-1].Count; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}
