package api

import (
	"net/http"
	"strings"

	"imahub/internal/ask"
	"imahub/internal/store"
)

// handleAsk 知识库智能问答（agent 级查询；全员可用）。
// body: {q, kb_id?, top_k?, ia?}  ia 限定单账号（空=全部团队账号）。
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Q     string `json:"q"`
		KBID  string `json:"kb_id"`
		TopK  int    `json:"top_k"`
		IA    string `json:"ia"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Q = strings.TrimSpace(req.Q)
	if req.Q == "" {
		writeBizErr(w, "问题不能为空")
		return
	}

	accs := s.poolAccounts(u)
	if len(accs) == 0 {
		writeBizErr(w, "团队尚未配置 IMA 账号（组长可在设置页添加）")
		return
	}
	askAccs := make([]ask.Account, 0, len(accs))
	for i := range accs {
		a := accs[i]
		if req.IA != "" && req.IA != "default" && a.ID != req.IA && a.Name != req.IA {
			continue
		}
		askAccs = append(askAccs, ask.Account{ID: a.ID, Name: a.Name, Client: s.clientFor(&a)})
	}
	if len(askAccs) == 0 {
		writeBizErr(w, "IMA 账号不存在: "+req.IA)
		return
	}

	st := s.Store.GetSettings()
	res, err := ask.Run(r.Context(), askAccs, req.Q, ask.Options{
		KBID:    req.KBID,
		TopK:    req.TopK,
		BaseURL: st.AskBaseURL,
		Model:   st.AskModel,
		APIKey:  st.AskAPIKey,
	})
	if err != nil {
		// 检索结果仍有价值：带回来源并透出错误
		writeOK(w, map[string]any{
			"answer": "", "sources": res.Sources, "mode": res.Mode,
			"error": err.Error(),
		})
		return
	}
	writeOK(w, map[string]any{
		"answer": res.Answer, "sources": res.Sources, "mode": res.Mode, "model": res.Model,
	})
}

// handleAskStatus 问答能力状态（全员可见）。
func (s *Server) handleAskStatus(w http.ResponseWriter, r *http.Request, u *store.User) {
	st := s.Store.GetSettings()
	writeOK(w, map[string]any{
		"configured": st.AskBaseURL != "" && st.AskModel != "",
		"model":      st.AskModel,
	})
}
