package checkout

import (
	"encoding/json"
	"reflect"
	"testing"
)

// All fixtures in this file are synthetic protocol contracts, not live acceptance.
func fixture(t *testing.T, s string) Model {
	t.Helper()
	var m Model
	if e := json.Unmarshal([]byte(s), &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestSubmitFields(t *testing.T) {
	m := fixture(t, `{"form":{"d":{"name":"Jack","zero":0,"off":false},"b":{"name":{"submit":true},"zero":{"p":{"submit":true}},"off":{"submit":true}},"c":["inactive"],"inactive":{"p":{"active":false},"d":{"secret":"hidden"},"b":{"secret":{"submit":true}}}}}`)
	v, e := SubmitFields(m, "form")
	if e != nil {
		t.Fatal(e)
	}
	if v.Get("form.off") != "false" || v.Get("form.zero") != "0" || v.Get("form.name") != "Jack" || len(v) != 3 {
		t.Fatalf("fields: %v", v)
	}
}
func TestModelPredicates(t *testing.T) {
	m := fixture(t, `{"form":{"d":{"x":"ok","n":false},"p":{"enabled":{"op":"AND","args":[{"op":"EQ","args":[{"get":"d.x"},"ok"]},{"op":"NOT","args":[{"get":"d.n"}]}]}},"b":{"x":{"submit":{"if":{"get":"p.enabled"},"then":true,"else":false},"active":{"op":"!EMPTY","args":[{"val":"_"}]}}}}}`)
	v, e := SubmitFields(m, "form")
	if e != nil || v.Get("form.x") != "ok" {
		t.Fatalf("%v %v", v, e)
	}
	m = fixture(t, `{"form":{"p":{"active":{"get":"p.enabled"},"enabled":{"get":"p.active"}}}}`)
	if _, e := SubmitFields(m, "form"); e == nil {
		t.Fatal("circular refs accepted")
	}
}
func TestUnsupportedModelFailsClosed(t *testing.T) {
	for _, s := range []string{`{"form":{"p":{"active":{"op":"UNKNOWN"}}}}`, `{"form":{"p":false}}`, `{"form":{"b":false}}`, `{"form":{"f":{"abstract":false}}}`, `{"form":{"d":{"x":"ok"},"b":{"x":{"submit":true,"p":false}}}}`, `{"form":{"d":{"x":"s"},"b":{"x":{"submit":true,"encryption":true}}}}`, `{"form":{"c":["a","a"],"a":{}}}`, `{"form":{"meta":{"wasPersisted":true}}}`} {
		if _, e := SubmitFields(fixture(t, s), "form"); e == nil {
			t.Fatalf("accepted unsupported model %s", s)
		}
	}
}
func TestBindingAction(t *testing.T) {
	for _, tc := range []struct {
		events string
		pass   bool
	}{{`[{"on":"click","do":"a.continue"}]`, true}, {`[{"on":"click","do":"a.continue"},{"on":"click","do":"js.sideEffect"}]`, false}, {`[{"on":"click","do":"a.continue","if":true}]`, false}, {`[{"on":"click","do":"a.one"},{"on":"click","do":"a.two"}]`, false}, {`[{"on":"click","do":{}}]`, false}} {
		m := fixture(t, `{"step":{"b":{"continue":{"events":`+tc.events+`}}}}`)
		a, e := BindingAction(m, "step", "continue")
		if tc.pass && (e != nil || a != "continue") || !tc.pass && e == nil {
			t.Fatalf("%s => %s %v", tc.events, a, e)
		}
	}
	m := fixture(t, `{"step":{"b":{"continue":{"p":{"enabled":false},"events":[{"on":"click","do":"a.go"}]}}}}`)
	if _, e := BindingAction(m, "step", "continue"); e == nil {
		t.Fatal("disabled binding accepted")
	}
}
func TestApplyResponseAtomic(t *testing.T) {
	m := fixture(t, `{"meta":{"h":{"one":"a"}},"root":{"c":["old"],"d":{"value":1},"b":{"go":{"p":{"enabled":false}}},"old":{"d":{}}}}`)
	before := clone(m)
	body := fixture(t, `{"meta":{"x":["root.old"],"l":["root.new"],"u":{"root":["d.value","b.go.enabled"]},"h":{"two":"b"}},"root":{"d":{"value":2},"b":{"go":{"enabled":true}},"new":{"d":{"x":3}}}}`)
	got, e := ApplyResponse(m, body)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(clone(m), before) {
		t.Fatal("input mutated")
	}
	if v, _ := lookup(got, "root.b.go.p.enabled"); v != true {
		t.Fatalf("binding patch missing: %v", got)
	}
	if _, exists := lookup(got, "root.old"); exists {
		t.Fatal("delete failed")
	}
	if v, _ := lookup(got, "meta.h.one"); v != "a" {
		t.Fatal("header merge failed")
	}
	if _, e := ApplyResponse(m, fixture(t, `{"meta":{"u":{"root":["d.value.deep"]}},"root":{"d":{"value":{"deep":2}}}}`)); e == nil {
		t.Fatal("unsupported patch accepted")
	}
	if !reflect.DeepEqual(clone(m), before) {
		t.Fatal("failed patch mutated input")
	}
}
func TestAbstractAndSetData(t *testing.T) {
	m := fixture(t, `{"template":{"d":{"x":"base"},"b":{"x":{"submit":true}}},"form":{"f":{"abstract":"/template"},"d":{"x":"local"}}}`)
	v, e := SubmitFields(m, "form")
	if e != nil || v.Get("form.x") != "local" {
		t.Fatalf("%v %v", v, e)
	}
	if e := SetData(m, "form", "x", "new"); e != nil {
		t.Fatal(e)
	}
	if e := SetData(m, "form", "absent", "new"); e == nil {
		t.Fatal("unknown contact field silently created")
	}
}

func TestBoundedAndCircularBindingExpressions(t *testing.T) {
	m := fixture(t, `{"form":{"d":{"x":"ok"},"b":{"x":{"submit":{"get":"b.x.submit"}}}}}`)
	if _, e := SubmitFields(m, "form"); e == nil {
		t.Fatal("circular binding accepted")
	}
	var expr any = true
	for i := 0; i < 150; i++ {
		expr = Model{"op": "NOT", "args": []any{expr}}
	}
	m = Model{"form": Model{"p": Model{"active": expr}}}
	if _, e := SubmitFields(m, "form"); e == nil {
		t.Fatal("unbounded expression accepted")
	}
}

// Synthetic regression built from observed recommendation metadata only; not a
// captured complete live shopping bag and not checkout acceptance.
func cachedRecommendationFixture(t *testing.T) Model {
	return fixture(t, `{"abs":{"RecommendedItem":{"d":{"cached":"retained"},"b":{"cached":{"submit":true}},"c":[]}},"shoppingCart":{"c":["recommendations"],"recommendations":{"c":["recommendedItem"],"recommendedItem":{"f":{"abstract":"/abs.RecommendedItem","type":"data","url":"/shop/recommendedForYou-full?partsInCart.0=SYNTHETIC&inline=true&recentAddedPart=SYNTHETIC"},"d":{"id":"synthetic","part":"SYNTHETIC"},"a":{"cachedAction":{"url":"/shop/synthetic-not-executed"}}}}}}`)
}
func TestCachedRecommendationDataMetadata(t *testing.T) {
	m := cachedRecommendationFixture(t)
	fields, e := SubmitFields(m, "shoppingCart")
	if e != nil {
		t.Fatal(e)
	}
	if fields.Get("shoppingCart.recommendations.recommendedItem.cached") != "retained" || len(fields) != 1 {
		t.Fatalf("cached template not serialized faithfully: %v", fields)
	}
	for _, tc := range []struct {
		name, path string
		value      any
	}{
		{"unknownType", "f.type", "script"}, {"wrongAbstract", "f.abstract", "/abs.Other"},
		{"absoluteURL", "f.url", "https://www.apple.com.cn/shop/recommendedForYou-full?partsInCart.0=x&inline=1&recentAddedPart=x"},
		{"otherPath", "f.url", "/shop/placeOrder?partsInCart.0=x&inline=1&recentAddedPart=x"},
		{"fragment", "f.url", "/shop/recommendedForYou-full?partsInCart.0=x&inline=1&recentAddedPart=x#x"},
		{"extraQuery", "f.url", "/shop/recommendedForYou-full?partsInCart.0=x&inline=1&recentAddedPart=x&unknown=x"},
		{"repeatedQuery", "f.url", "/shop/recommendedForYou-full?partsInCart.0=x&inline=1&inline=2&recentAddedPart=x"},
		{"missingQuery", "f.url", "/shop/recommendedForYou-full?partsInCart.0=x&inline=1"},
		{"badQuery", "f.url", "/shop/recommendedForYou-full?partsInCart.0=%zz&inline=1&recentAddedPart=x"},
		{"unknownFault", "f.extra", true}, {"nonemptyProperties", "p.enabled", true}, {"malformedProperties", "p", false}, {"malformedBindings", "b", false}, {"malformedChildren", "c", false}, {"nonemptyBinding", "b.extra", Model{}},
		{"children", "c", []any{"extra"}}, {"unknownData", "d.extra", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := cachedRecommendationFixture(t)
			if e := setProperty(m, "shoppingCart.recommendations.recommendedItem."+tc.path, tc.value, true); e != nil {
				t.Fatal(e)
			}
			if _, e := SubmitFields(m, "shoppingCart"); e == nil {
				t.Fatal("unrecognized recommendation data metadata accepted")
			}
		})
	}
	m = cachedRecommendationFixture(t)
	item, _ := rawNode(m, "shoppingCart.recommendations.recommendedItem")
	m["elsewhere"] = item
	if _, e := Node(m, "elsewhere"); e == nil {
		t.Fatal("metadata allowed outside exact observed module")
	}
}

func TestJSONNumberTruth(t *testing.T) {
	for _, s := range []string{"0", "0.0", "-0", "-0.0", "0e0", "0e10", "-0E-5", "NaN", "Infinity", "1e9999", "invalid"} {
		if truth(json.Number(s)) {
			t.Fatalf("zero/nonfinite/invalid number %q considered true", s)
		}
	}
	for _, s := range []string{"1", "-1", "0.1", "1e-10"} {
		if !truth(json.Number(s)) {
			t.Fatalf("finite nonzero number %q considered false", s)
		}
	}
}

// Synthetic reproduction of the observed fulfillment event shape. Analytics are
// declarative metadata only; this test does not capture or execute a live click.
func TestBindingActionDeclarativeAnalytics(t *testing.T) {
	m := fixture(t, `{"checkout":{"fulfillment":{"d":{"pickupTab":{"pickup":{"timeSlot":{"dateTimeSlots":{"timeSlotValue":"synthetic-slot"}},"storeLocator":{"searchInput":"synthetic-location"}}}},"b":{"continue":{"events":[{"on":"click","do":"a.continueFromFulfillmentToPickupContact","metric":"t.fulfillmentContinueAction"},{"on":"click","metric":"t.timeSlotNotSelectedError","condition":{"op":"EQ","args":[{"get":"d.pickupTab.pickup.timeSlot.dateTimeSlots.timeSlotValue"},""]}},{"on":"click","metric":"t.emptySearchError","condition":{"op":"EQ","args":[{"get":"d.pickupTab.pickup.storeLocator.searchInput"},""]}}]}}}}}`)
	action, e := BindingAction(m, "checkout.fulfillment", "continue")
	if e != nil || action != "continueFromFulfillmentToPickupContact" {
		t.Fatalf("observed declarative analytics rejected: %q %v", action, e)
	}
	// True telemetry predicates and missing telemetry references are not gates.
	for _, value := range []any{"", nil} {
		if err := setProperty(m, "checkout.fulfillment.d.pickupTab.pickup.timeSlot.dateTimeSlots.timeSlotValue", value, false); err != nil {
			t.Fatal(err)
		}
		if err := setProperty(m, "checkout.fulfillment.d.pickupTab.pickup.storeLocator.searchInput", value, false); err != nil {
			t.Fatal(err)
		}
		if _, err := BindingAction(m, "checkout.fulfillment", "continue"); err != nil {
			t.Fatalf("telemetry result gated action: %v", err)
		}
	}
}

func TestBindingActionRejectsUnsafeAnalytics(t *testing.T) {
	for _, events := range []string{
		`[{"on":"click","do":"a.go","metric":"not-a-metric"}]`,
		`[{"on":"click","do":"a.go","metric":["t.one"]}]`,
		`[{"on":"click","do":"a.go","metric":"t.one.two"}]`,
		`[{"on":"click","do":"a.go","metric":"t.one","unknown":true}]`,
		`[{"on":"click","do":"a.go","metric":"t.one","condition":false}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","do":"a.other"}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","do":"js.run"}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","do":null}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","unknown":true}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","condition":{"op":"RUN_JS","args":[]}}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":"t.two","condition":"js.run()"}]`,
		`[{"on":"click","do":"a.go"},{"on":"click","metric":""}]`,
		`[{"on":"click","metric":"t.only"}]`,
	} {
		m := fixture(t, `{"step":{"b":{"continue":{"events":`+events+`}}}}`)
		if _, e := BindingAction(m, "step", "continue"); e == nil {
			t.Fatalf("unsafe analytics accepted: %s", events)
		}
	}
	m := fixture(t, `{"step":{"b":{"continue":{"enabled":false,"events":[{"on":"click","do":"a.go","metric":"t.go"},{"on":"click","metric":"t.error"}]}}}}`)
	if _, e := BindingAction(m, "step", "continue"); e == nil {
		t.Fatal("analytics bypassed disabled binding")
	}
}

// Synthetic regression for an observed repeated eFapiaoSelector child reference;
// the model contains one module, and its submitted field must appear only once.
func TestPickupContactRepeatedInvoiceChild(t *testing.T) {
	m := fixture(t, `{"checkout":{"pickupContact":{"c":["selfPickupContact","footnotes","eFapiaoSelector","faq","eFapiaoSelector"],"selfPickupContact":{},"footnotes":{},"faq":{},"eFapiaoSelector":{"d":{"invoice":"synthetic"},"b":{"invoice":{"submit":true}}}}}}`)
	fields, e := SubmitFields(m, "checkout.pickupContact")
	if e != nil {
		t.Fatal(e)
	}
	if got := fields["checkout.pickupContact.eFapiaoSelector.invoice"]; len(got) != 1 || got[0] != "synthetic" || len(fields) != 1 {
		t.Fatalf("invoice submitted more than once or lost: %v", fields)
	}
	for _, tc := range []struct {
		name, path string
		children   []any
	}{
		{"otherDuplicate", "checkout.pickupContact", []any{"faq", "faq"}},
		{"mixedDuplicate", "checkout.pickupContact", []any{"eFapiaoSelector", "eFapiaoSelector", "faq", "faq"}},
		{"nonString", "checkout.pickupContact", []any{"eFapiaoSelector", true, "eFapiaoSelector"}},
		{"otherPath", "other", []any{"eFapiaoSelector", "eFapiaoSelector"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{}
			if tc.path == "checkout.pickupContact" {
				m["checkout"] = Model{"pickupContact": Model{"c": tc.children, "faq": Model{}, "eFapiaoSelector": Model{}}}
			} else {
				m["other"] = Model{"c": tc.children, "eFapiaoSelector": Model{}}
			}
			if _, e := SubmitFields(m, tc.path); e == nil {
				t.Fatal("unrecognized duplicate child accepted")
			}
		})
	}
}
