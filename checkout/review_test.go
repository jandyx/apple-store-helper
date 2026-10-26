package checkout

import (
	"strings"
	"testing"
)

// This review is a synthetic source-derived contract, NOT a live checkout capture.
func reviewFixture(t *testing.T) (Model, Target) {
	return fixture(t, `{"checkout":{"c":["review"],"review":{"c":["fulfillmentReview","billingReview"],"fulfillmentReview":{"c":["group"],"group":{"d":{"pickup":{"storeAddressQuote":"Apple 深圳益田假日广场 广东","storeNumber":"R001","contact":{"firstName":"Jack","lastName":"Z","emailAddress":"jack@example.invalid","mobilePhone":"13000000000"}}},"c":["item"],"item":{"d":{"productId":"SKU/A","name":"Phone 512GB 黑色","originalQty":1},"itemQuantity":{"d":{"quantity":1}}}}},"billingReview":{"d":{"layouts":[["wallet"]]},"wallet":{"d":{"paymentType":"WECHAT","paymentTypeLabel":"微信支付"}}},"summary":{"d":{"total":"RMB 17,999"}}},"companionBar":{"orderSummary":{"d":{"items":"1 件商品","total":"RMB 17,999"}}}}}`), Target{SKU: "SKU/A", ProductName: "Phone 512GB 黑色", StoreID: "R001", StoreName: "Apple 深圳益田假日广场", Payment: "WECHAT", MaxTotalCents: 1800000, Contact: Contact{FirstName: "Jack", LastName: "Z", Email: "jack@example.invalid", Phone: "13000000000"}}
}
func TestParseMoney(t *testing.T) {
	for _, tc := range []struct {
		s string
		n int64
	}{{"RMB 17,999", 1799900}, {"RMB 1.5", 150}, {"RMB 0.01", 1}, {"RMB 1,234.56", 123456}} {
		n, e := ParseMoney(tc.s)
		if e != nil || n != tc.n {
			t.Fatalf("%s: %d %v", tc.s, n, e)
		}
	}
	for _, s := range []string{"RMB 0", "RMB -1", "RMB 1.001", "RMB 1,23", "¥1", "RMB 1e3", "RMB 92233720368547759"} {
		if _, e := ParseMoney(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func TestValidateReview(t *testing.T) {
	m, target := reviewFixture(t)
	total, e := ValidateReview(m, target)
	if e != nil || total != 1799900 {
		t.Fatalf("%d %v", total, e)
	}
}
func TestReviewRejectsMismatches(t *testing.T) {
	tests := []struct {
		name, path string
		value      any
	}{{"sku", "checkout.review.fulfillmentReview.group.item.d.productId", "other"}, {"product", "checkout.review.fulfillmentReview.group.item.d.name", "Other"}, {"store", "checkout.review.fulfillmentReview.group.d.pickup.storeAddressQuote", "Apple Other"}, {"storeID", "checkout.review.fulfillmentReview.group.d.pickup.storeNumber", "R002"}, {"unavailable", "checkout.review.fulfillmentReview.group.d.pickup.promise", "暂不提供取货"}, {"quantity", "checkout.review.fulfillmentReview.group.item.itemQuantity.d.quantity", float64(2)}, {"falseQuantity", "checkout.review.fulfillmentReview.group.item.d.originalQty", false}, {"zeroQuantity", "checkout.review.fulfillmentReview.group.item.d.originalQty", float64(0)}, {"stringQuantity", "checkout.review.fulfillmentReview.group.item.d.originalQty", "1"}, {"contact", "checkout.review.fulfillmentReview.group.d.pickup.contact.mobilePhone", "13000000001"}, {"missingContact", "checkout.review.fulfillmentReview.group.d.pickup.contact", Model{}}, {"payment", "checkout.review.billingReview.wallet.d.paymentType", "ALIPAY"}, {"mixedPayment", "checkout.review.billingReview.wallet.d.paymentTypeLabel", "支付宝"}, {"total", "checkout.review.summary.d.total", "RMB 1"}, {"malformedReviewTotal", "checkout.review.summary.d.total", "unknown"}, {"summaryCount", "checkout.companionBar.orderSummary.d.items", "2件商品"}, {"extraItem", "checkout.review.fulfillmentReview.group.item.c", []any{"addon"}}, {"missingLayout", "checkout.review.billingReview.d.layouts", []any{}}, {"notReview", "checkout.c", []any{"fulfillment"}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, target := reviewFixture(t)
			if e := setProperty(m, tc.path, tc.value, true); e != nil {
				t.Fatal(e)
			}
			if _, e := ValidateReview(m, target); e == nil {
				t.Fatal("unsafe review accepted")
			}
		})
	}
	m, target := reviewFixture(t)
	target.MaxTotalCents = 100
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("cap ignored")
	}
	m, target = reviewFixture(t)
	target.Contact.Phone = ""
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("empty expected contact accepted")
	}
}
func TestStaleContactDoesNotProveReview(t *testing.T) {
	m, target := reviewFixture(t)
	pickup, _ := rawNode(m, "checkout.review.fulfillmentReview.group.d.pickup")
	delete(pickup, "contact")
	checkout, _ := object(m["checkout"])
	checkout["pickupContact"] = Model{"d": Model{"firstName": "Jack", "lastName": "Z", "emailAddress": "jack@example.invalid", "mobilePhone": "13000000000"}}
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("stale non-review contact accepted")
	}
}

func TestReviewMissingAuthoritativeSchemas(t *testing.T) {
	for _, path := range []string{"checkout.review.summary", "checkout.review.fulfillmentReview.group.d.pickup.storeNumber", "checkout.review.fulfillmentReview.group.item.d.originalQty", "checkout.review.billingReview.wallet.d.paymentType"} {
		m, target := reviewFixture(t)
		i := strings.LastIndex(path, ".")
		parent, e := rawNode(m, path[:i])
		if e != nil {
			t.Fatal(e)
		}
		delete(parent, path[i+1:])
		if strings.HasSuffix(path, "originalQty") {
			item, _ := rawNode(m, "checkout.review.fulfillmentReview.group.item")
			delete(item, "itemQuantity")
		}
		if strings.HasSuffix(path, "paymentType") {
			d, _ := rawNode(m, "checkout.review.billingReview.wallet.d")
			delete(d, "paymentTypeLabel")
		}
		if _, e := ValidateReview(m, target); e == nil {
			t.Fatalf("accepted missing %s", path)
		}
	}
	m, target := reviewFixture(t)
	_ = setProperty(m, "checkout.review.fulfillmentReview.group.item.itemQuantity.d.currentQty", float64(2), true)
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("conflicting module quantities accepted")
	}
}

// Slot fixtures are synthetic schema contracts, not live REVIEW acceptance.
func TestReviewPickupSlot(t *testing.T) {
	m, target := reviewFixture(t)
	target.PickupSlotID = "slot-1"
	pickup, e := rawNode(m, "checkout.review.fulfillmentReview.group.d.pickup")
	if e != nil {
		t.Fatal(e)
	}
	pickup["timeSlotId"] = "slot-1"
	if _, e := ValidateReview(m, target); e != nil {
		t.Fatalf("exact server-selected slot rejected: %v", e)
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"wrong", "slot-2"}, {"empty", ""}, {"null", nil}, {"multiple", []any{"slot-1", "slot-2"}},
		{"singleArray", []any{"slot-1"}}, {"nested", Model{"id": "slot-1"}}, {"numeric", float64(1)}, {"boolean", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pickup["timeSlotId"] = tc.value
			if _, e := ValidateReview(m, target); e == nil {
				t.Fatal("unverified or ambiguous slot accepted")
			}
		})
	}
	delete(pickup, "timeSlotId")
	pickup["timeSlotID"] = "slot-1" // Unknown spelling is not authoritative.
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("missing exact schema inferred from unobserved alias")
	}
	// A correct value in an inactive/stale form cannot substitute for review data.
	checkout, _ := rawNode(m, "checkout")
	checkout["fulfillment"] = Model{"d": Model{"timeSlotId": "slot-1"}}
	if _, e := ValidateReview(m, target); e == nil {
		t.Fatal("stale fulfillment slot established review identity")
	}
}
