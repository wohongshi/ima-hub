// Package ask 知识库智能问答（agent 级查询能力）：
// 全团队知识库检索 + 可配置的 OpenAI 兼容模型合成回答，内置自跑。
package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"imahub/internal/ima"
)

// Account 参与检索的团队账号。
type Account struct {
	ID     string
	Name   string
	Client *ima.Client
}

// Source 引用来源。
type Source struct {
	IA          string `json:"ia"`
	AccountName string `json:"account_name"`
	KBID        string `json:"kb_id"`
	KBName      string `json:"kb_name"`
	MediaID     string `json:"media_id"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
}

// Options 问答参数。
type Options struct {
	KBID    string // 限定知识库（空=全部）
	TopK    int    // 引用条数（默认 6）
	MaxCtx  int    // 单条资料字符数（默认 800）
	BaseURL string // OpenAI 兼容 base（如 https://api.openai.com/v1），空=仅检索
	Model   string
	APIKey  string
}

// Result 问答结果。
type Result struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
	Mode    string   `json:"mode"` // llm | retrieval
	Model   string   `json:"model,omitempty"`
}

// Run 检索全团队知识库并（可选）用模型合成回答。
func Run(ctx context.Context, accs []Account, question string, opt Options) (Result, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return Result{}, errors2("问题不能为空")
	}
	if opt.TopK <= 0 {
		opt.TopK = 6
	}
	if opt.MaxCtx <= 0 {
		opt.MaxCtx = 800
	}

	sources, errs := retrieve(ctx, accs, question, opt)
	if len(sources) == 0 && len(errs) > 0 {
		// 后端全挂（凭证失效/网络故障）时如实报错，不给空答案
		return Result{}, errors2("知识库检索失败：" + strings.Join(errs, "；"))
	}
	if len(sources) > opt.TopK {
		sources = sources[:opt.TopK]
	}
	res := Result{Sources: sources, Mode: "retrieval"}
	if opt.BaseURL == "" || opt.Model == "" {
		return res, nil
	}
	answer, err := synthesize(ctx, question, sources, opt)
	if err != nil {
		return res, err
	}
	res.Answer = answer
	res.Mode = "llm"
	res.Model = opt.Model
	return res, nil
}

type kbRef struct {
	acc    Account
	kbID   string
	kbName string
}

// retrieve 跨账号跨知识库检索（search_knowledge 全文匹配），失败逐路收集不静默。
func retrieve(ctx context.Context, accs []Account, question string, opt Options) ([]Source, []string) {
	seen := map[string]bool{}
	out := []Source{}
	errs := []string{}
	// 先收集知识库（每个账号最多 50 个）
	for _, acc := range accs {
		kbs, err := acc.Client.SearchKnowledgeBase(ctx, "", "", 50)
		if err != nil {
			errs = append(errs, fmt.Sprintf("账号「%s」知识库列表失败：%v", acc.Name, err))
			continue
		}
		for _, kb := range kbs.InfoList {
			if opt.KBID != "" && kb.ID != opt.KBID {
				continue
			}
			rsp, err := acc.Client.SearchKnowledge(ctx, kb.ID, question, "")
			if err != nil {
				errs = append(errs, fmt.Sprintf("知识库「%s」检索失败：%v", kb.Name, err))
				continue
			}
			for _, hit := range rsp.InfoList {
				if seen[hit.MediaID] {
					continue
				}
				seen[hit.MediaID] = true
				out = append(out, Source{
					IA: acc.ID, AccountName: acc.Name,
					KBID: kb.ID, KBName: kb.Name,
					MediaID: hit.MediaID, Title: hit.Title,
					Snippet: cleanHighlight(hit.HighlightContent),
				})
				if len(out) >= opt.TopK*2 {
					return out, errs
				}
			}
		}
	}
	return out, errs
}

func cleanHighlight(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// synthesize 用 OpenAI 兼容接口合成回答（工具内置，不依赖调用方的 agent）。
func synthesize(ctx context.Context, question string, sources []Source, opt Options) (string, error) {
	var sb strings.Builder
	sb.WriteString("以下是团队知识库中与问题相关的资料片段（可能不完整）：\n\n")
	for i, s := range sources {
		sn := s.Snippet
		if len([]rune(sn)) > opt.MaxCtx {
			sn = string([]rune(sn)[:opt.MaxCtx]) + "…"
		}
		fmt.Fprintf(&sb, "[%d]《%s》（知识库：%s）\n%s\n\n", i+1, s.Title, s.KBName, sn)
	}
	sb.WriteString("请仅依据以上资料回答问题；资料不足时明确说「知识库中没有找到」，不要编造。回答用中文，简洁分点。")

	payload := map[string]any{
		"model": opt.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是团队知识库问答助手，只依据给定资料回答。"},
			{"role": "user", "content": sb.String() + "\n\n问题：" + question},
		},
		"temperature": 0.3,
	}
	raw, _ := json.Marshal(payload)

	url := strings.TrimRight(opt.BaseURL, "/") + "/chat/completions"
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if opt.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+opt.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("模型调用失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("模型返回 HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("模型响应解析失败: %s", truncate(string(body), 200))
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type errStr string

func (e errStr) Error() string { return string(e) }

func errors2(s string) error { return errStr(s) }
