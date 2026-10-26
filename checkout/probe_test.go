package checkout

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestSessionProbeNeverClaimsLoginOrReadiness(t *testing.T) {
	model, err := decodeObject([]byte(`{"shoppingCart":{"items":{"c":["item1"],"item1":{"d":{"sku":"MK2Q4CH/A"},"itemQuantity":{"d":{"quantity":1}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := probeBag(model)
	if err != nil || len(result.Items) != 1 || result.Items[0].SKU != "MK2Q4CH/A" || result.Items[0].Quantity != 1 {
		t.Fatal(result, err)
	}
	if _, err := probeBag(Model{"signIn": Model{}}); err == nil {
		t.Fatal("login page was accepted as a valid bag")
	}
}

func TestPrepareDiagnosticRequiresExplicitTargetBeforeSessionRead(t *testing.T) {
	for _, tc := range []struct {
		sku string
		cap int64
	}{{"", 1799900}, {"OTHER", 1799900}, {"MK2Q4CH/A", 0}} {
		if _, err := PrepareCheckoutSession(context.Background(), "/not/a/session", tc.sku, tc.cap); err == nil || !strings.Contains(err.Error(), "explicit supported SKU") {
			t.Fatal("diagnostic accepted unspecified/unsafe target", err)
		}
	}
}

func TestAuthenticationAndOriginFailuresHaveDistinctRedactedCauses(t *testing.T) {
	base, _ := url.Parse("https://www.apple.com.cn")
	s := wireSession{base: base}
	if _, err := s.resolve("https://secure11.www.apple.com.cn/shop/signIn?secret=private"); err == nil || !strings.Contains(err.Error(), "authentication required") || strings.Contains(err.Error(), "private") {
		t.Fatal("login requirement was not safely classified", err)
	}
	if _, err := s.resolve("https://unapproved.test/shop/checkout?secret=private"); err == nil || !strings.Contains(err.Error(), "origin refused") || strings.Contains(err.Error(), "private") {
		t.Fatal("unapproved origin was not safely classified", err)
	}
}
