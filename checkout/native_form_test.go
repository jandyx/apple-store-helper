package checkout

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNativeAddRejectsPlaceholderActionBeforeRequest(t *testing.T) {
	for _, action := range []string{"", "#", "#add", "?product=SKU", "/shop/buy-iphone/iphone-duo/select#add"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(200) }))
			defer server.Close()
			base, _ := url.Parse(server.URL + "/shop/buy-iphone/iphone-duo/select")
			client := server.Client()
			client.Jar, _ = cookiejar.New(nil)
			client.Jar.SetCookies(base, []*http.Cookie{{Name: "as_atb", Value: "fixture|token", Path: "/"}})
			s := wireSession{base: base, client: client, local: true}
			summary := Model{"buyNowButton": fmt.Sprintf(`<form method="get" action="%s"><input name="product" value="SKU"><button type="submit" name="add-to-cart" value="add-to-cart"></button></form>`, action)}
			if err := s.add(context.Background(), summary); err == nil {
				t.Error("placeholder action accepted")
			}
			if calls != 0 {
				t.Fatalf("placeholder caused %d network mutations", calls)
			}
		})
	}
}

func TestHTTPFailureReportsStatusWithoutResponseSecrets(t *testing.T) {
	for _, status := range []int{302, 403, 541} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "https://example.invalid/private-token")
				w.WriteHeader(status)
				fmt.Fprint(w, "secret response")
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			s := wireSession{base: base, client: server.Client(), local: true}
			_, _, err := s.request(context.Background(), "GET", "/shop/bag", nil, false)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Errorf("missing HTTP status: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), server.URL)) {
				t.Fatal("response secret exposed")
			}
			if calls != 1 {
				t.Fatal("failed request retried")
			}
		})
	}
}
