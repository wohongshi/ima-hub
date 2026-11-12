package store

// lab.go — 教研工作台（/lab）资料收集：元数据进 data.json，文件体由 api 层落 data/lab/。

import (
	"sort"
	"time"
)

// LabFile 教研工作台收集的一份资料。
type LabFile struct {
	ID        string `json:"id"`
	Name      string `json:"name"` // 原始文件名（展示/下载用）
	Size      int64  `json:"size"`
	By        string `json:"by,omitempty"` // 上传者自报昵称（可空）
	CreatedAt int64  `json:"created_at"`
}

// LabAdd 记录一份资料。
func (s *Store) LabAdd(f LabFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.CreatedAt == 0 {
		f.CreatedAt = time.Now().Unix()
	}
	s.d.LabFiles = append(s.d.LabFiles, f)
	return s.saveLocked()
}

// LabList 资料清单（新→旧）。
func (s *Store) LabList() []LabFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LabFile, len(s.d.LabFiles))
	copy(out, s.d.LabFiles)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// LabDelete 删除记录（返回旧记录供调用方删文件体）。
func (s *Store) LabDelete(id string) (LabFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.d.LabFiles {
		if f.ID == id {
			s.d.LabFiles = append(s.d.LabFiles[:i], s.d.LabFiles[i+1:]...)
			return f, s.saveLocked()
		}
	}
	return LabFile{}, ErrNotFound
}
