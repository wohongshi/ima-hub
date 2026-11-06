package api

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"imahub/internal/ima"
	"imahub/internal/store"
)

// handleDownloadZip 批量打包下载（zip 流式）。
// body: {ia?, items:[{media_id,title}], kb?, folder?}
// - 给 items：按列表打包；
// - 否则给 kb/folder：打包该文件夹下全部文件（浅层递归，上限 200 个）。
func (s *Server) handleDownloadZip(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		IA     string `json:"ia"`
		KB     string `json:"kb"`
		Folder string `json:"folder"`
		Items  []struct {
			MediaID string `json:"media_id"`
			Title   string `json:"title"`
		} `json:"items"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	c, ok := s.imaClient(w, u, req.IA)
	if !ok {
		return
	}

	const (
		maxFiles    = 200
		maxTotal    = 500 << 20
		maxPerFile  = 200 << 20
		maxRecursed = 3
	)

	type target struct{ mediaID, title string }
	targets := []target{}
	if len(req.Items) > 0 {
		for _, it := range req.Items {
			if it.MediaID != "" {
				targets = append(targets, target{it.MediaID, it.Title})
			}
		}
	} else if req.KB != "" {
		// 遍历文件夹收集文件
		queue := []struct {
			folder string
			depth  int
		}{{folder: req.Folder, depth: 0}}
		if req.Folder == "" {
			queue[0].folder = req.KB
		}
		for len(queue) > 0 && len(targets) < maxFiles {
			cur := queue[0]
			queue = queue[1:]
			if cur.depth > maxRecursed {
				continue
			}
			cursor := ""
			for page := 0; page < 50 && len(targets) < maxFiles; page++ {
				rsp, err := c.GetKnowledgeList(r.Context(), req.KB, cur.folder, cursor, 50)
				if err != nil {
					break
				}
				for _, f := range rsp.Folders {
					queue = append(queue, struct {
						folder string
						depth  int
					}{folder: f.FolderID, depth: cur.depth + 1})
				}
				for _, f := range rsp.Files {
					targets = append(targets, target{f.MediaID, f.Title})
					if len(targets) >= maxFiles {
						break
					}
				}
				if rsp.IsEnd || rsp.NextCursor == "" {
					break
				}
				cursor = rsp.NextCursor
			}
		}
	} else {
		writeBizErr(w, "缺少 items 或 kb 参数")
		return
	}
	if len(targets) == 0 {
		writeBizErr(w, "没有可下载的文件")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"imahub-%s.zip\"", time.Now().Format("20060102-150405")))

	zw := zip.NewWriter(w)
	defer zw.Close()

	used := map[string]int{}
	var total int64
	for _, t := range targets {
		if total > maxTotal {
			break
		}
		name := strings.TrimSpace(t.title)
		if name == "" {
			name = t.mediaID
		}
		// zip 内重名去重
		used[name]++
		if used[name] > 1 {
			name = fmt.Sprintf("%d_%s", used[name], name)
		}
		if err := s.writeZipEntry(zw, c, r.Context(), t.mediaID, name, maxPerFile, &total); err != nil {
			// 失败条目留错误说明，保证用户知道缺了什么
			if f, zerr := zw.Create("[失败] " + name + ".txt"); zerr == nil {
				fmt.Fprintf(f, "下载失败：%s", err)
			}
		}
	}
}

// writeZipEntry 拉取一个条目写入 zip。
func (s *Server) writeZipEntry(zw *zip.Writer, c *ima.Client, ctx context.Context, mediaID, name string, maxPerFile int64, total *int64) error {
	info, err := c.GetMediaInfo(ctx, mediaID)
	if err != nil {
		return err
	}
	// 笔记类：导出文本
	if info.NotebookExt != nil && info.NotebookExt.NotebookID != "" {
		content, err := c.GetNoteContent(ctx, info.NotebookExt.NotebookID)
		if err != nil {
			return err
		}
		f, err := zw.Create(strings.TrimSuffix(name, "") + ".txt")
		if err != nil {
			return err
		}
		n, _ := io.WriteString(f, content)
		*total += int64(n)
		return nil
	}
	body, _, err := c.FetchMedia(ctx, info)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	n, _ := io.Copy(f, io.LimitReader(body, maxPerFile))
	*total += n
	return nil
}

// ---------- 团队公告 ----------

func (s *Server) handleListAnnouncements(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeOK(w, map[string]any{"announcements": s.Store.ListAnnouncements(5)})
}

func (s *Server) handleCreateAnnouncement(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Content string `json:"content"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeBizErr(w, "公告内容不能为空")
		return
	}
	a, err := s.Store.AddAnnouncement(store.Announcement{
		UserID: u.ID, By: u.Username, Content: req.Content,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	writeOK(w, map[string]any{"announcement": a})
}

func (s *Server) handleUpdateAnnouncement(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	var req struct {
		Content string `json:"content"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeBizErr(w, "公告内容不能为空")
		return
	}
	err := s.Store.UpdateAnnouncement(id, func(a *store.Announcement) error {
		a.Content = req.Content
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, 404, "公告不存在")
		return
	}
	writeOK(w, nil)
}

func (s *Server) handleDeleteAnnouncement(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	if err := s.Store.DeleteAnnouncement(id); err != nil {
		writeErr(w, http.StatusNotFound, 404, "公告不存在")
		return
	}
	writeOK(w, nil)
}
