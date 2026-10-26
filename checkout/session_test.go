package checkout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Only the bag checkout action path and header names are grounded in the user's
// read-only bag-evidence.json. Values and every downstream response are synthetic.
func TestObservedBagCheckoutActionAndHeaders(t *testing.T) {
	calls := 0
	sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/shop/bagx/checkout_now" {
			t.Error("wrong action")
		}
		for _, h := range []string{"x-aos-model-page", "modelVersion", "x-aos-stk", "syntax"} {
			if r.Header.Get(h) != "fixture" {
				t.Error("missing model header", h)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"head":{"status":"200"},"body":{"meta":{"l":["shoppingCart"]},"shoppingCart":{"c":[]}}}`))
	}))
	defer sv.Close()
	u, _ := url.Parse(sv.URL)
	m := Model{"meta": Model{"h": Model{"x-aos-model-page": "fixture", "modelVersion": "fixture", "x-aos-stk": "fixture", "syntax": "fixture"}}, "shoppingCart": Model{"actions": Model{"a": Model{"checkout": Model{"url": "/shop/bagx/checkout_now?_a=fixture&_m=fixture", "submit": true}}}}}
	s := wireSession{client: sv.Client(), base: u, model: m, local: true}
	if e := s.action(context.Background(), "shoppingCart.actions", "checkout", nil); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("wrong request count")
	}
}
func TestObservedBagActionDoesNotBroadenOtherStages(t *testing.T) {
	for _, tc := range []struct {
		path, name, raw string
		submit          bool
	}{{"checkout.review", "next", "/shop/bagx/checkout_now?_a=f&_m=f", true}, {"shoppingCart.actions", "delete", "/shop/bagx/checkout_now?_a=f&_m=f", true}, {"shoppingCart.actions", "checkout", "/shop/bagx/checkout_now?_a=f&_m=f", false}, {"shoppingCart.actions", "checkout", "/shop/bagx/checkout_now?_a=f", true}, {"shoppingCart.actions", "checkout", "/shop/bagx/checkout_now?_a=f&_m=f&extra=f", true}, {"shoppingCart.actions", "checkout", "/shop/bagx/checkout_now_extra?_a=f&_m=f", true}} {
		t.Run(tc.path+tc.name+tc.raw, func(t *testing.T) {
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe action reached server") }))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			m := Model{}
			setProperty(m, tc.path, Model{"a": Model{tc.name: Model{"url": tc.raw, "submit": tc.submit}}}, true)
			s := wireSession{client: sv.Client(), base: u, model: m, local: true}
			if e := s.action(context.Background(), tc.path, tc.name, nil); e == nil {
				t.Fatal("unsafe action accepted")
			}
		})
	}
}
func TestSyntaxHeaderInjectionRefused(t *testing.T) {
	sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid header sent") }))
	defer sv.Close()
	u, _ := url.Parse(sv.URL)
	s := wireSession{client: sv.Client(), base: u, local: true, model: Model{"meta": Model{"h": Model{"syntax": "fixture\r\nsecret"}}}}
	if _, _, e := s.request(context.Background(), "POST", "/shop/checkout", nil, true); e == nil {
		t.Fatal("header injection accepted")
	}
}

func TestNativeAddRedirectRejectsUnknownDestinations(t *testing.T) {
	for _, location := range []string{"https://example.test/shop/bag", "/shop/bag?add-to-cart=true", "/shop/buy-iphone/iphone-duo", "/shop/checkout/payment", "/unknown", ""} {
		t.Run(location, func(t *testing.T) {
			calls := 0
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", location)
				w.WriteHeader(302)
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true}
			if _, _, e := s.requestCore(context.Background(), "GET", "/shop/buy-iphone/iphone-duo", nil, false, true); e == nil {
				t.Fatal("unknown native destination accepted")
			}
			if calls != 1 {
				t.Fatal("mutating request replayed")
			}
		})
	}
}

func TestNativeAddProductionOriginBoundary(t *testing.T) {
	source, _ := url.Parse("https://www.apple.com.cn/shop/buy-iphone/iphone-duo")
	s := wireSession{base: source} // Production origin checks, not the local-test branch.
	for _, raw := range []string{"https://secure11.www.apple.com.cn/shop/bag", "https://secure.store.apple.com.cn/shop/bag", "https://www.apple.com.cn:443/shop/bag"} {
		destination, e := s.resolve(raw)
		if e != nil {
			t.Fatalf("fixture not Apple-allowed: %v", e)
		}
		if safeNativeAddRedirect(source, destination) {
			t.Fatal("native mutation followed across host boundary")
		}
	}
	same, e := s.resolve("/shop/bag")
	if e != nil || !safeNativeAddRedirect(source, same) {
		t.Fatal("same-origin safe bag read refused")
	}
	changedScheme, _ := url.Parse("http://www.apple.com.cn/shop/bag")
	if safeNativeAddRedirect(source, changedScheme) {
		t.Fatal("scheme change accepted")
	}
}

// The redirect shape and bootstrap paths are observed; tokens/models are synthetic.
func TestCheckoutBootstrapPostRedirect(t *testing.T) {
	for _, status := range []int{302, 303} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls []string
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch r.URL.Path {
				case "/shop/checkoutx/guest":
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"head":{"status":302,"data":{"method":"post","args":{"pltn":"fixture"},"url":"/shop/checkout/start"}}}`))
				case "/shop/checkout/start":
					r.ParseForm()
					if r.Method != "POST" || len(r.PostForm) != 1 || r.PostForm.Get("pltn") != "fixture" {
						t.Error("bootstrap form lost or changed")
					}
					w.Header().Set("Location", "/shop/checkout?fixture=1")
					w.WriteHeader(status)
				case "/shop/checkout":
					if r.Method != "GET" || r.URL.Query().Get("fixture") != "1" {
						t.Error("wrong checkout navigation")
					}
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte(`<script id="init_data">{"checkout":{"c":[]}}</script>`))
				default:
					t.Error("unexpected request")
				}
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true, model: Model{"checkout": Model{"a": Model{"guest": Model{"url": "/shop/checkoutx/guest"}}}}}
			if err := s.action(context.Background(), "checkout", "guest", nil); err != nil {
				t.Fatal(err)
			}
			if len(calls) != 3 || calls[1] != "POST /shop/checkout/start" || s.base.RawQuery != "fixture=1" {
				t.Fatalf("wrong bootstrap sequence: %v", calls)
			}
		})
	}
}

func TestCheckoutBootstrapRejectsUnobservedRedirectData(t *testing.T) {
	cases := []struct {
		name string
		data Model
	}{
		{"unknown method", Model{"method": "put"}},
		{"nonstring method", Model{"method": true}},
		{"GET arguments", Model{"method": "get", "args": Model{"pltn": "fixture"}}},
		{"legacy arguments", Model{"args": Model{"pltn": "fixture"}}},
		{"missing token", Model{"method": "post", "args": Model{}}},
		{"empty token", Model{"method": "post", "args": Model{"pltn": ""}}},
		{"wrong token type", Model{"method": "post", "args": Model{"pltn": 123}}},
		{"extra arguments", Model{"method": "post", "args": Model{"pltn": "fixture", "extra": "no"}}},
		{"wrong path", Model{"method": "post", "url": "/shop/checkout/review", "args": Model{"pltn": "fixture"}}},
		{"query", Model{"method": "post", "url": "/shop/checkout/start?extra=1", "args": Model{"pltn": "fixture"}}},
		{"fragment", Model{"method": "post", "url": "/shop/checkout/start#extra", "args": Model{"pltn": "fixture"}}},
		{"userinfo", Model{"method": "post", "url": "https://user@secure7.www.apple.com.cn/shop/checkout/start", "args": Model{"pltn": "fixture"}}},
		{"cross origin", Model{"method": "post", "url": "https://secure7.www.apple.com.cn/shop/checkout/start", "args": Model{"pltn": "fixture"}}},
		{"signin", Model{"method": "post", "url": "/shop/signIn", "args": Model{"pltn": "fixture"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("refused redirect reached network")
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true}
			raw := "/shop/checkout/start"
			if v, ok := tc.data["url"].(string); ok {
				raw = v
			}
			if err := s.followActionRedirect(context.Background(), raw, tc.data); err == nil {
				t.Fatal("unsupported redirect accepted")
			}
		})
	}
}

func TestCheckoutBootstrapNavigationGuard(t *testing.T) {
	for _, tc := range []struct {
		name, location string
		status         int
	}{
		{"preserve POST", "/shop/checkout", 307},
		{"permanent POST", "/shop/checkout", 308},
		{"unexpected success", "/shop/checkout", 200},
		{"external", "https://example.test/shop/checkout", 302},
		{"other Apple host", "https://secure8.www.apple.com.cn/shop/checkout", 302},
		{"userinfo", "https://user@secure7.www.apple.com.cn/shop/checkout", 302},
		{"fragment", "/shop/checkout#fragment", 302},
		{"wrong path", "/shop/checkout/review", 302},
		{"missing location", "", 302},
		{"post loop", "/shop/checkout/start", 302},
		{"GET redirect loop", "/shop/checkout", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls > 2 || (calls == 2 && r.Method != "GET") {
					t.Error("request replayed")
				}
				w.Header().Set("Location", tc.location)
				w.WriteHeader(tc.status)
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			s := wireSession{client: sv.Client(), base: u, local: true}
			err := s.followActionRedirect(context.Background(), "/shop/checkout/start", Model{"method": "post", "args": Model{"pltn": "fixture"}})
			if err == nil {
				t.Fatal("unsafe navigation accepted")
			}
			want := 1
			if tc.name == "GET redirect loop" {
				want = 2
			}
			if calls != want {
				t.Fatalf("got %d requests, want %d", calls, want)
			}
		})
	}
}

func TestCheckoutLegacyGetRedirect(t *testing.T) {
	for _, data := range []Model{{}, {"method": "get"}} {
		calls := 0
		sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != "GET" {
				t.Error("legacy redirect changed method")
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<script id="init_data">{"checkout":{"c":[]}}</script>`))
		}))
		u, _ := url.Parse(sv.URL)
		s := wireSession{client: sv.Client(), base: u, local: true}
		if err := s.followActionRedirect(context.Background(), "/shop/checkout", data); err != nil || calls != 1 {
			t.Fatalf("legacy redirect failed: %v (%d)", err, calls)
		}
		sv.Close()
	}
}

type bootstrapTripwireTransport struct{ t *testing.T }

func (rt bootstrapTripwireTransport) RoundTrip(*http.Request) (*http.Response, error) {
	rt.t.Error("unsafe production bootstrap reached transport")
	return nil, context.Canceled
}

func TestCheckoutBootstrapProductionOriginBoundary(t *testing.T) {
	for _, tc := range []struct{ base, destination string }{
		{"https://www.apple.com.cn/shop/bag", "/shop/checkout/start"},
		{"https://secure7.www.apple.com.cn/shop/checkout", "https://secure8.www.apple.com.cn/shop/checkout/start"},
		{"https://secure7.www.apple.com.cn/shop/checkout", "https://secure7.www.apple.com.cn:443/shop/checkout/start"},
		{"https://secure7.www.apple.com.cn/shop/checkout", "http://secure7.www.apple.com.cn/shop/checkout/start"},
		{"https://secure7.www.apple.com.cn/shop/checkout", "https://example.test/shop/checkout/start"},
		{"https://secure7.www.apple.com.cn/shop/checkout", "/shop/signIn"},
	} {
		t.Run(tc.base+tc.destination, func(t *testing.T) {
			u, _ := url.Parse(tc.base)
			s := wireSession{base: u, client: &http.Client{Transport: bootstrapTripwireTransport{t}}}
			if err := s.followActionRedirect(context.Background(), tc.destination, Model{"method": "post", "args": Model{"pltn": "fixture"}}); err == nil {
				t.Fatal("unsafe production bootstrap accepted")
			}
		})
	}
}
