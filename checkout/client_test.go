package checkout

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateWrite(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func testConfig(t *testing.T) (Config, Target) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	c := Config{SessionFile: filepath.Join(dir, "session.json"), ContactFile: filepath.Join(dir, "contact.json"), StateDir: filepath.Join(dir, "state"), SKU: "MK2Q4CH/A", StoreIDs: []string{"R001"}, Province: "广东", City: "深圳", District: "南山区", MaxTotalCents: 1799900, Payment: "WECHAT"}
	contact := Contact{FirstName: "First", LastName: "Last", Email: "a@example.test", Phone: "123456789"}
	privateWrite(t, c.ContactFile, contact)
	privateWrite(t, c.SessionFile, Session{UserAgent: "fixture", Cookies: []Cookie{{Name: "session", Value: "private", Domain: "www.apple.com.cn", Path: "/", Secure: true}}})
	return c, Target{SKU: c.SKU, StoreID: "R001", Province: c.Province, City: c.City, District: c.District, MaxTotalCents: c.MaxTotalCents, Payment: c.Payment, Contact: contact}
}
func TestPrivateFilesAndTarget(t *testing.T) {
	c, target := testConfig(t)
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	r, _ := NewRunner(c, "")
	if e := r.validateTarget(target); e != nil {
		t.Fatal(e)
	}
	target.MaxTotalCents++
	if r.validateTarget(target) == nil {
		t.Fatal("accepted changed cap")
	}
	os.Chmod(c.ContactFile, 0644)
	if _, e := LoadContact(c.ContactFile); e == nil {
		t.Fatal("accepted public contact")
	}
	privateWrite(t, c.ContactFile, map[string]any{"first_name": "x", "unknown": "y"})
	os.Chmod(c.ContactFile, 0600)
	if _, e := LoadContact(c.ContactFile); e == nil {
		t.Fatal("accepted unknown fields")
	}
}

func TestRunnerKeepsImmutableStorePolicySnapshot(t *testing.T) {
	c, target := testConfig(t)
	r, err := NewRunner(c, ModeReview)
	if err != nil {
		t.Fatal(err)
	}
	c.StoreIDs[0] = "R_CHANGED"
	if err := r.validateTarget(target); err != nil {
		t.Fatal("caller changed an already armed runner policy", err)
	}
	target.StoreID = "R_CHANGED"
	if err := r.validateTarget(target); err == nil {
		t.Fatal("runner accepted caller-mutated store policy")
	}
}
func TestWireFailuresAreRedacted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		ct, body string
	}{{"541", 541, "text/html", "secret"}, {"html", 200, "text/html", "secret"}, {"login", 200, "text/html", `<script id="init_data">{"signIn":{"secret":"private"}}</script>`}} {
		t.Run(tc.name, func(t *testing.T) {
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.ct)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true}
			e := s.load(context.Background(), "/shop/bag")
			if e == nil || strings.Contains(e.Error(), "secret") || strings.Contains(e.Error(), sv.URL) {
				t.Fatalf("bad error %v", e)
			}
		})
	}
}
func TestWireRefusesExternalAndCancellation(t *testing.T) {
	sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("must not call") }))
	defer sv.Close()
	u, _ := url.Parse(sv.URL)
	s := wireSession{client: sv.Client(), base: u, local: true}
	for _, raw := range []string{"https://example.test/", "http://user:pass@" + u.Host + "/", "/shop/signIn"} {
		if _, e := s.resolve(raw); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := s.request(ctx, "GET", "/", nil, false); e == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestSubmissionReceiptAndLock(t *testing.T) {
	c, target := testConfig(t)
	r, _ := NewRunner(c, ModeSubmit)
	f, e := r.lock()
	if e != nil {
		t.Fatal(e)
	}
	if f2, e := r.lock(); e == nil {
		f2.Close()
		t.Fatal("accepted concurrent runner")
	}
	f.Close()
	receipt := storedReceipt{identity(target), Result{State: "submission_unknown", SKU: target.SKU, StoreID: target.StoreID}}
	path := filepath.Join(c.StateDir, "submission.json")
	if e = saveReceipt(path, receipt, true); e != nil {
		t.Fatal(e)
	}
	if e = saveReceipt(path, receipt, true); e == nil {
		t.Fatal("overwrote receipt")
	}
	result, e := r.Run(context.Background(), target)
	if e == nil || result.State != "submission_unknown" {
		t.Fatalf("restart: %#v %v", result, e)
	}
	target.StoreID = "R002"
	r.config.StoreIDs = append(r.config.StoreIDs, "R002")
	result, e = r.Run(context.Background(), target)
	if e == nil || result.State == "created" {
		t.Fatal("accepted stale other-target receipt")
	}
}
func TestBagNoMutation(t *testing.T) {
	_, target := testConfig(t)
	for _, m := range []Model{{}, {"shoppingCart": Model{"items": Model{"c": []any{"one", "two"}}}}, {"shoppingCart": Model{"items": Model{"c": []any{"one"}, "one": Model{"d": Model{"sku": "wrong"}}}}}} {
		if _, e := validateBag(m, target); e == nil {
			t.Fatal("accepted invalid bag")
		}
	}
}

func TestUnknownPatchAndTimeoutStop(t *testing.T) {
	for _, kind := range []string{"unknown_patch", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "timeout" {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"head":{"status":"200"},"body":{"meta":{"unknown":"private"},"checkout":{"c":["review"]}}}`))
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true, model: Model{"checkout": Model{"review": actionNode("/shop/checkout/test")}}}
			ctx, cancel := context.WithTimeout(context.Background(), 20*1000000)
			defer cancel()
			e := s.action(ctx, "checkout.review", "next", nil)
			if e == nil || strings.Contains(e.Error(), "private") {
				t.Fatal("unsupported response accepted", e)
			}
		})
	}
}
func TestCookieScopeAndProductionOrigins(t *testing.T) {
	s := Session{UserAgent: "fixture", Cookies: []Cookie{{Name: "x", Value: "secret", Domain: "example.test", Path: "/", Secure: true}}}
	if _, e := sessionClient(s); e == nil {
		t.Fatal("external cookie accepted")
	}
	for _, raw := range []string{"https://www.apple.com.cn.evil.test/", "http://www.apple.com.cn/", "https://www.apple.com.cn:444/", "https://user:secret@www.apple.com.cn/"} {
		u, _ := url.Parse(raw)
		if appleURL(u) {
			t.Fatal("bad origin", raw)
		}
	}
}

func TestCurrentCheckoutHostsAndHostOnlyCookies(t *testing.T) {
	for _, h := range []string{"www.apple.com.cn", "secure.www.apple.com.cn", "secure11.www.apple.com.cn", "secure123.www.apple.com.cn", "secure.store.apple.com.cn"} {
		u, _ := url.Parse("https://" + h + "/shop/checkout")
		if !appleURL(u) {
			t.Fatal("genuine host refused", h)
		}
	}
	for _, h := range []string{"secure11.www.apple.com.cn.evil.test", "evil.secure11.www.apple.com.cn", "securex.www.apple.com.cn", "secure11.apple.com.cn", "apple.com.cn"} {
		u, _ := url.Parse("https://" + h + "/")
		if appleURL(u) {
			t.Fatal("unknown host accepted", h)
		}
	}
	client, e := sessionClient(Session{UserAgent: "fixture", Cookies: []Cookie{{Name: "hostonly", Value: "private", Domain: "www.apple.com.cn", Path: "/", Secure: true}, {Name: "shared", Value: "private", Domain: ".apple.com.cn", Path: "/", Secure: true}, {Name: "secureonly", Value: "private", Domain: "secure11.www.apple.com.cn", Path: "/", Secure: true}}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		host string
		want map[string]bool
	}{{"www.apple.com.cn", map[string]bool{"hostonly": true, "shared": true}}, {"secure11.www.apple.com.cn", map[string]bool{"secureonly": true, "shared": true}}, {"secure12.www.apple.com.cn", map[string]bool{"shared": true}}} {
		u, _ := url.Parse("https://" + tc.host + "/")
		got := client.Jar.Cookies(u)
		if len(got) != len(tc.want) {
			t.Fatalf("cookie isolation failed for %s", tc.host)
		}
		for _, c := range got {
			if !tc.want[c.Name] {
				t.Fatal("host-only cookie leaked")
			}
		}
	}
}

func TestEmptyCookieValueIsPreserved(t *testing.T) {
	session := Session{UserAgent: "fixture", Cookies: []Cookie{{Name: "ac_ss", Domain: "www.apple.com.cn", Path: "/", Secure: true, HTTPOnly: true}}}
	client, e := sessionClient(session)
	if e != nil {
		t.Fatal("valid empty cookie rejected", e)
	}
	req, _ := http.NewRequest("GET", "https://www.apple.com.cn/shop/bag", nil)
	for _, c := range client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	if req.Header.Get("Cookie") != "ac_ss=" {
		t.Fatal("empty-valued cookie not preserved")
	}
	if _, e = sessionClient(Session{UserAgent: "fixture"}); e == nil {
		t.Fatal("empty cookie jar accepted")
	}
	session.Cookies[0].Expires = 1
	if _, e = sessionClient(session); e == nil {
		t.Fatal("expired empty jar accepted")
	}
	for _, cookie := range []Cookie{{Domain: "www.apple.com.cn", Path: "/", Secure: true}, {Name: "ac_ss", Value: "invalid\nvalue", Domain: "www.apple.com.cn", Path: "/", Secure: true}, {Name: "ac_ss", Domain: "www.apple.com.cn", Path: "/", Secure: false}} {
		session.Cookies = []Cookie{cookie}
		if _, e = sessionClient(session); e == nil {
			t.Fatal("invalid exported cookie accepted")
		}
	}
}
