package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"imahub/internal/store"
)

// queueDir 上传队列暂存目录（data/queue/）。
func (s *Server) queueDir() string {
	return filepath.Join(filepath.Dir(s.TmpDir), "queue")
}

// handleQueueUpload 入队：文件暂存本地，网络/ima 恢复后自动续传（离线上传）。
func (s *Server) handleQueueUpload(w http.ResponseWriter, r *http.Request, u *store.User) {
	const maxQueueBody = 550 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxQueueBody)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeBizErr(w, "multipart 解析失败: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	fhs := r.MultipartForm.File["file"]
	if len(fhs) == 0 {
		writeBizErr(w, "缺少 file")
		return
	}
	name := filepath.Base(fhs[0].Filename)
	if name == "" || name == "." || name == "/" {
		writeBizErr(w, "文件名无效")
		return
	}
	dir := s.queueDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	id := "q" + store.RandomToken()[:11]
	dst := filepath.Join(dir, id+"_"+name)
	src, err := fhs[0].Open()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	defer src.Close()
	f, err := os.Create(dst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	n, err := io.Copy(f, io.LimitReader(src, maxQueueBody))
	f.Close()
	if err != nil {
		os.Remove(dst)
		writeErr(w, http.StatusInternalServerError, 500, "暂存失败: "+err.Error())
		return
	}
	it := s.Store.AddQueueItem(store.UploadQueueItem{
		ID: id, UserID: u.ID, Name: name, Path: dst, Size: n,
		KBID: r.FormValue("kb_id"), FolderID: r.FormValue("folder_id"),
		IA: r.FormValue("ia"), Status: "pending",
	})
	writeOK(w, map[string]any{"item": it})
}

// handleQueueList 队列列表。
func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeOK(w, map[string]any{"items": s.Store.ListQueue()})
}

// handleQueueDelete 删除队列条目（含暂存文件）。
func (s *Server) handleQueueDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	for _, it := range s.Store.ListQueue() {
		if it.ID == id {
			_ = os.Remove(it.Path)
			break
		}
	}
	if err := s.Store.DeleteQueueItem(id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
		return
	}
	writeOK(w, nil)
}

// handleQueueRetry 单条立即重试。
func (s *Server) handleQueueRetry(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if err := s.Store.UpdateQueueItem(id, func(x *store.UploadQueueItem) error {
		x.Status = "pending"
		x.Error = ""
		return nil
	}); err != nil {
		writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
		return
	}
	ok := s.drainOne(id)
	writeOK(w, map[string]any{"ok": ok, "items": s.Store.ListQueue()})
}

// handleQueueDrain 立即续传全部待发。
func (s *Server) handleQueueDrain(w http.ResponseWriter, r *http.Request, u *store.User) {
	n := s.drainQueue()
	writeOK(w, map[string]any{"sent": n, "items": s.Store.ListQueue()})
}

// drainQueue 顺序续传 pending 条目（单飞，防带宽互抢）。返回成功条数。
func (s *Server) drainQueue() int {
	sent := 0
	for _, it := range s.Store.ListQueue() {
		if it.Status == "pending" && s.drainOne(it.ID) {
			sent++
		}
	}
	return sent
}

// drainOne 上传单条：成功出队并记活动；失败标记 failed 供面板展示/重试。
func (s *Server) drainOne(id string) bool {
	var item store.UploadQueueItem
	for _, it := range s.Store.ListQueue() {
		if it.ID == id {
			item = it
			break
		}
	}
	if item.ID == "" {
		return false
	}
	u, ok := s.Store.FindUserByID(item.UserID)
	if !ok {
		s.failQueueItem(id, fmt.Errorf("归属用户已不存在"))
		return false
	}
	acc, err := s.resolveImaAccount(u, item.IA)
	if err != nil {
		s.failQueueItem(id, err)
		return false
	}
	kbID := item.KBID
	if kbID == "" {
		kbID = acc.UploadKBID
	}
	if kbID == "" {
		s.failQueueItem(id, fmt.Errorf("未指定目标知识库"))
		return false
	}
	f, err := os.Open(item.Path)
	if err != nil {
		s.failQueueItem(id, err)
		return false
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	mediaID, _, err := s.clientFor(acc).UploadFile(ctx, kbID, item.FolderID, item.Name, item.Size, f)
	if err != nil {
		s.failQueueItem(id, err)
		return false
	}
	_ = mediaID
	s.Store.BumpUpload(u.ID, item.Size)
	s.Store.AddActivity(store.Activity{
		Kind: "upload", Title: item.Name, IA: acc.ID, AccountName: acc.Name,
		KBName: kbID, Size: item.Size, UserID: u.ID, By: u.Username,
	})
	_ = os.Remove(item.Path)
	_ = s.Store.DeleteQueueItem(id)
	return true
}

func (s *Server) failQueueItem(id string, err error) {
	_ = s.Store.UpdateQueueItem(id, func(x *store.UploadQueueItem) error {
		x.Status = "failed"
		x.Error = err.Error()
		x.Attempts++
		return nil
	})
}

// RunQueueDrainer 队列自动续传：每 60 秒清一次 pending（网络恢复后静默补传）。
func (s *Server) RunQueueDrainer(stop <-chan struct{}) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if n := s.drainQueue(); n > 0 {
				log.Printf("上传队列续传 %d 个文件", n)
			}
		}
	}
}
