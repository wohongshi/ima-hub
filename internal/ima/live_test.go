package ima

// 实连验收测试：用真实 ima 凭证验证引擎全链路（只读 + 影子上传）。
// 运行：IMA_LIVE_DATA=/path/to/data.json go test ./internal/ima -run TestLiveAPI -v
// 凭证从 data.json 自动读取，绝不打印；影子上传只走 create_media + COS，不调 add_knowledge，
// 不会在知识库留下文件。

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func loadLiveCreds(t *testing.T) (string, string) {
	t.Helper()
	p := os.Getenv("IMA_LIVE_DATA")
	if p == "" {
		t.Skip("未设置 IMA_LIVE_DATA，跳过实连测试")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", p, err)
	}
	var d struct {
		TeamAccounts []struct {
			ClientID string `json:"client_id"`
			APIKey   string `json:"api_key"`
		} `json:"team_accounts"`
		Settings struct {
			DefaultIMAClientID string `json:"default_ima_client_id"`
			DefaultIMAAPIKey   string `json:"default_ima_api_key"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("解析 data.json 失败: %v", err)
	}
	if len(d.TeamAccounts) > 0 && d.TeamAccounts[0].ClientID != "" {
		return d.TeamAccounts[0].ClientID, d.TeamAccounts[0].APIKey
	}
	if d.Settings.DefaultIMAClientID != "" {
		return d.Settings.DefaultIMAClientID, d.Settings.DefaultIMAAPIKey
	}
	t.Fatal("data.json 中没有可用凭证（团队池与服务端默认均为空）")
	return "", ""
}

func TestLiveAPI(t *testing.T) {
	cid, key := loadLiveCreds(t)
	c := New(cid, key, "")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// 1) 连通性
	t.Log("== 1. 连通性（get_addable_knowledge_base_list）==")
	if _, err := c.GetAddableKnowledgeBaseList(ctx, "", 1); err != nil {
		t.Fatalf("认证/连通失败: %v", err)
	}
	t.Log("OK：凭证有效")

	// 2) 知识库列表
	t.Log("== 2. 知识库列表 ==")
	kbs, err := c.SearchKnowledgeBase(ctx, "", "", 20)
	if err != nil {
		t.Fatalf("search_knowledge_base 失败: %v", err)
	}
	for _, kb := range kbs.InfoList {
		t.Logf("KB: %s (id=%s)", kb.Name, kb.ID)
	}
	if len(kbs.InfoList) == 0 {
		t.Log("（账号里还没有知识库，后续浏览类用例跳过）")
	}

	// 3) 浏览首个知识库（验证文件/文件夹混排解析）
	totalFiles, totalFolders := 0, 0
	var firstFile *KnowledgeInfo
	if len(kbs.InfoList) > 0 {
		kb := kbs.InfoList[0]
		t.Log("== 3. 浏览:", kb.Name, "==")
		queue := []string{kb.ID}
		depth := 0
		for len(queue) > 0 && depth < 3 {
			folder := queue[0]
			queue = queue[1:]
			cursor := ""
			for {
				rsp, err := c.GetKnowledgeList(ctx, kb.ID, folder, cursor, 20)
				if err != nil {
					t.Fatalf("get_knowledge_list 失败: %v", err)
				}
				for _, f := range rsp.Folders {
					totalFolders++
					if depth < 2 {
						queue = append(queue, f.FolderID)
					}
					t.Logf("  [文件夹] %s（文件 %d 子夹 %d）", f.Name, f.FileNumber, f.FolderNumber)
				}
				for _, f := range rsp.Files {
					totalFiles++
					if firstFile == nil {
						fcopy := f
						firstFile = &fcopy
					}
					t.Logf("  [文件] %s", f.Title)
				}
				if rsp.IsEnd || rsp.NextCursor == "" {
					break
				}
				cursor = rsp.NextCursor
			}
			depth++
		}
		t.Logf("浏览统计：文件 %d，文件夹 %d", totalFiles, totalFolders)
	}

	// 4) 搜索（用发现到的文件名做关键词，保证有命中）
	if firstFile != nil && len(kbs.InfoList) > 0 {
		t.Log("== 4. 知识库内搜索 ==")
		kw := firstFile.Title
		if i := strings.IndexAny(kw, ".-_ "); i > 2 {
			kw = kw[:i]
		}
		rsp, err := c.SearchKnowledge(ctx, kbs.InfoList[0].ID, kw, "")
		if err != nil {
			t.Fatalf("search_knowledge 失败: %v", err)
		}
		t.Logf("搜索 %q 命中 %d 条", kw, len(rsp.InfoList))
		for _, h := range rsp.InfoList {
			t.Logf("  命中: %s", h.Title)
		}

		// 5) 下载验证（get_media_info + 拉前 64 字节）
		t.Log("== 5. 条目下载链路 ==")
		info, err := c.GetMediaInfo(ctx, firstFile.MediaID)
		if err != nil {
			t.Fatalf("get_media_info 失败: %v", err)
		}
		t.Logf("media_type=%d 有URL=%v 笔记=%v", info.MediaType, info.URLInfo != nil, info.NotebookExt != nil)
		if info.URLInfo != nil {
			body, _, err := c.FetchMedia(ctx, info)
			if err != nil {
				t.Fatalf("原文拉取失败: %v", err)
			}
			buf := make([]byte, 64)
			n, _ := body.Read(buf)
			body.Close()
			t.Logf("原文可读，前 %d 字节: %q", n, string(buf[:n]))
		}
	}

	// 6) 笔记链路
	t.Log("== 6. 笔记链路 ==")
	nbs, err := c.ListNotebook(ctx, "0", 10)
	if err != nil {
		t.Fatalf("list_notebook 失败: %v", err)
	}
	t.Logf("笔记本 %d 个", len(nbs.NoteFolderInfos))
	for _, nb := range nbs.NoteFolderInfos {
		t.Logf("  [笔记本] %s（%d 篇）", nb.Name, nb.NoteNumber)
	}
	notes, err := c.ListNote(ctx, "", "0", 5)
	if err == nil {
		t.Logf("笔记列表 %d 篇", len(notes.NoteBookList))
		if len(notes.NoteBookList) > 0 {
			content, err := c.GetNoteContent(ctx, notes.NoteBookList[0].NoteID)
			if err != nil {
				t.Logf("读笔记失败（可容忍）: %v", err)
			} else {
				t.Logf("读笔记 OK，正文 %d 字符", len(content))
			}
		}
	}

	// 7) 影子上传（create_media + COS PUT 真实签名，不 add_knowledge，不留文件）
	t.Log("== 7. 影子上传（COS 签名链路）==")
	kbID := ""
	if len(kbs.InfoList) > 0 {
		kbID = kbs.InfoList[0].ID
	}
	if kbID == "" {
		t.Log("无知识库，跳过影子上传")
		return
	}
	content := []byte("ImaHub connectivity check " + time.Now().Format("2006-01-02 15:04:05"))
	var cm createMediaRsp
	if err := c.call(ctx, "openapi/wiki/v1/create_media", map[string]any{
		"file_name":         "ImaHub-连通测试-可忽略.txt",
		"file_size":         len(content),
		"content_type":      "text/plain",
		"knowledge_base_id": kbID,
		"file_ext":          "txt",
	}, &cm); err != nil {
		t.Fatalf("create_media 失败: %v", err)
	}
	t.Logf("create_media OK（media_id=%s）", cm.MediaID)
	if err := cosUpload(ctx, cm.CosCredential, "text/plain", bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("COS 上传失败: %v", err)
	}
	t.Log("COS PUT OK（真实签名链路通过；未调 add_knowledge，知识库无残留）")
}
