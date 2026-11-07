package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"imahub/internal/ima"
	"imahub/internal/store"
)

// 队列配额：条数与总量上限（超出拒绝入队，防止占满磁盘）
const (
	queueMaxItems = 20
	queueMaxTotal = 2 << 30
)

// drainMu 单飞锁：定时器与手动续传/重试共享，防同一条目并发双传。
var drainMu sync.Mutex

// queueDir 上传队列暂存目录（data/queue/）。
func (s *Server) queueDir() string {
	return filepath.Join(filepath.Dir(s.TmpDir), "queue")
}

// queueView 队列视图（归属隔离：普通用户只看自己的；响应剥掉 Path/UserID）。
func (s *Server) queueView(u *store.User) []store.UploadQueueItem {
	all := s.Store.ListQueue()
	out := make([]store.UploadQueueItem, 0, len(all))
	for _, it := range all {
		if u.Role != "admin" && it.UserID != u.ID {
			continue
		}
		it.Path = ""   // 服务端绝对路径不外发
		it.UserID = "" // 归属细节不外发
		out = append(out, it)
	}
	return out
}

// queueOwned 找到条目并校验归属（admin 可操作全部）。
func (s *Server) queueOwned(id string, u *store.User) (store.UploadQueueItem, bool) {
	for _, it := range s.Store.ListQueue() {
		if it.ID == id {
			if u.Role != "admin" && it.UserID != u.ID {
				return store.UploadQueueItem{}, false
			}
			return it, true
		}
	}
	return store.UploadQueueItem{}, false
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
	// 类型/大小校验与直传同规则（压缩包不支持排队：续传走单文件通道）
	mt := ima.DetectMediaType(name)
	if mt < 0 || ima.IsArchive(name) {
		writeBizErr(w, "不支持排队的文件类型："+filepath.Ext(name))
		return
	}
	if fhs[0].Size > ima.MaxUploadSize(mt) {
		writeBizErr(w, fmt.Sprintf("文件超过大小上限（%dMB）", ima.MaxUploadSize(mt)>>20))
		return
	}
	// 配额：条数与总量
	items := s.Store.ListQueue()
	var total int64
	for _, it := range items {
		total += it.Size
	}
	if len(items) >= queueMaxItems {
		writeBizErr(w, fmt.Sprintf("队列已满（%d 个），请先续传或删除", queueMaxItems))
		return
	}
	if total+int64(fhs[0].Size) > queueMaxTotal {
		writeBizErr(w, "队列总量超过上限（2GB）")
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
	cerr := f.Close()
	if err == nil {
		err = cerr // 落盘刷写失败同样算暂存失败，不能报成功
	}
	if err != nil {
		os.Remove(dst)
		writeErr(w, http.StatusInternalServerError, 500, "暂存失败: "+err.Error())
		return
	}
	it, err := s.Store.AddQueueItem(store.UploadQueueItem{
		ID: id, UserID: u.ID, Name: name, Path: dst, Size: n,
		KBID: r.FormValue("kb_id"), FolderID: r.FormValue("folder_id"),
		IA: r.FormValue("ia"), Status: "pending",
	})
	if err != nil {
		os.Remove(dst) // 元数据没落盘，文件不能留成孤儿
		writeErr(w, http.StatusInternalServerError, 500, "入队落盘失败: "+err.Error())
		return
	}
	it.Path = ""
	it.UserID = ""
	writeOK(w, map[string]any{"item": it})
}

// handleQueueList 队列列表（归属隔离，不含服务端路径）。
func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeOK(w, map[string]any{"items": s.queueView(u)})
}

// handleQueueDelete 删除队列条目（含暂存文件）。
func (s *Server) handleQueueDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	it, ok := s.queueOwned(id, u)
	if !ok {
		writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
		return
	}
	_ = os.Remove(it.Path)
	if err := s.Store.DeleteQueueItem(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeOK(w, nil)
}

// handleQueueRetry 单条立即重试（异步执行，结果看列表刷新）。
func (s *Server) handleQueueRetry(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if _, ok := s.queueOwned(id, u); !ok {
		writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
		return
	}
	if err := s.Store.UpdateQueueItem(id, func(x *store.UploadQueueItem) error {
		x.Status = "pending"
		x.Error = ""
		return nil
	}); err != nil {
		writeErr(w, http.StatusNotFound, 404, "队列条目不存在")
		return
	}
	go s.drainOne(id)
	writeOK(w, map[string]any{"started": true, "items": s.queueView(u)})
}

// handleQueueDrain 立即续传全部待发（异步，立即返回）。
func (s *Server) handleQueueDrain(w http.ResponseWriter, r *http.Request, u *store.User) {
	go s.drainQueue()
	writeOK(w, map[string]any{"started": true, "items": s.queueView(u)})
}

// drainQueue 顺序续传 pending/failed 条目（单飞锁；失败条目持续自动重试）。
// 返回本次成功条数。
func (s *Server) drainQueue() int {
	drainMu.Lock()
	defer drainMu.Unlock()
	sent := 0
	for _, it := range s.Store.ListQueue() {
		if (it.Status == "pending" || it.Status == "failed") && s.drainOneLocked(it.ID) {
			sent++
		}
	}
	return sent
}

// drainOne 供单条重试入口（同样走单飞锁）。
func (s *Server) drainOne(id string) bool {
	drainMu.Lock()
	defer drainMu.Unlock()
	return s.drainOneLocked(id)
}

// drainOneLocked 上传单条：成功出队并记活动；失败保留条目与错误，下轮继续重试。
func (s *Server) drainOneLocked(id string) bool {
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
	acc, err := s.pickQueueAccount(u, item)
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

// pickQueueAccount 选目标账号：显式 ia 直接解析；未指定时与直传 auto 语义一致——
// 跳过没有默认上传知识库的账号，避免「未指定目标知识库」死局。
func (s *Server) pickQueueAccount(u *store.User, item store.UploadQueueItem) (*store.IMAAccount, error) {
	if item.IA != "" {
		return s.resolveImaAccount(u, item.IA)
	}
	pool := s.poolAccounts(u)
	for i := range pool {
		if item.KBID != "" || pool[i].UploadKBID != "" {
			a := pool[i]
			return &a, nil
		}
	}
	return nil, errors.New("没有配置默认上传知识库的账号（设置页配置，或入队时指定目标）")
}

func (s *Server) failQueueItem(id string, err error) {
	_ = s.Store.UpdateQueueItem(id, func(x *store.UploadQueueItem) error {
		x.Status = "failed"
		x.Error = err.Error()
		x.Attempts++
		return nil
	})
}

// RunQueueDrainer 队列自动续传：每 60 秒清一次（含 failed，网络恢复后静默补传）。
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
