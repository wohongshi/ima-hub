package api

import (
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"

	"imahub/internal/ima"
	"imahub/internal/store"
)

// uploadArchive 压缩包（zip/7z/rar）自动解压上传：
// 解压到临时目录后逐个文件走标准上传流程（同一账号/知识库），返回逐项结果。
func (s *Server) uploadArchive(w http.ResponseWriter, r *http.Request, u *store.User,
	acc *store.IMAAccount, c *ima.Client, kbID, folderID, name string, fileHeader *multipart.FileHeader, force bool) {

	const maxArchiveSelf = 500 << 20
	if fileHeader.Size > maxArchiveSelf {
		writeBizErr(w, "压缩包超过大小上限（500MB）")
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	defer src.Close()

	tmp, err := os.CreateTemp(s.TmpDir, "arc-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		writeErr(w, http.StatusInternalServerError, 500, "压缩包暂存失败: "+err.Error())
		return
	}
	tmp.Close()

	dest := tmp.Name() + "_x"
	if err := os.MkdirAll(dest, 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, 500, err.Error())
		return
	}
	defer os.RemoveAll(dest)

	files, err := ima.ExtractArchive(tmp.Name(), dest)
	if err != nil {
		writeBizErr(w, err.Error())
		return
	}
	if len(files) == 0 {
		writeBizErr(w, "压缩包里没有可上传的文件")
		return
	}

	uploaded := []map[string]any{}
	failed := []map[string]any{}
	skipped := []map[string]any{}
	var totalSize int64
	for _, e := range files {
		// 同名闸：与单文件流程一致；批量语义为跳过并报清单（force 才覆盖上传）
		if !force {
			if mt := ima.DetectMediaType(e.RelName); mt >= 0 {
				if names, err := c.CheckRepeatedNames(r.Context(), kbID, folderID, []string{e.RelName}, mt); err == nil && names[e.RelName] {
					skipped = append(skipped, map[string]any{"title": e.RelName, "reason": "知识库已存在同名条目（加 force 覆盖上传）"})
					continue
				}
			}
		}
		f, err := os.Open(e.Path)
		if err != nil {
			failed = append(failed, map[string]any{"title": e.RelName, "error": err.Error()})
			continue
		}
		mediaID, repeated, err := c.UploadFile(r.Context(), kbID, folderID, e.RelName, e.Size, f)
		f.Close()
		if err != nil {
			failed = append(failed, map[string]any{"title": e.RelName, "error": err.Error()})
			continue
		}
		totalSize += e.Size
		uploaded = append(uploaded, map[string]any{
			"title": e.RelName, "media_id": mediaID, "repeated": repeated,
		})
	}

	if len(uploaded) > 0 {
		s.Store.BumpUpload(u.ID, totalSize)
		s.Store.AddActivity(store.Activity{
			Kind:  "upload",
			Title: name + "（自动解压 " + strconv.Itoa(len(uploaded)) + " 个文件）",
			IA:    acc.ID, AccountName: acc.Name,
			KBName: r.FormValue("kb_name"),
			Size:   totalSize, UserID: u.ID, By: u.Username,
		})
	}

	writeOK(w, map[string]any{
		"archive":      true,
		"file_name":    name,
		"extracted":    len(files),
		"uploaded":     uploaded,
		"failed":       failed,
		"skipped":      skipped,
		"ia":           acc.ID,
		"account_name": acc.Name,
		"kb_id":        kbID,
	})
}
