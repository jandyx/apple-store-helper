package checkout

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func actionNode(url string) Model {
	return Model{"a": Model{"next": Model{"url": url}}, "b": Model{"continue": Model{"events": []any{Model{"on": "click", "do": "a.next"}}}}}
}
func TestSyntheticRequestPipeline(t *testing.T) {
	for _, mode := range []string{ModeReview, ModeSubmit, "final_unknown", "stale_confirmation", "ambiguous_confirmation"} {
		t.Run(mode, func(t *testing.T) {
			c, target := testConfig(t)
			review, _ := reviewFixture(t)
			setProperty(review, "checkout.review.fulfillmentReview.group.item.d.productId", target.SKU, true)
			setProperty(review, "checkout.review.fulfillmentReview.group.item.d.name", skuTitle(target.SKU), true)
			setProperty(review, "checkout.review.fulfillmentReview.group.d.pickup.contact", Model{"firstName": target.Contact.FirstName, "lastName": target.Contact.LastName, "emailAddress": target.Contact.Email, "mobilePhone": target.Contact.Phone}, true)
			place := actionNode("/shop/checkout/final")
			a, _ := object(place["a"])
			next, _ := object(a["next"])
			next["submit"] = true
			place["c"] = []any{"termsAndConditions"}
			place["termsAndConditions"] = Model{"d": Model{"shouldShowCheckbox": true, "termsCheckbox": false}, "b": Model{"termsCheckbox": Model{"submit": true}}}
			setProperty(review, "checkout.review.placeOrder", place, true)
			bag := Model{"shoppingCart": Model{"items": Model{"c": []any{"phone"}, "phone": Model{"d": Model{"sku": target.SKU}, "itemQuantity": Model{"d": Model{"quantity": 1}}}}, "summary": Model{"d": Model{"total": "RMB 17,999"}}, "actions": Model{"a": Model{"checkout": Model{"url": "/shop/checkout/start"}}}}}
			locator := Model{"a": Model{"search": Model{"url": "/shop/checkout/search"}, "select": Model{"url": "/shop/checkout/select"}}, "searchResults": Model{"d": Model{"retailStores": []any{Model{"storeId": target.StoreID, "storeName": "Apple 深圳益田假日广场", "storeDisabled": false, "availability": Model{"availableNowForAllLines": true, "lineItemAvailability": []any{Model{"partName": skuTitle(target.SKU), "availableNowForLine": true}}}}}}}}
			fulfill := Model{"checkout": Model{"c": []any{"companionBar", "fulfillment", "locationConsent", "session"}, "fulfillment": Model{"fulfillmentOptions": Model{"a": Model{"selectFulfillmentLocationAction": Model{"url": "/shop/checkout/location"}}}, "pickupTab": Model{"pickup": Model{"c": []any{}, "storeLocator": locator}}}}}
			contactNode := actionNode("/shop/checkout/contactdone")
			contactNode["d"] = Model{"firstName": "", "lastName": "", "emailAddress": "", "mobilePhone": ""}
			b, _ := object(contactNode["b"])
			for _, k := range []string{"firstName", "lastName", "emailAddress", "mobilePhone"} {
				b[k] = Model{}
			}
			contact := Model{"checkout": Model{"c": []any{"pickupContact"}, "pickupContact": contactNode}}
			billingNode := actionNode("/shop/checkout/billingdone")
			billingNode["billingOptions"] = Model{"d": Model{"options": []any{Model{"value": "WECHAT"}}}, "a": Model{"selectBillingOptionAction": Model{"url": "/shop/checkout/payment"}}}
			billing := Model{"checkout": Model{"c": []any{"billing"}, "billing": billingNode}}
			models := map[string]Model{"/shop/bag": bag, "/shop/checkout/fulfill": fulfill, "/shop/checkout/contact": contact, "/shop/checkout/billing": billing, "/shop/checkout/review": review, "/shop/checkout/created": {"checkout": Model{"c": []any{"routeOut"}, "routeOut": Model{"d": Model{"orderNumber": "W123456789"}}}}}
			if mode == "stale_confirmation" {
				models["/shop/checkout/created"] = fixture(t, `{"checkout":{"c":["review"],"routeOut":{"d":{"orderNumber":"W987654321"}}}}`)
			}
			if mode == "ambiguous_confirmation" {
				models["/shop/checkout/created"] = fixture(t, `{"checkout":{"c":["routeOut","poi"],"routeOut":{"d":{"orderNumber":"W123456789"}},"poi":{"c":["routeOut"],"routeOut":{"d":{"orderNumber":"W987654321"}}}}}`)
			}
			finalCalls := 0
			mutations := 0
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "GET" {
					if req.URL.Path == "/shop/updateSummary" {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(Model{"head": Model{"status": "200"}, "body": Model{"response": Model{"summarySection": Model{"summary": Model{"productTitle": skuTitle(target.SKU), "isBuyable": true, "disabled": false, "priceData": Model{"fullPrice": Model{"priceString": "RMB 17,999"}}}}}}})
						return
					}
					m, ok := models[req.URL.Path]
					if !ok {
						t.Error("unknown GET", req.URL.Path)
						w.WriteHeader(404)
						return
					}
					w.Header().Set("Content-Type", "text/html")
					b, _ := json.Marshal(m)
					fmt.Fprintf(w, "<script id=\"init_data\">%s</script>", b)
					return
				}
				mutations++
				dest := "/shop/checkout/fulfill"
				switch req.URL.Path {
				case "/shop/checkout/start", "/shop/checkout/location", "/shop/checkout/search":
				case "/shop/checkout/select":
					dest = "/shop/checkout/contact"
				case "/shop/checkout/contactdone":
					dest = "/shop/checkout/billing"
				case "/shop/checkout/payment":
					dest = "/shop/checkout/billing"
				case "/shop/checkout/billingdone":
					dest = "/shop/checkout/review"
				case "/shop/checkout/final":
					finalCalls++
					req.ParseForm()
					if req.Form.Get("checkout.review.placeOrder.termsAndConditions.termsCheckbox") != "true" {
						t.Error("terms missing from final POST")
					}
					if mode == "final_unknown" {
						w.WriteHeader(541)
						return
					}
					dest = "/shop/checkout/created"
				default:
					t.Error("unknown action", req.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(Model{"head": Model{"status": "303", "data": Model{"url": dest}}})
			}))
			defer sv.Close()
			runMode := mode
			if mode != ModeReview && mode != ModeSubmit {
				runMode = ModeSubmit
			}
			r, _ := NewRunner(c, runMode)
			r.local = true
			r.base = sv.URL
			r.newClient = func(Session) (*http.Client, error) { return sv.Client(), nil }
			result, e := r.Run(context.Background(), target)
			if mode != ModeReview && mode != ModeSubmit {
				if e == nil || result.State != "submission_unknown" || finalCalls != 1 {
					t.Fatalf("unknown result %#v %v", result, e)
				}
				_, e = r.Run(context.Background(), target)
				if e == nil || finalCalls != 1 {
					t.Fatal("unknown retried")
				}
				return
			}
			if e != nil {
				t.Fatalf("stage=%s result=%#v err=%v", result.Stage, result, e)
			}
			if mutations != 7+(map[bool]int{true: 1}[mode == ModeSubmit]) {
				t.Fatalf("mutations %d", mutations)
			}
			if mode == ModeReview {
				if result.State != "review_ready" || finalCalls != 0 {
					t.Fatal(result, finalCalls)
				}
			} else {
				if result.State != "created" || finalCalls != 1 {
					t.Fatal(result, finalCalls)
				}
				_, e = r.Run(context.Background(), target)
				if e == nil || finalCalls != 1 {
					t.Fatal("restarted final", e, finalCalls)
				}
			}
		})
	}
}

func TestContactAndTermsMutateAuthoritativeModel(t *testing.T) {
	_, target := testConfig(t)
	m := fixture(t, `{"checkout":{"pickupContact":{"d":{"firstName":"old","lastName":"old","emailAddress":"old","mobilePhone":"old"},"b":{"firstName":{"submit":true},"lastName":{"submit":true},"emailAddress":{"submit":true},"mobilePhone":{"submit":true}}},"review":{"placeOrder":{"termsAndConditions":{"d":{"shouldShowCheckbox":true,"termsCheckbox":false},"b":{"termsCheckbox":{"submit":true}}}}}}}`)
	if e := fillContact(m, target.Contact); e != nil {
		t.Fatal(e)
	}
	d, _ := rawNode(m, "checkout.pickupContact.d")
	if d["mobilePhone"] != target.Contact.Phone || d["emailAddress"] != target.Contact.Email {
		t.Fatal("contact did not update model")
	}
	if e := acceptTerms(m); e != nil {
		t.Fatal(e)
	}
	d, _ = rawNode(m, "checkout.review.placeOrder.termsAndConditions.d")
	if d["termsCheckbox"] != true {
		t.Fatal("terms not updated")
	}
}

func TestSyntheticTimeslotSelection(t *testing.T) {
	m := fixture(t, `{"checkout":{"fulfillment":{"pickupTab":{"pickup":{"c":["timeSlot"],"timeSlot":{"dateTimeSlots":{"d":{"pickUpDates":[{"dayOfMonth":"23","date":"2026-10-23"}],"timeSlotWindows":[{"23":[{"timeSlotValue":"23-1000","isRestricted":false,"timeSlotType":"WINDOW","checkInEnd":"11:00","checkInStart":"10:00","signKey":"fixture-private","timeZone":"Asia/Shanghai","SlotId":"slot-fixture-1","displayStart":"10:00","displayEnd":"11:00"}]}]}}}}}}}}`)
	if e := chooseSlot(m); e != nil {
		t.Fatal(e)
	}
	d, _ := rawNode(m, "checkout.fulfillment.pickupTab.pickup.timeSlot.dateTimeSlots.d")
	if d["timeSlotId"] != "slot-fixture-1" || d["date"] != "2026-10-23" {
		t.Fatal("slot not persisted")
	}
}

func TestActiveOrderConfirmationRejectsStaleAndAmbiguous(t *testing.T) {
	for _, raw := range []string{
		`{"checkout":{"c":["review"],"routeOut":{"d":{"orderNumber":"W123456789"}}}}`,
		`{"checkout":{"c":["routeOut","poi"],"routeOut":{"d":{"orderNumber":"W123456789"}},"poi":{"c":["routeOut"],"routeOut":{"d":{"orderNumber":"W987654321"}}}}}`,
		`{"checkout":{"c":["poi"],"routeOut":{"d":{"orderNumber":"W123456789"}},"poi":{"c":["review"],"routeOut":{"d":{"orderNumber":"W987654321"}}}}}`,
		`{"checkout":{"c":["routeOut"],"routeOut":{"d":{}}}}`,
	} {
		if _, e := activeOrderNumber(fixture(t, raw)); e == nil {
			t.Fatal("inactive or ambiguous order accepted")
		}
	}
	number, e := activeOrderNumber(fixture(t, `{"checkout":{"c":["poi"],"routeOut":{"d":{"orderNumber":"W111111111"}},"poi":{"c":["routeOut"],"routeOut":{"d":{"orderNumber":"W222222222"}}}}}`))
	if e != nil || number != "W222222222" {
		t.Fatal("active branch not respected")
	}
}
func TestDurableStateDirectoryBeforeMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "state")
	if e := durableStateDirectory(path); e != nil {
		t.Fatal(e)
	}
	for p := path; p != filepath.Dir(filepath.Dir(filepath.Dir(path))); p = filepath.Dir(p) {
		st, e := os.Stat(p)
		if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
			t.Fatal("state directory not private")
		}
	}
	bad := filepath.Join(t.TempDir(), "file")
	if e := os.WriteFile(bad, []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := durableStateDirectory(filepath.Join(bad, "state")); e == nil {
		t.Fatal("invalid state accepted")
	}
}

// This native-add redirect is source-derived synthetic protocol, NOT a live
// captured add request. Exactly one mutation may precede the safe bag GET.
func TestEmptyBagNativeAddRedirectOnce(t *testing.T) {
	for _, status := range []int{302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, target := testConfig(t)
			empty := fixture(t, `{"shoppingCart":{"items":{"c":[]}}}`)
			if yes, e := validateBag(empty, target); e != nil || !yes {
				t.Fatal("empty fixture invalid")
			}
			mutations, reads := 0, 0
			sv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/shop/buy-iphone/iphone-duo":
					mutations++
					w.Header().Set("Location", "/shop/bag")
					w.WriteHeader(status)
				case "/shop/bag":
					reads++
					if r.URL.RawQuery != "" {
						t.Error("mutating fields copied into bag read")
					}
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprintf(w, `<script id="init_data">{"shoppingCart":{"items":{"c":["phone"],"phone":{"d":{"sku":%q},"itemQuantity":{"d":{"quantity":1}}}},"summary":{"d":{"total":"RMB 17,999"}}}}</script>`, target.SKU)
				default:
					t.Error("unknown request")
				}
			}))
			defer sv.Close()
			u, _ := url.Parse(sv.URL)
			client := sv.Client()
			client.Jar, _ = cookiejar.New(nil)
			client.Jar.SetCookies(u, []*http.Cookie{{Name: "as_atb", Value: "fixture|token", Path: "/"}})
			s := wireSession{client: client, base: u, local: true, model: empty}
			e := s.add(context.Background(), Model{"buyNowButton": `<form action="/shop/buy-iphone/iphone-duo" method="get"><input name="product" value="fixture"><button name="add-to-cart" value="true"></button></form>`})
			if mutations != 1 {
				t.Fatal("mutation replayed")
			}
			if status == 307 || status == 308 {
				if e == nil || reads != 0 {
					t.Fatal("unsafe redirect followed")
				}
				return
			}
			if e != nil || reads != 1 {
				t.Fatal("safe read failed", e)
			}
			if e = s.load(context.Background(), "/shop/bag"); e != nil {
				t.Fatal(e)
			}
			if yes, e := validateBag(s.model, target); e != nil || yes {
				t.Fatal("added bag mismatch")
			}
			if mutations != 1 {
				t.Fatal("mutation replayed")
			}
		})
	}
}

// This is a synthetic fixture matching the observed CN pickup slot schema;
// values are placeholders, not captured customer or signature data.
func nativeShapeTimeslotFixture(t *testing.T) (Model, Model, Model) {
	t.Helper()
	m := fixture(t, `{"checkout":{"fulfillment":{"pickupTab":{"pickup":{"c":["timeSlot"],"timeSlot":{"dateTimeSlots":{"d":{"storeSelected":"R639","pickUpDates":[{"dayOfMonth":"23","date":"2026-10-23"}],"timeSlotWindows":[{"23":[{"Label":"10:00 - 11:00","SlotId":"slot-fixture-native","checkInEnd":"11:00","checkInStart":"10:00","isRestricted":null,"signKey":"fixture-signature","timeSlotValue":"23-1000","timeZone":"Asia/Shanghai"}]}]}}}}}}}}`)
	d, err := rawNode(m, "checkout.fulfillment.pickupTab.pickup.timeSlot.dateTimeSlots.d")
	if err != nil {
		t.Fatal(err)
	}
	windows := d["timeSlotWindows"].([]any)
	day, _ := object(windows[0])
	slot, _ := object(day["23"].([]any)[0])
	return m, d, slot
}

func TestNativeShapeTimeslotSelection(t *testing.T) {
	m, d, _ := nativeShapeTimeslotFixture(t)
	if err := chooseSlot(m); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"timeSlotId": "slot-fixture-native", "date": "2026-10-23",
		"startTime": "10:00", "endTime": "11:00", "signKey": "fixture-signature",
		"timeZone": "Asia/Shanghai", "timeSlotValue": "23-1000",
		"timeSlotType": "", "displayStartTime": "", "displayEndTime": "", "isRestricted": nil,
	} {
		if d[key] != want {
			t.Errorf("%s = %v, want %v", key, d[key], want)
		}
	}
}

func TestTimeslotRejectsMissingRequiredFields(t *testing.T) {
	for _, field := range []string{"SlotId", "checkInEnd", "checkInStart", "signKey", "timeSlotValue", "timeZone"} {
		for _, invalid := range []string{"missing", "empty", "null", "wrong_type"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				m, _, slot := nativeShapeTimeslotFixture(t)
				// A false restriction flag isolates required-field validation from
				// the independently tested nullable restriction representation.
				slot["isRestricted"] = false
				switch invalid {
				case "missing":
					delete(slot, field)
				case "empty":
					slot[field] = ""
				case "null":
					slot[field] = nil
				case "wrong_type":
					slot[field] = true
				}
				before, _ := json.Marshal(m)
				if err := chooseSlot(m); err == nil {
					t.Fatal("accepted invalid required slot field")
				}
				after, _ := json.Marshal(m)
				if string(before) != string(after) {
					t.Fatal("failed selection changed authoritative model")
				}
			})
		}
	}
}

func TestTimeslotRejectsRestrictedAndUnknownRestriction(t *testing.T) {
	for _, restriction := range []any{true, "false", "restricted", float64(0)} {
		m, _, slot := nativeShapeTimeslotFixture(t)
		slot["isRestricted"] = restriction
		if err := chooseSlot(m); err == nil {
			t.Fatalf("accepted restriction representation %T", restriction)
		}
	}
	m, _, slot := nativeShapeTimeslotFixture(t)
	delete(slot, "isRestricted")
	if err := chooseSlot(m); err == nil {
		t.Fatal("accepted missing restriction field")
	}
}
