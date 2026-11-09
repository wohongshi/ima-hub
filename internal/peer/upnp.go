package peer

// UPnP-IGD 尽力而为的端口映射（NAT 自动打通第一层）：SSDP 找网关 → SOAP
// AddPortMapping 把本节点 TCP 端口映射到公网 → 周期续租 → 退出撤销。
// 任何一步失败都只记日志静默放弃（中继信箱兜底，映射只是加速直连）。

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TryUPnP 尽力映射 TCP 端口（阻塞到 stop；失败即退）。
func TryUPnP(port int, stop <-chan struct{}) {
	gw, ctrlURL, svc, err := findIGD()
	if err != nil {
		log.Printf("UPnP：未找到网关（%v），中继兜底", err)
		return
	}
	log.Printf("UPnP：网关 %s，映射端口 %d", gw, port)
	client := &http.Client{Timeout: 10 * time.Second}
	_ = soapCall(client, ctrlURL, svc, "AddPortMapping", map[string]string{
		"NewRemoteHost":             "",
		"NewExternalPort":           strconv.Itoa(port),
		"NewProtocol":               "TCP",
		"NewInternalPort":           strconv.Itoa(port),
		"NewInternalClient":         localIPv4(),
		"NewEnabled":                "1",
		"NewPortMappingDescription": "imahub-p2p",
		"NewLeaseDuration":          "1800",
	})
	// 续租
	tk := time.NewTicker(20 * time.Minute)
	defer tk.Stop()
	for {
		select {
		case <-stop:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = soapCallContext(ctx, client, ctrlURL, svc, "DeletePortMapping", map[string]string{
				"NewRemoteHost":   "",
				"NewExternalPort": strconv.Itoa(port),
				"NewProtocol":     "TCP",
			})
			cancel()
			return
		case <-tk.C:
			if err := soapCall(client, ctrlURL, svc, "AddPortMapping", map[string]string{
				"NewRemoteHost":             "",
				"NewExternalPort":           strconv.Itoa(port),
				"NewProtocol":               "TCP",
				"NewInternalPort":           strconv.Itoa(port),
				"NewInternalClient":         localIPv4(),
				"NewEnabled":                "1",
				"NewPortMappingDescription": "imahub-p2p",
				"NewLeaseDuration":          "1800",
			}); err != nil {
				log.Printf("UPnP：续租失败 %v", err)
			}
		}
	}
}

// findIGD SSDP 搜网关并解析出 WAN 连接控制地址。
func findIGD() (gwAddr, ctrlURL, svcType string, err error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return "", "", "", err
	}
	defer conn.Close()
	msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	dst, _ := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if _, err := conn.WriteTo([]byte(msg), dst); err != nil {
		return "", "", "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 2048)
	n, from, err := conn.ReadFrom(buf)
	if err != nil {
		return "", "", "", err
	}
	_ = from
	loc := regexp.MustCompile(`(?i)location:\s*(\S+)`).FindStringSubmatch(string(buf[:n]))
	if loc == nil {
		return "", "", "", fmt.Errorf("SSDP 响应无 LOCATION")
	}
	resp, err := http.Get(strings.TrimSpace(loc[1])) //nolint:gosec // 网关本地描述文档
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// 找 WANIPConnection / WANPPPConnection 的 controlURL
	var dev struct {
		XMLName xml.Name `xml:"root"`
	}
	_ = xml.Unmarshal(raw, &dev)
	desc := string(raw)
	svcTypes := []string{"urn:schemas-upnp-org:service:WANIPConnection:1", "urn:schemas-upnp-org:service:WANPPPConnection:1"}
	for _, st := range svcTypes {
		if idx := strings.Index(desc, st); idx >= 0 {
			seg := desc[idx:]
			if m := regexp.MustCompile(`<controlURL>\s*([^<]+)\s*</controlURL>`).FindStringSubmatch(seg); m != nil {
				base := strings.TrimRight(strings.TrimSpace(loc[1]), "")
				u := strings.TrimSpace(m[1])
				if strings.HasPrefix(u, "http") {
					return base, u, st, nil
				}
				// 相对路径拼接
				bu := regexp.MustCompile(`^(https?://[^/]+)`).FindStringSubmatch(base)
				if bu == nil {
					return "", "", "", fmt.Errorf("controlURL 拼不出")
				}
				return base, bu[1] + u, st, nil
			}
		}
	}
	return "", "", "", fmt.Errorf("描述里无 WAN 连接服务")
}

func soapCall(client *http.Client, ctrlURL, svcType, action string, args map[string]string) error {
	return soapCallContext(context.Background(), client, ctrlURL, svcType, action, args)
}

// soapCallContext SOAP 动作调用。
func soapCallContext(ctx context.Context, client *http.Client, ctrlURL, svcType, action string, args map[string]string) error {
	var params strings.Builder
	for k, v := range args {
		params.WriteString("<" + k + ">" + xmlEscape(v) + "</" + k + ">")
	}
	payload := "<?xml version=\"1.0\"?>" +
		"<s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\" s:encodingStyle=\"http://schemas.xmlsoap.org/soap/encoding/\">" +
		"<s:Body><u:" + action + " xmlns:u=\"" + svcType + "\">" + params.String() +
		"</u:" + action + "></s:Body></s:Envelope>"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ctrlURL, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/xml; charset=\"utf-8\"")
	req.Header.Set("SOAPAction", "\""+svcType+"#"+action+"\"")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("SOAP %s 失败 %d: %s", action, resp.StatusCode, truncate(string(raw), 160))
	}
	return nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// localIPv4 第一个非环回 IPv4。
func localIPv4() string {
	conn, err := net.Dial("udp4", "223.5.5.5:53")
	if err == nil {
		defer conn.Close()
		if a, ok := conn.LocalAddr().(*net.UDPAddr); ok && a.IP.To4() != nil {
			return a.IP.String()
		}
	}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "0.0.0.0"
}
