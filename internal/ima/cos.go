package ima

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// cosUpload 复刻官方 cos-upload.cjs：COS PUT Object + q-sign-algorithm=sha1 签名。
// 参考 https://cloud.tencent.com/document/product/436/7778
func cosUpload(ctx context.Context, cred Credential, contentType string, r io.Reader, size int64) error {
	if cred.BucketName == "" || cred.CosKey == "" {
		return fmt.Errorf("COS 凭证不完整")
	}
	startTime := cred.StartTime.Int64()
	if startTime == 0 {
		startTime = time.Now().Unix()
	}
	expiredTime := cred.ExpiredTime.Int64()
	if expiredTime == 0 {
		if startTime == 0 {
			startTime = time.Now().Unix()
		}
		expiredTime = startTime + 3600
	}

	hostname := cred.BucketName + ".cos." + cred.Region + ".myqcloud.com"
	pathname := "/" + cred.CosKey
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	signHeaders := map[string]string{
		"content-length": strconv.FormatInt(size, 10),
		"host":           hostname,
	}
	authorization := buildCOSAuthorization(cred.SecretID, cred.SecretKey, "PUT", pathname, signHeaders, startTime, expiredTime)

	target := "https://" + hostname + pathname
	if ep := os.Getenv("IMA_COS_ENDPOINT"); ep != "" {
		// 测试钩子：把 COS PUT 指向本地 mock（仅运维测试用）
		target = strings.TrimRight(ep, "/") + "/" + cred.CosKey
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, io.LimitReader(r, size))
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", authorization)
	req.Header.Set("x-cos-security-token", cred.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("COS 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func hmacSHA1Hex(key, data string) string {
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func sha1Hex(data string) string {
	s := sha1.Sum([]byte(data))
	return hex.EncodeToString(s[:])
}

func buildCOSAuthorization(secretID, secretKey, method, pathname string, headers map[string]string, startTime, expiredTime int64) string {
	keyTime := fmt.Sprintf("%d;%d", startTime, expiredTime)

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, strings.ToLower(k)+"="+url.QueryEscape(headers[k]))
	}
	httpHeaders := strings.Join(parts, "&")
	httpString := strings.ToLower(method) + "\n" + pathname + "\n\n" + httpHeaders + "\n"

	signKey := hmacSHA1Hex(secretKey, keyTime)
	stringToSign := "sha1\n" + keyTime + "\n" + sha1Hex(httpString) + "\n"
	signature := hmacSHA1Hex(signKey, stringToSign)

	headerList := strings.Join(keys, ";")
	headerList = strings.ToLower(headerList)
	return strings.Join([]string{
		"q-sign-algorithm=sha1",
		"q-ak=" + secretID,
		"q-sign-time=" + keyTime,
		"q-key-time=" + keyTime,
		"q-header-list=" + headerList,
		"q-url-param-list=",
		"q-signature=" + signature,
	}, "&")
}
