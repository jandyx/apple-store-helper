package services

import (
	"strings"
	"testing"

	"apple-store-helper/model"
)

func TestPurchaseDestinations(t *testing.T) {
	t.Setenv("ASH_SHOP_ORIGIN", "http://127.0.0.1:9999")
	for _, area := range model.Areas {
		item := ListenItem{Area: area, Product: model.Product{Type: "iphoneduo", Code: "TESTCH/A"}}
		if got := (PurchaseOptions{}).URL(item); got != BagURL(area) {
			t.Fatalf("legacy destination: %s", got)
		}
		want := area.ShopOrigin() + "/shop/buy-iphone/iphone-duo/testch/a"
		if got := (PurchaseOptions{Mode: OpenProduct}).URL(item); got != want {
			t.Fatalf("product destination: %s, want %s", got, want)
		}
		favorites := area.ShopOrigin() + "/shop/yoursaves"
		opt := PurchaseOptions{Mode: OpenFavorites, FavoritesURL: favorites}
		if opt.Validate() != nil || opt.URL(item) != favorites {
			t.Fatalf("favorites not preserved: %+v", opt)
		}
	}
}

func TestFavoritesValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "http://www.apple.com/shop", "https://www.apple.com.evil.test/", "https://www.apple.com@evil.test/", "https://evil.test@www.apple.com/", "https://www.apple.com:443/shop"} {
		opt := PurchaseOptions{Mode: OpenFavorites, FavoritesURL: raw}
		if opt.Validate() == nil {
			t.Fatalf("accepted unsafe URL %q", raw)
		}
		item := ListenItem{Area: model.Areas[0]}
		if opt.URL(item) != ProductURL(item.Area, item.Product) {
			t.Fatal("invalid favorites should use safe product fallback")
		}
	}
}

func TestPurchaseLinksAndPush(t *testing.T) {
	a := ListenItem{Area: model.Areas[0], Product: model.Product{Type: "iphoneduo", Code: "ONE/A"}}
	b := a
	b.Product.Code = "TWO/A"
	hits := []ListenItem{a, a, b}
	opt := PurchaseOptions{Mode: OpenProduct}
	if got := PurchaseLinks(hits, opt); len(got) != 2 {
		t.Fatalf("same SKU at multiple stores should deduplicate: %v", got)
	}
	body, link := purchasePush(hits, opt)
	if link != ProductURL(a.Area, a.Product) || !strings.Contains(body, ProductURL(b.Area, b.Product)) {
		t.Fatalf("push missing SKU destinations: %s %s", body, link)
	}
	if len(PurchaseLinks(hits, PurchaseOptions{})) != 1 {
		t.Fatal("same-region bags should deduplicate")
	}
	if got := PurchaseLinks(nil, opt); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestPurchaseSettingsRoundTrip(t *testing.T) {
	useTempConfigDir(t)
	want := PurchaseOptions{Mode: OpenFavorites, FavoritesURL: "https://www.apple.com.cn/shop/yoursaves"}
	if err := SaveSettings(UserSettings{Purchase: want}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings()
	if err != nil || got.Purchase != want {
		t.Fatalf("%+v %v", got, err)
	}
	s := newListenService()
	s.SetPurchaseOptions(PurchaseOptions{Mode: "future"})
	if s.PurchaseOptions().Mode != OpenBag {
		t.Fatal("unknown mode must preserve bag behavior")
	}
}

func TestHitFreezesPurchaseOptionsBeforeCallbacks(t *testing.T) {
	fake, s := setupFakeApple(t)
	addItem(s, model.Areas[0], "R639", "A1")
	fake.inStock["R639.A1"] = true
	want := PurchaseOptions{Mode: OpenProduct}
	s.SetPurchaseOptions(want)
	s.OnChange(func() { s.SetPurchaseOptions(PurchaseOptions{Mode: OpenBag}) })
	called := false
	s.OnInStock(func(hits []ListenItem, options PurchaseOptions) {
		called = true
		if options != want {
			t.Fatalf("event lost its snapshot: %+v", options)
		}
		content, link := purchasePush(hits, options)
		if link != PurchaseLinks(hits, options)[0] || !strings.Contains(content, link) {
			t.Fatal("push and browser must share snapshot")
		}
	})
	s.tick()
	if !called {
		t.Fatal("missing hit")
	}
}
