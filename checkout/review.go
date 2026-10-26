package checkout

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var moneyPattern = regexp.MustCompile(`^RMB\s+(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)(?:\.[0-9]{1,2})?$`)

// ParseMoney accepts only explicit positive RMB amounts, without floating point.
func ParseMoney(text string) (int64, error) {
	if !moneyPattern.MatchString(text) {
		return 0, fmt.Errorf("unrecognized RMB amount")
	}
	s := strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(text, "RMB")), ",", "")
	whole, fraction, _ := strings.Cut(s, ".")
	if len(fraction) == 1 {
		fraction += "0"
	}
	if fraction == "" {
		fraction = "00"
	}
	n, e := strconv.ParseInt(whole, 10, 64)
	if e != nil || n > (math.MaxInt64-99)/100 {
		return 0, fmt.Errorf("amount overflow")
	}
	c, _ := strconv.ParseInt(fraction, 10, 64)
	n = n*100 + c
	if n <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}
	return n, nil
}

var tags = regexp.MustCompile(`<[^>]*>`)
var unavailable = regexp.MustCompile(`(?i)无货|售罄|不可取货|无法取货|暂无现货|不可用|暂不提供|不支持取货|unavailable|not\s+available|ineligible`)

func reviewText(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("review field is not text")
	}
	return strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, " "))), " "), nil
}
func children(n Model) ([]Model, error) {
	names, e := stringList(n["c"])
	if e != nil {
		return nil, e
	}
	r := []Model{}
	for _, name := range names {
		v, ok := object(n[name])
		if !ok {
			return nil, fmt.Errorf("missing review child %s", name)
		}
		p, _ := object(v["p"])
		if a, exists := p["active"]; exists && a != true {
			return nil, fmt.Errorf("inactive or unsupported review child")
		}
		r = append(r, v)
	}
	return r, nil
}
func listed(n Model, key string) bool {
	names, e := stringList(n["c"])
	if e != nil {
		return false
	}
	for _, name := range names {
		if name == key {
			return true
		}
	}
	return false
}
func reviewData(n Model) (Model, error) {
	d, ok := object(n["d"])
	if !ok {
		return nil, fmt.Errorf("missing review data")
	}
	return d, nil
}
func exactOne(v any) bool {
	switch x := v.(type) {
	case int:
		return x == 1
	case int64:
		return x == 1
	case float64:
		return x == 1
	case json.Number:
		return string(x) == "1"
	}
	return false
}
func validateQuantities(item Model) error {
	count := 0
	d, e := reviewData(item)
	if e != nil {
		return e
	}
	for _, key := range []string{"originalQty", "quantity", "qty", "currentQty"} {
		if v, exists := d[key]; exists {
			count++
			if !exactOne(v) {
				return fmt.Errorf("conflicting or invalid item quantity")
			}
		}
	}
	if q, exists := item["itemQuantity"]; exists {
		qd, ok := lookup(q, "d")
		m, yes := object(qd)
		if !ok || !yes {
			return fmt.Errorf("invalid item quantity module")
		}
		v, exists := m["quantity"]
		if !exists || !exactOne(v) {
			return fmt.Errorf("invalid current item quantity")
		}
		count++
		for _, key := range []string{"originalQty", "qty", "currentQty"} {
			if v, exists := m[key]; exists && !exactOne(v) {
				return fmt.Errorf("conflicting quantity module field")
			}
		}
	}
	if count == 0 {
		return fmt.Errorf("missing item quantity")
	}
	return nil
}
func reviewItems(group Model) ([]Model, error) {
	items, e := children(group)
	if e != nil {
		return nil, e
	}
	pending := append([]Model{}, items...)
	for steps := 0; len(pending) > 0; steps++ {
		if steps > 256 {
			return nil, fmt.Errorf("review item tree too deep")
		}
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, exists := item["c"]; !exists {
			continue
		}
		cs, e := children(item)
		if e != nil {
			return nil, e
		}
		for _, c := range cs {
			pending = append(pending, c)
			d, _ := object(c["d"])
			_, product := d["productId"]
			if product || truth(d["isChildItem"]) || truth(d["isBundlePromoItem"]) {
				items = append(items, c)
			}
		}
	}
	return items, nil
}
func paymentNodes(n Model, depth int) ([]Model, error) {
	if depth > 32 {
		return nil, fmt.Errorf("payment layouts too deep")
	}
	d, e := reviewData(n)
	if e != nil {
		return nil, e
	}
	rows, ok := d["layouts"].([]any)
	if !ok || len(rows) == 0 {
		return nil, fmt.Errorf("missing selected payment layout")
	}
	r := []Model{}
	seen := map[string]bool{}
	for _, row := range rows {
		names, e := stringList(row)
		if e != nil || len(names) == 0 {
			return nil, fmt.Errorf("invalid payment layout")
		}
		for _, name := range names {
			if seen[name] {
				return nil, fmt.Errorf("duplicate selected payment")
			}
			seen[name] = true
			c, ok := object(n[name])
			if !ok {
				return nil, fmt.Errorf("missing selected payment")
			}
			if name == "address" || name == "vatID" {
				continue
			}
			cd, e := reviewData(c)
			if e != nil {
				return nil, e
			}
			if _, exists := cd["layouts"]; exists {
				nested, e := paymentNodes(c, depth+1)
				if e != nil {
					return nil, e
				}
				r = append(r, nested...)
			} else {
				r = append(r, cd)
			}
		}
	}
	return r, nil
}

// Contact is accepted only from the active fulfillment review pickup data, never
// from a stale checkout.pickupContact form. These schemas are synthetic contracts;
// an unknown live schema must be captured and reviewed rather than guessed.
func validateContact(pickup Model, expected Contact) error {
	want := map[string]string{"firstName": expected.FirstName, "lastName": expected.LastName, "emailAddress": expected.Email, "mobilePhone": expected.Phone}
	for _, s := range want {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("mandatory expected pickup contact missing")
		}
	}
	candidates := []Model{pickup}
	for _, key := range []string{"contact", "pickupContact"} {
		if v, exists := pickup[key]; exists {
			m, ok := object(v)
			if !ok {
				return fmt.Errorf("unrecognized pickup contact schema")
			}
			candidates = append(candidates, m)
		}
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		for key, w := range want {
			aliases := []string{key}
			if key == "emailAddress" {
				aliases = append(aliases, "email")
			}
			if key == "mobilePhone" {
				aliases = append(aliases, "phone", "phoneNumber")
			}
			for _, alias := range aliases {
				if v, exists := c[alias]; exists {
					got, e := reviewText(v)
					if e != nil || got != w {
						return fmt.Errorf("pickup contact mismatch for %s", key)
					}
					seen[key] = true
				}
			}
		}
	}
	if len(seen) != 4 {
		return fmt.Errorf("active review contact cannot be verified")
	}
	return nil
}

// ValidateReview fails closed before any order submission. No live acceptance is
// implied by the source-derived schema or its synthetic fixtures.
func ValidateReview(m Model, target Target) (int64, error) {
	if target.SKU == "" || target.ProductName == "" || target.StoreID == "" || target.StoreName == "" || target.MaxTotalCents <= 0 {
		return 0, fmt.Errorf("incomplete review target")
	}
	if target.Payment != "WECHAT" && target.Payment != "ALIPAY" {
		return 0, fmt.Errorf("unsupported payment")
	}
	checkout, e := rawNode(m, "checkout")
	if e != nil || !listed(checkout, "review") {
		return 0, fmt.Errorf("not at review")
	}
	review, e := rawNode(checkout, "review")
	if e != nil || !listed(review, "fulfillmentReview") || !listed(review, "billingReview") {
		return 0, fmt.Errorf("missing review modules")
	}
	fr, e := rawNode(review, "fulfillmentReview")
	if e != nil {
		return 0, e
	}
	groups, e := children(fr)
	if e != nil || len(groups) != 1 {
		return 0, fmt.Errorf("expected one pickup group")
	}
	gd, e := reviewData(groups[0])
	if e != nil {
		return 0, e
	}
	pickup, ok := object(gd["pickup"])
	if !ok || len(pickup) == 0 || gd["delivers"] != nil {
		return 0, fmt.Errorf("not pickup fulfillment")
	}
	address, e := reviewText(pickup["storeAddressQuote"])
	store, _ := reviewText(target.StoreName)
	if e != nil || store == "" || strings.Count(address, store) != 1 {
		return 0, fmt.Errorf("pickup store mismatch")
	}
	storeIDs := 0
	for _, key := range []string{"storeNumber", "storeId"} {
		if id, exists := pickup[key]; exists {
			storeIDs++
			if id != target.StoreID {
				return 0, fmt.Errorf("pickup store ID mismatch")
			}
		}
	}
	if storeIDs == 0 {
		return 0, fmt.Errorf("missing authoritative pickup store ID")
	}
	// Only this explicit active-review field establishes the selected slot.
	// Do not infer IDs from labels or alternate unobserved schema variants.
	if target.PickupSlotID != "" {
		slot, ok := pickup["timeSlotId"].(string)
		if !ok || slot == "" || slot != target.PickupSlotID {
			return 0, fmt.Errorf("pickup review time slot missing, ambiguous or mismatched")
		}
	}
	for _, v := range pickup {
		if s, ok := v.(string); ok && unavailable.MatchString(s) {
			return 0, fmt.Errorf("pickup unavailable")
		}
	}
	if e := validateContact(pickup, target.Contact); e != nil {
		return 0, e
	}
	items, e := reviewItems(groups[0])
	if e != nil || len(items) != 1 {
		return 0, fmt.Errorf("expected exactly one product")
	}
	id, _ := lookup(items[0], "d.productId")
	name, _ := lookup(items[0], "d.name")
	text, e := reviewText(name)
	expected, _ := reviewText(target.ProductName)
	if e != nil || id != target.SKU || text != expected {
		return 0, fmt.Errorf("review product mismatch")
	}
	if e := validateQuantities(items[0]); e != nil {
		return 0, e
	}
	billing, e := rawNode(review, "billingReview")
	if e != nil {
		return 0, e
	}
	payments, e := paymentNodes(billing, 0)
	if e != nil || len(payments) != 1 {
		return 0, fmt.Errorf("expected one selected payment")
	}
	allowed := map[string]bool{target.Payment: true}
	if target.Payment == "WECHAT" {
		allowed["微信支付"] = true
	} else {
		allowed["支付宝"] = true
	}
	identities := 0
	for _, key := range []string{"paymentType", "displayPaymentType", "paymentTypeLabel", "type", "labelImageAlt"} {
		if v, exists := payments[0][key]; exists {
			value, e := reviewText(v)
			if e != nil || !allowed[value] {
				return 0, fmt.Errorf("review payment mismatch")
			}
			identities++
		}
	}
	if identities == 0 {
		return 0, fmt.Errorf("missing payment identity")
	}
	summary, e := rawNode(checkout, "companionBar.orderSummary.d")
	if e != nil {
		return 0, e
	}
	itemText, e := reviewText(summary["items"])
	if e != nil || strings.ReplaceAll(itemText, " ", "") != "1件商品" {
		return 0, fmt.Errorf("summary item count mismatch")
	}
	totalText, ok := summary["total"].(string)
	if !ok {
		return 0, fmt.Errorf("missing summary total")
	}
	total, e := ParseMoney(totalText)
	if e != nil || total > target.MaxTotalCents {
		return 0, fmt.Errorf("invalid or excessive total")
	}
	if s, exists := review["summary"]; exists {
		value, exists := lookup(s, "d.total")
		if !exists {
			return 0, fmt.Errorf("missing review summary total")
		}
		text, ok := value.(string)
		if !ok {
			return 0, fmt.Errorf("invalid review total")
		}
		other, e := ParseMoney(text)
		if e != nil || other != total {
			return 0, fmt.Errorf("review totals disagree")
		}
	} else {
		return 0, fmt.Errorf("missing review summary")
	}
	return total, nil
}
