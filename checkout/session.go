package checkout

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type wireSession struct {
	client *http.Client
	base   *url.URL
	model  Model
	ua     string
	local  bool
}

func (s *wireSession) resolve(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, problem("invalid action URL")
	}
	u = s.base.ResolveReference(u)
	if (!s.local && !appleURL(u)) || (s.local && (u.Scheme != s.base.Scheme || u.Host != s.base.Host)) || u.User != nil {
		return nil, problem("action origin refused")
	}
	if strings.Contains(strings.ToLower(u.Path), "signin") {
		return nil, problem("Apple Store authentication required")
	}
	return u, nil
}
func (s *wireSession) request(ctx context.Context, method, raw string, fields url.Values, modelHeaders bool) ([]byte, string, error) {
	return s.requestCore(ctx, method, raw, fields, modelHeaders, false)
}
func (s *wireSession) requestCore(ctx context.Context, method, raw string, fields url.Values, modelHeaders, nativeAdd bool) ([]byte, string, error) {
	u, e := s.resolve(raw)
	if e != nil {
		return nil, "", e
	}
	var body io.Reader
	if method == "GET" {
		q := u.Query()
		for k, v := range fields {
			q[k] = v
		}
		u.RawQuery = q.Encode()
	} else {
		body = strings.NewReader(fields.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return nil, "", problem("request creation failed")
	}
	req.Header.Set("User-Agent", s.ua)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", u.Scheme+"://"+u.Host)
		req.Header.Set("Referer", s.base.String())
		req.Header.Set("X-Requested-With", "Fetch")
	}
	if modelHeaders && s.model != nil {
		if m, e := Node(s.model, "meta.h"); e == nil {
			for k, v := range m {
				lk := strings.ToLower(k)
				if lk != "x-aos-stk" && lk != "x-aos-model-page" && lk != "x-aos-ui-fetch-call-1" && lk != "modelversion" && lk != "syntax" {
					return nil, "", problem("unsupported model header")
				}
				str, ok := v.(string)
				if !ok || strings.ContainsAny(str, "\r\n") {
					return nil, "", problem("invalid model header")
				}
				req.Header.Set(k, str)
			}
		}
	}
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(req)
	if e != nil {
		return nil, "", problem("request failed or cancelled")
	}
	defer resp.Body.Close()
	if nativeAdd && (resp.StatusCode == 302 || resp.StatusCode == 303) {
		destination, e := s.resolve(resp.Header.Get("Location"))
		if e != nil || resp.Header.Get("Location") == "" || !safeNativeAddRedirect(u, destination) {
			return nil, "", problem("native add redirect refused")
		}
		// One mutation only. The read uses no copied form fields and redirects stop.
		return s.request(ctx, "GET", destination.String(), nil, false)
	}
	if resp.StatusCode != 200 {
		return nil, "", problem(fmt.Sprintf("HTTP response refused (status %d)", resp.StatusCode))
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if e != nil || len(b) > 4<<20 {
		return nil, "", problem("response size or read failed")
	}
	return b, resp.Header.Get("Content-Type"), nil
}

var initRE = regexp.MustCompile(`(?s)<script\b[^>]*\bid=["']init_data["'][^>]*>(.*?)</script>`)

func (s *wireSession) load(ctx context.Context, raw string) error {
	b, ct, e := s.request(ctx, "GET", raw, nil, false)
	if e != nil {
		return e
	}
	if !strings.Contains(ct, "text/html") {
		return problem("expected checkout HTML")
	}
	matches := initRE.FindAllSubmatch(b, -1)
	if len(matches) != 1 {
		return problem("checkout model unavailable")
	}
	m, e := decodeObject(matches[0][1])
	if e != nil {
		return e
	}
	if _, ok := m["signIn"]; ok {
		return problem("login required")
	}
	s.model = m
	u, e := s.resolve(raw)
	if e != nil {
		return e
	}
	s.base = u
	return s.updateBase()
}
func (s *wireSession) updateBase() error {
	if page, e := Node(s.model, "meta.page"); e == nil {
		if raw, ok := page["htmlUrl"].(string); ok {
			u, e := s.resolve(raw)
			if e != nil {
				return e
			}
			s.base = u
		}
	}
	return nil
}
func (s *wireSession) action(ctx context.Context, path, name string, overrides url.Values) error {
	n, e := Node(s.model, path)
	if e != nil {
		return problem("missing action node")
	}
	a, e := Node(Model(n), "a."+name)
	if e != nil {
		return problem("missing action")
	}
	raw, ok := a["url"].(string)
	if !ok || raw == "" {
		return problem("missing action URL")
	}
	if method, exists := a["method"]; exists && method != "POST" && method != "post" {
		return problem("unsupported action method")
	}
	actionURL, e := s.resolve(raw)
	if e != nil {
		return e
	}
	bagCheckout := false
	if actionURL.Path == "/shop/bagx/checkout_now" && path == "shoppingCart.actions" && name == "checkout" && a["submit"] == true {
		q := actionURL.Query()
		bagCheckout = len(q) == 2 && len(q["_a"]) == 1 && len(q["_m"]) == 1 && q.Get("_a") != "" && q.Get("_m") != ""
		if !s.local && actionURL.Hostname() != "www.apple.com.cn" {
			bagCheckout = false
		}
	}
	if !strings.HasPrefix(actionURL.Path, "/shop/checkout") && actionURL.Path != "/shop/bag" && !bagCheckout {
		return problem("action path refused")
	}
	scope := path
	if v, ok := a["module"].(string); ok {
		scope = strings.TrimPrefix(v, "/")
		if v == "^" {
			i := strings.LastIndex(path, ".")
			if i < 0 {
				return problem("invalid action scope")
			}
			scope = path[:i]
		}
	}
	fields := url.Values{}
	if a["submit"] == true {
		fields, e = SubmitFields(s.model, scope)
		if e != nil {
			return problem("unsupported submit fields")
		}
	}
	for k, v := range overrides {
		fields[k] = v
	}
	b, ct, e := s.request(ctx, "POST", raw, fields, true)
	if e != nil {
		return e
	}
	if !strings.Contains(ct, "application/json") {
		return problem("expected action JSON")
	}
	payload, e := decodeObject(b)
	if e != nil {
		return e
	}
	head, e := Node(payload, "head")
	if e != nil {
		return problem("missing application status")
	}
	status := fmt.Sprint(head["status"])
	if status == "302" || status == "303" {
		data, e := Node(head, "data")
		if e != nil {
			return problem("invalid action redirect")
		}
		raw, ok := data["url"].(string)
		if !ok {
			return problem("invalid action redirect")
		}
		return s.followActionRedirect(ctx, raw, data)
	}
	if status != "200" {
		return problem("application response refused")
	}
	body, e := Node(payload, "body")
	if e != nil {
		return problem("missing model response")
	}
	s.model, e = ApplyResponse(s.model, body)
	if e != nil {
		return problem("unsupported response model")
	}
	return s.updateBase()
}
func (s *wireSession) binding(ctx context.Context, path string) error {
	name, e := BindingAction(s.model, path, "continue")
	if e != nil {
		return problem("unsupported continuation binding")
	}
	return s.action(ctx, path, name, nil)
}

// A native add can cross no origin boundary, including between Apple hosts.
func safeNativeAddRedirect(source, destination *url.URL) bool {
	return source != nil && destination != nil && destination.Scheme == source.Scheme && destination.Host == source.Host && destination.User == nil && destination.Path == "/shop/bag" && destination.RawQuery == "" && destination.Fragment == ""
}

// Only the observed checkout bootstrap may turn an application redirect into a
// POST. The token is sent once, and never copied to the subsequent navigation.
func (s *wireSession) followActionRedirect(ctx context.Context, raw string, data Model) error {
	method := "get"
	if v, exists := data["method"]; exists {
		var ok bool
		method, ok = v.(string)
		if !ok {
			return problem("invalid redirect method")
		}
		method = strings.ToLower(method)
	}
	if method == "get" {
		if _, exists := data["args"]; exists {
			return problem("GET redirect arguments refused")
		}
		return s.load(ctx, raw)
	}
	if method != "post" {
		return problem("unsupported redirect method")
	}
	u, err := s.resolve(raw)
	if err != nil {
		return err
	}
	if u.Scheme != s.base.Scheme || u.Host != s.base.Host || u.Path != "/shop/checkout/start" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || (!s.local && u.Hostname() == "www.apple.com.cn") {
		return problem("checkout bootstrap origin or path refused")
	}
	args, err := Node(data, "args")
	if err != nil || len(args) != 1 {
		return problem("checkout bootstrap arguments refused")
	}
	token, ok := args["pltn"].(string)
	if !ok || strings.TrimSpace(token) == "" {
		return problem("checkout bootstrap token refused")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(url.Values{"pltn": {token}}.Encode()))
	if err != nil {
		return problem("checkout bootstrap request failed")
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	req.Header.Set("Referer", s.base.String())
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return problem("checkout bootstrap request failed or cancelled")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		return problem("checkout bootstrap response refused")
	}
	location := resp.Header.Get("Location")
	ref, err := url.Parse(location)
	if err != nil || location == "" {
		return problem("checkout bootstrap redirect refused")
	}
	destination := u.ResolveReference(ref)
	if destination.Scheme != u.Scheme || destination.Host != u.Host || destination.User != nil || destination.Path != "/shop/checkout" || destination.RawPath != "" || destination.Fragment != "" {
		return problem("checkout bootstrap redirect refused")
	}
	// load refuses further HTTP redirects, so neither POST nor navigation loops.
	return s.load(ctx, destination.String())
}
