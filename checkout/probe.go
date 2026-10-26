package checkout

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

type BagItem struct {
	SKU      string `json:"sku"`
	Quantity int64  `json:"quantity"`
}

// SessionProbe proves only that a private session can read its shopping bag.
// It cannot prove store login, pickup availability or final order readiness.
type SessionProbe struct {
	Items []BagItem `json:"items"`
}

type CheckoutPreview struct {
	Phase string `json:"phase"`
}

func ProbeSession(ctx context.Context, sessionFile string) (SessionProbe, error) {
	exported, err := LoadSession(sessionFile)
	if err != nil {
		return SessionProbe{}, err
	}
	client, err := sessionClient(exported)
	if err != nil {
		return SessionProbe{}, err
	}
	base, _ := url.Parse("https://www.apple.com.cn")
	s := &wireSession{client: client, base: base, ua: exported.UserAgent}
	if err := s.load(ctx, "/shop/bag"); err != nil {
		return SessionProbe{}, err
	}
	return probeBag(s.model)
}

// PrepareCheckoutSession is an explicit protocol diagnostic, not a read-only
// check: it enters checkout for an existing exact one-item bag. It never adds,
// changes quantities, selects pickup/payment, accepts terms or submits an order.
func PrepareCheckoutSession(ctx context.Context, sessionFile, sku string, maxTotalCents int64) (CheckoutPreview, error) {
	if !supportedSKU(sku) || maxTotalCents <= 0 {
		return CheckoutPreview{}, problem("explicit supported SKU and amount cap required")
	}
	exported, err := LoadSession(sessionFile)
	if err != nil {
		return CheckoutPreview{}, err
	}
	client, err := sessionClient(exported)
	if err != nil {
		return CheckoutPreview{}, err
	}
	base, _ := url.Parse("https://www.apple.com.cn")
	s := &wireSession{client: client, base: base, ua: exported.UserAgent}
	if err := s.load(ctx, "/shop/bag"); err != nil {
		return CheckoutPreview{}, err
	}
	if empty, err := validateBag(s.model, Target{SKU: sku, MaxTotalCents: maxTotalCents}); err != nil || empty {
		return CheckoutPreview{}, problem("preview requires one existing exact phone within cap")
	}
	if err := s.action(ctx, "shoppingCart.actions", "checkout", nil); err != nil {
		return CheckoutPreview{}, err
	}
	current := phase(s.model)
	if current != "fulfillment" {
		return CheckoutPreview{}, problem("checkout authentication or unsupported initial phase")
	}
	return CheckoutPreview{Phase: current}, nil
}

func probeBag(model Model) (SessionProbe, error) {
	items, err := Node(model, "shoppingCart.items")
	if err != nil {
		return SessionProbe{}, problem("bag unavailable")
	}
	children, ok := items["c"].([]any)
	if !ok {
		return SessionProbe{}, problem("bag item structure unsupported")
	}
	result := SessionProbe{Items: []BagItem{}}
	for _, child := range children {
		name, ok := child.(string)
		if !ok {
			return SessionProbe{}, problem("bag item structure unsupported")
		}
		data, err := Node(items, name+".d")
		if err != nil {
			return SessionProbe{}, problem("bag item unavailable")
		}
		sku, ok := data["sku"].(string)
		if !ok || sku == "" {
			return SessionProbe{}, problem("bag SKU unavailable")
		}
		q, err := Node(items, name+".itemQuantity.d")
		if err != nil {
			return SessionProbe{}, problem("bag quantity unavailable")
		}
		quantity, err := strconv.ParseInt(fmt.Sprint(q["quantity"]), 10, 64)
		if err != nil || quantity < 1 {
			return SessionProbe{}, problem("bag quantity unsupported")
		}
		result.Items = append(result.Items, BagItem{SKU: sku, Quantity: quantity})
	}
	return result, nil
}
