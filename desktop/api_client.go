package main

// API 客户端：桌面界面 ↔ 本机引擎（同一进程后端，走 HTTP cookie 会话）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"
)

type apiClient struct {
	base string
	cli  *http.Client
}

func newAPIClient(base string) *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{
		base: base,
		cli:  &http.Client{Jar: jar, Timeout: 20 * time.Second},
	}
}

type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (c *apiClient) do(method, path string, body any) (map[string]any, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequest(method, c.base+path, &buf)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("响应解析失败")
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("%s", r.Msg)
	}
	out := map[string]any{}
	if len(r.Data) > 0 {
		_ = json.Unmarshal(r.Data, &out)
	}
	return out, nil
}

func (c *apiClient) get(path string) (map[string]any, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *apiClient) post(path string, body any) (map[string]any, error) {
	return c.do(http.MethodPost, path, body)
}
