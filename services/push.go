package services

// 手机推送：Bark / Server酱 / 企业微信 / Telegram / 通用 Webhook。
// 参考 https://github.com/dashug/apple-store-helper 的 services/notify.go 移植，
// 并补上对各渠道业务错误码的检查（Server酱、企业微信出错时 HTTP 仍是 200）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

const (
	ChannelBark       = "Bark"
	ChannelServerChan = "Server酱"
	ChannelWeCom      = "企业微信"
	ChannelTelegram   = "Telegram"
	ChannelWebhook    = "Webhook"
)

// pushClient 独立于库存查询：推送慢不能拖住监听
var pushClient = &http.Client{Timeout: 10 * time.Second}

// PushMessage 一条待推送的提醒
type PushMessage struct {
	Title   string
	Content string
	URL     string
}

// PushResult 单个推送地址的结果。逐条返回，配了多个渠道坏一个时能看出是哪个
type PushResult struct {
	Target  string
	Channel string
	Err     error
}

// DetectChannel 按地址域名识别渠道，无法识别的按通用 Webhook 处理
func DetectChannel(target string) string {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return ChannelWebhook
	}

	switch host := strings.ToLower(parsed.Host); {
	case strings.HasSuffix(host, "day.app"):
		return ChannelBark
	case strings.HasSuffix(host, "ftqq.com"):
		return ChannelServerChan
	case strings.HasSuffix(host, "qyapi.weixin.qq.com"):
		return ChannelWeCom
	case strings.HasSuffix(host, "api.telegram.org"):
		return ChannelTelegram
	default:
		return ChannelWebhook
	}
}

// SplitPushTargets 按行拆分推送地址，忽略空行和 # 开头的注释
func SplitPushTargets(raw string) []string {
	var targets []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, line)
	}
	return targets
}

// buildPushRequest 按渠道组装请求
func buildPushRequest(target string, channel string, m PushMessage) (*http.Request, error) {
	switch channel {
	case ChannelBark:
		// {地址}/{标题}/{正文}?url=...，标题与正文在 path 中必须转义
		link := fmt.Sprintf("%s/%s/%s?%s",
			strings.TrimRight(target, "/"),
			url.PathEscape(m.Title),
			url.PathEscape(m.Content),
			url.Values{"url": []string{m.URL}}.Encode(),
		)
		return http.NewRequest(http.MethodGet, link, nil)

	case ChannelServerChan:
		form := url.Values{
			"title": []string{m.Title},
			// desp 是 Markdown，裸地址在微信里不一定能点，写成链接；另附原地址方便复制
			"desp": []string{m.Content + "\n\n[👉 打开购物车](" + m.URL + ")\n\n" + m.URL},
		}
		req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil

	case ChannelWeCom:
		return jsonRequest(target, map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": m.Title + "\n" + m.Content + "\n" + m.URL},
		})

	case ChannelTelegram:
		// chat_id 由用户写在地址里：https://api.telegram.org/bot<TOKEN>/sendMessage?chat_id=<ID>
		parsed, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		query := parsed.Query()
		query.Set("text", m.Title+"\n"+m.Content+"\n"+m.URL)
		parsed.RawQuery = query.Encode()
		return http.NewRequest(http.MethodGet, parsed.String(), nil)

	default:
		return jsonRequest(target, map[string]string{
			"title":   m.Title,
			"content": m.Content,
			"url":     m.URL,
		})
	}
}

func jsonRequest(target string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// checkPushResponse 部分渠道出错时 HTTP 仍返回 200，需要看业务错误码
func checkPushResponse(channel string, status int, body string) error {
	snippet := strings.TrimSpace(body)
	if len(snippet) > 120 {
		snippet = snippet[:120] + "…"
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d %s", status, snippet)
	}

	switch channel {
	case ChannelServerChan:
		if code := gjson.Get(body, "code"); code.Exists() && code.Int() != 0 {
			return fmt.Errorf("Server酱返回错误：%s", snippet)
		}
	case ChannelWeCom:
		if code := gjson.Get(body, "errcode"); code.Exists() && code.Int() != 0 {
			return fmt.Errorf("企业微信返回错误：%s", gjson.Get(body, "errmsg").String())
		}
	case ChannelTelegram:
		if ok := gjson.Get(body, "ok"); ok.Exists() && !ok.Bool() {
			return fmt.Errorf("Telegram 返回错误：%s", gjson.Get(body, "description").String())
		}
	case ChannelBark:
		if code := gjson.Get(body, "code"); code.Exists() && code.Int() != 200 {
			return fmt.Errorf("Bark 返回错误：%s", gjson.Get(body, "message").String())
		}
	}
	return nil
}

func pushOne(target string, m PushMessage) PushResult {
	channel := DetectChannel(target)
	result := PushResult{Target: target, Channel: channel}

	req, err := buildPushRequest(target, channel, m)
	if err != nil {
		result.Err = fmt.Errorf("地址无法解析: %w", err)
		return result
	}

	resp, err := pushClient.Do(req)
	if err != nil {
		result.Err = err
		return result
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	result.Err = checkPushResponse(channel, resp.StatusCode, string(body))
	return result
}

// Push 并发推送到所有地址，按输入顺序返回每个地址的结果
func Push(raw string, m PushMessage) []PushResult {
	targets := SplitPushTargets(raw)
	results := make([]PushResult, len(targets))

	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			results[i] = pushOne(target, m)
		}(i, target)
	}
	wg.Wait()

	return results
}

// MaskTarget 隐藏地址中的密钥部分，用于界面和日志展示
func MaskTarget(target string) string {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		if len(target) > 12 {
			return target[:12] + "…"
		}
		return target
	}
	path := parsed.Path
	if len(path) > 8 {
		path = path[:8] + "…"
	}
	return parsed.Scheme + "://" + parsed.Host + path
}
