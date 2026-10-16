package services

import (
	"fmt"
	"net/url"
	"strings"

	"apple-store-helper/model"
)

const (
	OpenBag       = "bag"
	OpenProduct   = "product"
	OpenFavorites = "favorites"
)

// PurchaseOptions controls the same destination for browser alerts and phone pushes.
// Empty/unknown modes retain the previous shopping-bag behavior.
type PurchaseOptions struct {
	Mode         string `json:"mode,omitempty"`
	FavoritesURL string `json:"favorites_url,omitempty"`
}

func (o PurchaseOptions) Normalized() PurchaseOptions {
	switch o.Mode {
	case OpenProduct, OpenFavorites:
	default:
		o.Mode = OpenBag
	}
	return o
}

// ValidateFavoritesURL accepts only HTTPS links on Apple's own store hosts.
// An optional override allows a user to retain their own official saved-items link.
func ValidateFavoritesURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" ||
		(u.Hostname() != "www.apple.com" && u.Hostname() != "www.apple.com.cn") {
		return fmt.Errorf("请填写 Apple 官网的 HTTPS 收藏页地址（www.apple.com 或 www.apple.com.cn）")
	}
	return nil
}

func (o PurchaseOptions) Validate() error {
	if o.Normalized().Mode == OpenFavorites && strings.TrimSpace(o.FavoritesURL) != "" {
		return ValidateFavoritesURL(o.FavoritesURL)
	}
	return nil
}

// ProductURL is a public purchase URL, never the diagnostic ASH_SHOP_ORIGIN.
func ProductURL(area model.Area, product model.Product) string {
	if product.Code == "" || product.Type == "" {
		return area.ShopOrigin() + "/shop/buy-iphone"
	}
	return fmt.Sprintf("%s/shop/buy-iphone/%s/%s", area.ShopOrigin(), familyPath(product.Type), strings.ToLower(product.Code))
}

func (o PurchaseOptions) URL(item ListenItem) string {
	switch o.Normalized().Mode {
	case OpenProduct:
		return ProductURL(item.Area, item.Product)
	case OpenFavorites:
		if strings.TrimSpace(o.FavoritesURL) == "" {
			return item.Area.ShopOrigin() + "/shop/yoursaves"
		}
		if ValidateFavoritesURL(o.FavoritesURL) == nil {
			return strings.TrimSpace(o.FavoritesURL)
		}
		// Invalid old/manual configuration must not open an arbitrary URL.
		return ProductURL(item.Area, item.Product)
	default:
		return BagURL(item.Area)
	}
}

// PurchaseLinks deduplicates identical destinations across stores, preserving hit order.
func PurchaseLinks(hits []ListenItem, options PurchaseOptions) []string {
	seen := map[string]bool{}
	var links []string
	for _, hit := range hits {
		link := options.URL(hit)
		if !seen[link] {
			seen[link] = true
			links = append(links, link)
		}
	}
	return links
}

func (s *listenService) SetPurchaseOptions(options PurchaseOptions) {
	s.mu.Lock()
	s.purchaseOptions = options.Normalized()
	s.mu.Unlock()
}

func (s *listenService) PurchaseOptions() PurchaseOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.purchaseOptions.Normalized()
}

func purchasePush(hits []ListenItem, options PurchaseOptions) (content, link string) {
	_, msg := HitMessage(hits)
	links := PurchaseLinks(hits, options)
	if len(links) == 0 {
		return msg, ""
	}
	// Include every destination in text too, for channels without clickable URL fields.
	return msg + "\n购买入口：\n" + strings.Join(links, "\n"), links[0]
}
