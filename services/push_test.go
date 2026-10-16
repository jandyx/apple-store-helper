package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetectChannel(t *testing.T) {
	cases := map[string]string{
		"https://api.day.app/abc":                                ChannelBark,
		"https://sctapi.ftqq.com/SCT123.send":                    ChannelServerChan,
		"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k": ChannelWeCom,
		"https://api.telegram.org/bot1:x/sendMessage?chat_id=1":  ChannelTelegram,
		"https://example.com/hook":                               ChannelWebhook,
		"not a url":                                              ChannelWebhook,
	}
	for target, want := range cases {
		if got := DetectChannel(target); got != want {
			t.Errorf("DetectChannel(%q) = %s, want %s", target, got, want)
		}
	}
}

func TestSplitPushTargets(t *testing.T) {
	got := SplitPushTargets(" https://a \n\n# 注释\nhttps://b\n")
	if len(got) != 2 || got[0] != "https://a" || got[1] != "https://b" {
		t.Fatalf("unexpected targets: %q", got)
	}
}

func TestBuildBarkRequestEscapes(t *testing.T) {
	m := PushMessage{Title: "有货 提醒", Content: "广州/天环 有货", URL: "https://www.apple.com.cn/shop/bag"}
	req, err := buildPushRequest("https://api.day.app/KEY/", ChannelBark, m)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Query().Get("url") != m.URL {
		t.Fatalf("url param lost: %s", req.URL)
	}
	if !strings.HasPrefix(req.URL.EscapedPath(), "/KEY/") || strings.Count(req.URL.EscapedPath(), "/") != 3 {
		t.Fatalf("title/content not escaped into exactly two segments: %s", req.URL.EscapedPath())
	}
}

func TestBuildTelegramKeepsChatID(t *testing.T) {
	req, err := buildPushRequest("https://api.telegram.org/bot1:x/sendMessage?chat_id=42", ChannelTelegram, PushMessage{Title: "t", Content: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Query().Get("chat_id") != "42" || !strings.Contains(req.URL.Query().Get("text"), "t\nc") {
		t.Fatalf("unexpected telegram url: %s", req.URL)
	}
}

func TestCheckPushResponse(t *testing.T) {
	cases := []struct {
		channel string
		status  int
		body    string
		wantErr bool
	}{
		{ChannelServerChan, 200, `{"code":0,"message":""}`, false},
		{ChannelServerChan, 200, `{"code":40001,"message":"bad pushkey"}`, true},
		{ChannelWeCom, 200, `{"errcode":0,"errmsg":"ok"}`, false},
		{ChannelWeCom, 200, `{"errcode":93000,"errmsg":"invalid webhook url"}`, true},
		{ChannelTelegram, 200, `{"ok":false,"description":"chat not found"}`, true},
		{ChannelBark, 200, `{"code":200,"message":"success"}`, false},
		{ChannelBark, 200, `{"code":400,"message":"failed to get device token"}`, true},
		{ChannelWebhook, 500, `oops`, true},
		{ChannelWebhook, 200, `anything`, false},
	}
	for _, c := range cases {
		err := checkPushResponse(c.channel, c.status, c.body)
		if (err != nil) != c.wantErr {
			t.Errorf("%s %d %s: err=%v, wantErr=%v", c.channel, c.status, c.body, err, c.wantErr)
		}
	}
}

func TestPushWebhookEndToEnd(t *testing.T) {
	var got map[string]string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
	}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()

	results := Push(ok.URL+"\n"+bad.URL, PushMessage{Title: "有货提醒", Content: "天环 Duo", URL: "https://x"})
	if len(results) != 2 || results[0].Err != nil || results[1].Err == nil {
		t.Fatalf("unexpected results: %+v", results)
	}
	if got["title"] != "有货提醒" || got["content"] != "天环 Duo" || got["url"] != "https://x" {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestMaskTargetHidesKey(t *testing.T) {
	masked := MaskTarget("https://api.day.app/AbCdEf0123456789FakeBarkKey")
	if strings.Contains(masked, "456789FakeBark") {
		t.Fatalf("key leaked: %s", masked)
	}
}
