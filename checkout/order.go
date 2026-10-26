package checkout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Runner struct {
	config    Config
	mode      string
	newClient ClientFactory
	base      string
	local     bool
}

func NewRunner(c Config, mode string) (*Runner, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	// The armed policy must not change through a caller-owned slice after the
	// confirmation dialog or while an inventory callback is selecting a store.
	c.StoreIDs = append([]string(nil), c.StoreIDs...)
	if mode == "" {
		mode = ModeReview
	}
	if mode != ModeReview && mode != ModeSubmit {
		return nil, problem("invalid mode")
	}
	return &Runner{config: c, mode: mode, newClient: sessionClient, base: "https://www.apple.com.cn"}, nil
}
func (r *Runner) validateTarget(t Target) error {
	c := r.config
	if t.SKU != c.SKU || t.Province != c.Province || t.City != c.City || t.District != c.District || t.MaxTotalCents != c.MaxTotalCents || t.Payment != c.Payment {
		return problem("target differs from configuration")
	}
	found := false
	for _, s := range c.StoreIDs {
		found = found || s == t.StoreID
	}
	if !found {
		return problem("store differs from configuration")
	}
	if e := validatePrivateContact(t.Contact); e != nil {
		return e
	}
	contact, e := LoadContact(c.ContactFile)
	if e != nil || contact != t.Contact {
		return problem("contact differs from private configuration")
	}
	return nil
}
func (r *Runner) Run(ctx context.Context, t Target) (result Result, err error) {
	result = Result{State: "failed", Stage: "configuration", SKU: t.SKU, StoreID: t.StoreID}
	if e := r.validateTarget(t); e != nil {
		return result, e
	}
	if r.mode == ModeSubmit && t.Contact.IDLast4 != "" {
		return result, problem("identification review verification unavailable; review mode required")
	}
	if e := ctx.Err(); e != nil {
		return result, problem("cancelled")
	}
	lock, e := r.lock()
	if e != nil {
		return result, e
	}
	defer lock.Close()
	receipt := filepath.Join(r.config.StateDir, "submission.json")
	if _, e := os.Stat(receipt); e == nil {
		var prior storedReceipt
		if e := privateJSON(receipt, &prior); e != nil {
			return result, problem("prior submission requires manual reconciliation")
		}
		if prior.Identity != identity(t) {
			return result, problem("prior submission belongs to different target")
		}
		return prior.Result, problem("prior submission requires manual reconciliation")
	} else if !os.IsNotExist(e) {
		return result, problem("submission state unavailable")
	}
	t.PickupSlotID = ""
	result.Stage = "session"
	session, e := LoadSession(r.config.SessionFile)
	if e != nil {
		return result, e
	}
	client, e := r.newClient(session)
	if e != nil {
		return result, problem("session unavailable")
	}
	base, e := url.Parse(r.base)
	if e != nil {
		return result, problem("invalid base")
	}
	s := &wireSession{client: client, base: base, ua: session.UserAgent, local: r.local}
	result.Stage = "product"
	summary, e := s.summary(ctx, t.SKU)
	if e != nil {
		return result, e
	}
	title, ok := summary["productTitle"].(string)
	if !ok || title != skuTitle(t.SKU) || summary["isBuyable"] != true || summary["disabled"] != false {
		return result, problem("unsupported or unavailable product")
	}
	t.ProductName = title
	priceNode, e := Node(summary, "priceData.fullPrice")
	if e != nil {
		return result, problem("product price unavailable")
	}
	price, _ := priceNode["priceString"].(string)
	amount, e := ParseMoney(price)
	if e != nil || amount > t.MaxTotalCents {
		return result, problem("product price exceeds cap or unknown")
	}
	result.Stage = "bag"
	if e = s.load(ctx, "/shop/bag"); e != nil {
		return result, e
	}
	empty, e := validateBag(s.model, t)
	if e != nil {
		return result, e
	}
	if empty {
		if e = s.add(ctx, summary); e != nil {
			return result, e
		}
		if e = s.load(ctx, "/shop/bag"); e != nil {
			return result, e
		}
		if empty, e = validateBag(s.model, t); e != nil || empty {
			return result, problem("added bag mismatch")
		}
	}
	result.Stage = "fulfillment"
	if e = s.action(ctx, "shoppingCart.actions", "checkout", nil); e != nil {
		return result, e
	}
	if phase(s.model) != "fulfillment" {
		return result, problem("checkout authentication or unsupported phase")
	}
	opt := "checkout.fulfillment.fulfillmentOptions"
	if e = s.action(ctx, opt, "selectFulfillmentLocationAction", url.Values{opt + ".selectFulfillmentLocation": {"RETAIL"}}); e != nil {
		return result, e
	}
	const locator = "checkout.fulfillment.pickupTab.pickup.storeLocator"
	prefix := locator + ".address.stateCitySelectorForCheckout."
	region := t.Province + " " + t.City + " " + t.District
	fields := url.Values{locator + ".searchInput": {region}, prefix + "state": {t.Province}, prefix + "city": {t.City}, prefix + "district": {t.District}, prefix + "provinceCityDistrict": {region}, prefix + "countryCode": {"CN"}}
	if e = s.action(ctx, locator, "search", fields); e != nil {
		return result, e
	}
	stores, e := Node(s.model, locator+".searchResults.d")
	if e != nil {
		return result, problem("store results unavailable")
	}
	rows, ok := stores["retailStores"].([]any)
	if !ok {
		return result, problem("store results unsupported")
	}
	matched := 0
	for _, raw := range rows {
		store, ok := object(raw)
		if !ok {
			return result, problem("invalid store result")
		}
		if store["storeId"] != t.StoreID {
			continue
		}
		matched++
		avail, e := Node(store, "availability")
		if e != nil || store["storeDisabled"] != false || avail["availableNowForAllLines"] != true {
			return result, problem("pickup stock unavailable")
		}
		lines, ok := avail["lineItemAvailability"].([]any)
		if !ok || len(lines) != 1 {
			return result, problem("pickup product mismatch")
		}
		line, ok := object(lines[0])
		if !ok || line["partName"] != t.ProductName || line["availableNowForLine"] != true {
			return result, problem("pickup product mismatch")
		}
		t.StoreName, _ = store["storeName"].(string)
	}
	if matched != 1 || t.StoreName == "" {
		return result, problem("authoritative store missing")
	}
	if e = s.action(ctx, locator, "select", url.Values{locator + ".selectStore": {t.StoreID}}); e != nil {
		return result, e
	}
	for i := 0; i < 5; i++ {
		current := phase(s.model)
		result.Stage = current
		if current == "review" {
			break
		}
		switch current {
		case "fulfillment":
			e = chooseSlot(s.model)
			if e == nil {
				if d, slotErr := rawNode(s.model, "checkout.fulfillment.pickupTab.pickup.timeSlot.dateTimeSlots.d"); slotErr == nil {
					slot, ok := d["timeSlotId"].(string)
					if !ok || slot == "" {
						e = problem("selected pickup slot missing")
					} else {
						t.PickupSlotID = slot
					}
				}
			}
		case "pickupContact":
			e = fillContact(s.model, t.Contact)
		case "billing":
			p := "checkout.billing.billingOptions"
			var n Model
			n, e = Node(s.model, p+".d")
			if e == nil {
				available := false
				if opts, ok := n["options"].([]any); ok {
					for _, v := range opts {
						if m, ok := object(v); ok && m["value"] == t.Payment {
							available = true
						}
					}
				}
				if !available {
					e = problem("payment option unavailable")
				} else {
					e = s.action(ctx, p, "selectBillingOptionAction", url.Values{p + ".selectBillingOption": {t.Payment}})
				}
			}
		default:
			e = problem("unsupported checkout phase")
		}
		if e != nil {
			return result, e
		}
		if e = s.binding(ctx, "checkout."+current); e != nil {
			return result, e
		}
		if phase(s.model) == current {
			return result, problem("checkout did not advance")
		}
	}
	result.Stage = "review"
	total, e := ValidateReview(s.model, t)
	if e != nil {
		return result, problem("review verification failed")
	}
	result.TotalCents = total
	result.State = "review_ready"
	if r.mode == ModeReview {
		return result, nil
	}
	result.Stage = "submit"
	if e = acceptTerms(s.model); e != nil {
		return result, e
	}
	name, e := BindingAction(s.model, "checkout.review.placeOrder", "continue")
	if e != nil {
		return result, problem("final action unavailable")
	}
	result.State = "submission_unknown"
	if e = saveReceipt(receipt, storedReceipt{identity(t), result}, true); e != nil {
		return result, e
	}
	if e = s.action(ctx, "checkout.review.placeOrder", name, nil); e != nil {
		return result, problem("submission outcome unknown; reconcile before retry")
	}
	order, e := activeOrderNumber(s.model)
	if e != nil {
		return result, problem("submission outcome unknown; reconcile before retry")
	}
	result.State = "created"
	result.OrderNumber = order
	if e = saveReceipt(receipt, storedReceipt{identity(t), result}, false); e != nil {
		result.State = "submission_unknown"
		return result, e
	}
	return result, nil
}
func (s *wireSession) summary(ctx context.Context, sku string) (Model, error) {
	b, ct, e := s.request(ctx, "GET", "/shop/updateSummary", url.Values{"fae": {"true"}, "node": {"home/shop_iphone/family/iphone_duo"}, "step": {"select"}, "product": {sku}, "acpart": {"none"}}, false)
	if e != nil {
		return nil, e
	}
	if !strings.Contains(ct, "application/json") {
		return nil, problem("expected summary JSON")
	}
	m, e := decodeObject(b)
	if e != nil {
		return nil, e
	}
	h, e := Node(m, "head")
	if e != nil || fmt.Sprint(h["status"]) != "200" {
		return nil, problem("summary rejected")
	}
	return Node(m, "body.response.summarySection.summary")
}
func validateBag(m Model, t Target) (bool, error) {
	items, e := Node(m, "shoppingCart.items")
	if e != nil {
		return false, problem("bag model missing")
	}
	children, ok := items["c"].([]any)
	if !ok {
		return false, problem("bag children unsupported")
	}
	if len(children) == 0 {
		return true, nil
	}
	if len(children) != 1 {
		return false, problem("bag must contain one matching item")
	}
	name, ok := children[0].(string)
	if !ok {
		return false, problem("bag item invalid")
	}
	d, e := Node(items, name+".d")
	if e != nil || d["sku"] != t.SKU {
		return false, problem("bag SKU mismatch")
	}
	q, e := Node(items, name+".itemQuantity.d")
	if e != nil || fmt.Sprint(q["quantity"]) != "1" {
		return false, problem("bag quantity mismatch")
	}
	sum, e := Node(m, "shoppingCart.summary.d")
	if e != nil {
		return false, problem("bag total missing")
	}
	price, ok := sum["total"].(string)
	if !ok {
		return false, problem("bag total missing")
	}
	n, e := ParseMoney(price)
	if e != nil || n > t.MaxTotalCents {
		return false, problem("bag total exceeds cap")
	}
	return false, nil
}

var formRE = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
var inputRE = regexp.MustCompile(`(?is)<(?:input|button)\b([^>]*)>`)
var attrRE = regexp.MustCompile(`([\w-]+)\s*=\s*["']([^"']*)["']`)

func attrs(s string) map[string]string {
	m := map[string]string{}
	for _, v := range attrRE.FindAllStringSubmatch(s, -1) {
		m[strings.ToLower(v[1])] = html.UnescapeString(v[2])
	}
	return m
}
func (s *wireSession) add(ctx context.Context, summary Model) error {
	raw, ok := summary["buyNowButton"].(string)
	if !ok {
		return problem("native add form missing")
	}
	var action, method string
	fields := url.Values{}
	count := 0
	for _, f := range formRE.FindAllStringSubmatch(raw, -1) {
		a := attrs(f[1])
		v := url.Values{}
		for _, i := range inputRE.FindAllStringSubmatch(f[2], -1) {
			ia := attrs(i[1])
			if strings.Contains(strings.ToLower(i[1]), "disabled") {
				continue
			}
			n := ia["name"]
			if n != "" && (ia["type"] != "submit" && ia["type"] != "button" || n == "add-to-cart") {
				v.Set(n, ia["value"])
			}
		}
		if _, ok := v["add-to-cart"]; ok {
			count++
			action = a["action"]
			method = strings.ToUpper(a["method"])
			fields = v
		}
	}
	nativeURL, parseErr := url.Parse(action)
	if count != 1 || (method != "POST" && method != "GET") || parseErr != nil || nativeURL.Path == "" || nativeURL.Fragment != "" {
		return problem("unsupported native add form")
	}
	u, e := s.resolve(action)
	if e != nil || !strings.HasPrefix(u.Path, "/shop/buy-iphone/iphone-duo") {
		return problem("native add destination refused")
	}
	tokens := 0
	if s.client.Jar == nil {
		return problem("bag cookie jar unavailable")
	}
	for _, c := range s.client.Jar.Cookies(s.base) {
		if c.Name == "as_atb" {
			parts := strings.Split(c.Value, "|")
			fields.Set("atbtoken", parts[len(parts)-1])
			tokens++
		}
	}
	if tokens != 1 || fields.Get("atbtoken") == "" {
		return problem("bag token unavailable")
	}
	_, _, e = s.requestCore(ctx, method, action, fields, false, true)
	return e
}
func phase(m Model) string {
	if _, ok := m["signIn"]; ok {
		return "authentication"
	}
	n, e := Node(m, "checkout")
	if e != nil {
		return "unknown"
	}
	c, ok := n["c"].([]any)
	if !ok {
		return "unknown"
	}
	// Native checkout lists utility modules alongside the active step. Ignore
	// only observed utilities; reject unknown or multiple steps rather than
	// choosing a possibly stale step by priority.
	current := ""
	for _, child := range c {
		name, ok := child.(string)
		if !ok {
			return "unknown"
		}
		switch name {
		case "companionBar", "locationConsent", "session":
			continue
		case "fulfillment", "pickupContact", "billing", "review", "routeOut", "poi":
			if current != "" {
				return "unknown"
			}
			current = name
		default:
			return "unknown"
		}
	}
	if current == "" {
		return "unknown"
	}
	return current
}
func walk(m Model, path string, fn func(Model, string) error) error {
	if strings.Count(path, ".") > 64 {
		return problem("module nesting limit")
	}
	if e := fn(m, path); e != nil {
		return e
	}
	if raw, ok := m["c"]; ok {
		c, ok := raw.([]any)
		if !ok || len(c) > 512 {
			return problem("unsupported module children")
		}
		for _, v := range c {
			name, ok := v.(string)
			if !ok {
				return problem("unsupported module child")
			}
			n, e := rawNode(m, name)
			if e != nil {
				return e
			}
			if e = walk(n, path+"."+name, fn); e != nil {
				return e
			}
		}
	}
	return nil
}
func fillContact(m Model, c Contact) error {
	if e := validatePrivateContact(c); e != nil {
		return e
	}
	root := "checkout.pickupContact"
	n, e := rawNode(m, root)
	if e != nil {
		return problem("pickup contact missing")
	}
	values := map[string]string{"firstName": c.FirstName, "lastName": c.LastName, "emailAddress": c.Email, "mobilePhone": c.Phone, "fullDaytimePhone": c.Phone}
	counts := map[string]int{}
	type update struct {
		data       Model
		key, value string
	}
	var updates []update
	e = walk(n, root, func(n Model, p string) error {
		d, _ := rawNode(n, "d")
		b, _ := rawNode(n, "b")
		for k, v := range values {
			if _, ok := d[k]; ok {
				if _, ok := b[k]; !ok {
					return problem("contact binding missing")
				}
				if _, valid := object(b[k]); !valid {
					return problem("contact binding unsupported")
				}
				updates = append(updates, update{d, k, v})
				field := k
				if k == "fullDaytimePhone" {
					field = "mobilePhone"
				}
				counts[field]++
			}
		}
		return nil
	})
	if e != nil {
		return problem("unsupported contact model")
	}
	for k := range values {
		if k == "fullDaytimePhone" {
			continue
		}
		if counts[k] != 1 {
			return problem("contact field missing or ambiguous")
		}
	}
	if raw, exists := lookup(n, "selfPickupContact.nationalIdSelf"); exists {
		id, valid := object(raw)
		if !valid {
			return problem("pickup identification model unsupported")
		}
		d, err := rawNode(id, "d")
		if err != nil {
			return problem("pickup identification model unsupported")
		}
		enabled, known := d["enabled"].(bool)
		if !known {
			return problem("pickup identification enabled state unsupported")
		}
		if enabled {
			if !validIDLast4(c.IDLast4) {
				return problem("pickup identification suffix required")
			}
			bindingCount := 0
			if err := walk(n, root, func(node Model, path string) error {
				b, _ := rawNode(node, "b")
				if raw, exists := b["nationalIdSelf"]; exists {
					bindingCount++
					binding, ok := object(raw)
					if !ok || path != root+".selfPickupContact.nationalIdSelf" || binding["key"] != "d.nationalIdSelf" {
						return problem("pickup identification binding unsupported")
					}
				}
				return nil
			}); err != nil || bindingCount != 1 {
				return problem("pickup identification binding missing or ambiguous")
			}
			if _, exists := d["nationalIdSelf"]; !exists {
				return problem("pickup identification field missing")
			}
			updates = append(updates, update{d, "nationalIdSelf", c.IDLast4})
		}
	}
	for _, u := range updates {
		u.data[u.key] = u.value
	}
	return nil
}
func chooseSlot(m Model) error {
	p := "checkout.fulfillment.pickupTab.pickup"
	n, e := rawNode(m, p)
	if e != nil {
		return problem("pickup module unavailable")
	}
	c, ok := n["c"].([]any)
	if !ok {
		return problem("pickup children unsupported")
	}
	has := false
	for _, v := range c {
		has = has || v == "timeSlot"
	}
	if !has {
		return nil
	}
	d, e := rawNode(n, "timeSlot.dateTimeSlots.d")
	if e != nil {
		return problem("timeslot model missing")
	}
	windows, ok := d["timeSlotWindows"].([]any)
	if !ok {
		return problem("timeslot windows unsupported")
	}
	var chosen Model
	for _, day := range windows {
		dm, ok := object(day)
		if !ok {
			return problem("timeslot day unsupported")
		}
		for _, raw := range dm {
			slots, ok := raw.([]any)
			if !ok {
				return problem("timeslot list unsupported")
			}
			for _, raw := range slots {
				slot, ok := object(raw)
				if !ok {
					return problem("timeslot entry unsupported")
				}
				v, ok := slot["timeSlotValue"].(string)
				restricted, knownRestriction := slot["isRestricted"]
				if ok && v != "" && knownRestriction && (restricted == nil || restricted == false) && chosen == nil {
					chosen = slot
				}
			}
		}
	}
	if chosen == nil {
		return problem("no pickup timeslot")
	}
	value := chosen["timeSlotValue"].(string)
	day := strings.Split(value, "-")[0]
	dates, ok := d["pickUpDates"].([]any)
	if !ok {
		return problem("pickup dates unavailable")
	}
	date := ""
	for _, raw := range dates {
		dm, ok := object(raw)
		if ok && fmt.Sprint(dm["dayOfMonth"]) == day {
			date, _ = dm["date"].(string)
		}
	}
	if date == "" {
		return problem("timeslot date mismatch")
	}
	// Validate the complete selection before updating the authoritative model.
	// Native CN slots omit display fields and timeSlotType; the checkout UI
	// maps those optional values to empty fields and preserves a null restriction.
	updates := Model{
		"dayRadio": day, "date": date,
		"isRecommended": chosen["recommendationLabel"] != "" && chosen["recommendationLabel"] != nil,
		"isRestricted":  chosen["isRestricted"],
	}
	required := map[string]string{"timeSlotValue": "timeSlotValue", "endTime": "checkInEnd", "startTime": "checkInStart", "signKey": "signKey", "timeZone": "timeZone", "timeSlotId": "SlotId"}
	for dst, src := range required {
		v, ok := chosen[src].(string)
		if !ok || strings.TrimSpace(v) == "" {
			return problem("timeslot fields missing")
		}
		updates[dst] = v
	}
	optional := map[string]string{"timeSlotType": "timeSlotType", "displayStartTime": "displayStart", "displayEndTime": "displayEnd"}
	for dst, src := range optional {
		v := ""
		if raw, exists := chosen[src]; exists && raw != nil {
			var ok bool
			v, ok = raw.(string)
			if !ok {
				return problem("timeslot optional fields unsupported")
			}
		}
		updates[dst] = v
	}
	for key, value := range updates {
		d[key] = value
	}
	return nil
}
func acceptTerms(m Model) error {
	n, e := rawNode(m, "checkout.review.placeOrder.termsAndConditions")
	if e != nil {
		return problem("terms unavailable")
	}
	count := 0
	e = walk(n, "terms", func(n Model, _ string) error {
		d, _ := rawNode(n, "d")
		b, _ := rawNode(n, "b")
		if d["shouldShowCheckbox"] == true {
			if _, ok := b["termsCheckbox"]; !ok {
				return problem("unsupported terms")
			}
			d["termsCheckbox"] = true
			count++
		}
		return nil
	})
	if e != nil || count == 0 {
		return problem("required terms unavailable")
	}
	return nil
}

type storedReceipt struct {
	Identity string `json:"identity"`
	Result   Result `json:"result"`
}

func identity(t Target) string {
	b, _ := json.Marshal(struct {
		SKU, Store, Province, City, District, Payment string
		Cap                                           int64
		Contact                                       Contact
	}{t.SKU, t.StoreID, t.Province, t.City, t.District, t.Payment, t.MaxTotalCents, t.Contact})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (r *Runner) lock() (*os.File, error) {
	if e := durableStateDirectory(r.config.StateDir); e != nil {
		return nil, problem("state directory unavailable")
	}
	st, e := os.Stat(r.config.StateDir)
	if e != nil || st.Mode().Perm()&0077 != 0 {
		return nil, problem("state directory must be private")
	}
	f, e := acquireLock(filepath.Join(r.config.StateDir, "checkout.lock"))
	if e != nil {
		return nil, problem("another checkout is active or lock unavailable")
	}

	return f, nil
}
func saveReceipt(path string, v storedReceipt, exclusive bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if !exclusive {
		path += ".next"
	}
	f, e := os.OpenFile(path, flags, 0600)
	if e != nil {
		return problem("durable receipt unavailable")
	}
	b, _ := json.Marshal(v)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return problem("durable receipt write failed")
	}
	if !exclusive {
		if os.Rename(path, strings.TrimSuffix(path, ".next")) != nil {
			return problem("durable receipt replace failed")
		}
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return problem("durable directory unavailable")
	}
	defer d.Close()
	if d.Sync() != nil {
		return problem("durable directory sync failed")
	}
	return nil
}

func skuTitle(s string) string {
	return map[string]string{"MK2N4CH/A": "iPhone Duo 256GB 夜空色", "MK2M4CH/A": "iPhone Duo 256GB 星光白色", "MK2Q4CH/A": "iPhone Duo 512GB 夜空色", "MK2P4CH/A": "iPhone Duo 512GB 星光白色"}[s]
}
func supportedSKU(s string) bool { return skuTitle(s) != "" }

// activeOrderNumber never treats retained, inactive model modules as a result.
func activeOrderNumber(m Model) (string, error) {
	path := ""
	switch phase(m) {
	case "routeOut":
		path = "checkout.routeOut"
	case "poi":
		poi, e := rawNode(m, "checkout.poi")
		if e != nil {
			return "", problem("active result missing")
		}
		children, ok := poi["c"].([]any)
		if !ok || len(children) != 1 || children[0] != "routeOut" {
			return "", problem("ambiguous active result")
		}
		path = "checkout.poi.routeOut"
	default:
		return "", problem("active order confirmation missing")
	}
	d, e := rawNode(m, path+".d")
	if e != nil {
		return "", problem("active result missing")
	}
	number, ok := d["orderNumber"].(string)
	if !ok || !regexp.MustCompile(`^[A-Za-z0-9-]{5,40}$`).MatchString(number) {
		return "", problem("active order number missing")
	}
	return number, nil
}

// Persist directory entries before any network mutation, including every newly
// created ancestor. This prevents a durable receipt living in a lost directory.
func durableStateDirectory(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return problem("state directory unavailable")
	}
	missing := []string{}
	cursor := path
	for {
		_, e := os.Stat(cursor)
		if e == nil {
			break
		}
		if !os.IsNotExist(e) {
			return problem("state directory unavailable")
		}
		missing = append(missing, cursor)
		parent := filepath.Dir(cursor)
		if parent == cursor {
			return problem("state directory unavailable")
		}
		cursor = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if e := os.Mkdir(missing[i], 0700); e != nil && !os.IsExist(e) {
			return problem("state directory creation failed")
		}
		if e := syncDirectory(missing[i]); e != nil {
			return e
		}
		if e := syncDirectory(filepath.Dir(missing[i])); e != nil {
			return e
		}
	}
	return syncDirectory(path)
}
func syncDirectory(path string) error {
	d, e := os.Open(path)
	if e != nil {
		return problem("durable directory unavailable")
	}
	defer d.Close()
	if d.Sync() != nil {
		return problem("durable directory sync failed")
	}
	return nil
}
