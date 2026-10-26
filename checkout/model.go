package checkout

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

func object(v any) (Model, bool) {
	switch x := v.(type) {
	case Model:
		return x, true
	case map[string]any:
		return Model(x), true
	}
	return nil, false
}
func lookup(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, key := range strings.Split(path, ".") {
		m, ok := object(v)
		if !ok {
			return nil, false
		}
		v, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return v, true
}
func rawNode(m Model, p string) (Model, error) {
	v, ok := lookup(m, p)
	n, yes := object(v)
	if !ok || !yes {
		return nil, fmt.Errorf("unsupported model: missing module %s", p)
	}
	return n, nil
}
func clone(v any) any {
	if m, ok := object(v); ok {
		r := Model{}
		for k, x := range m {
			r[k] = clone(x)
		}
		return r
	}
	if a, ok := v.([]any); ok {
		r := make([]any, len(a))
		for i, x := range a {
			r[i] = clone(x)
		}
		return r
	}
	return v
}
func resolve(scope, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty model reference")
	}
	switch ref[0] {
	case '/':
		return ref[1:], nil
	case '^':
		if i := strings.LastIndex(scope, "."); i >= 0 {
			scope = scope[:i]
		} else {
			scope = ""
		}
		ref = ref[1:]
	case '~':
		ref = ref[1:]
	}
	return strings.Trim(scope+"."+ref, "."), nil
}

// cachedRecommendationFault recognizes only the observed inert data-loader
// metadata. Serialization uses the already-present model; it never loads this URL.
func cachedRecommendationFault(n Model, path string, f Model) bool {
	if path != "shoppingCart.recommendations.recommendedItem" || len(f) != 3 ||
		f["abstract"] != "/abs.RecommendedItem" || f["type"] != "data" {
		return false
	}
	raw, ok := f["url"].(string)
	if !ok || !strings.HasPrefix(raw, "/shop/recommendedForYou-full?") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil ||
		u.Fragment != "" || u.Path != "/shop/recommendedForYou-full" || u.RawPath != "" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 3 {
		return false
	}
	for _, key := range []string{"partsInCart.0", "inline", "recentAddedPart"} {
		if len(query[key]) != 1 {
			return false
		}
	}
	for _, key := range []string{"p", "b"} {
		if raw, exists := n[key]; exists {
			v, ok := object(raw)
			if !ok || len(v) != 0 {
				return false
			}
		}
	}
	if raw, exists := n["c"]; exists {
		c, ok := raw.([]any)
		if !ok || len(c) != 0 {
			return false
		}
	}
	d, ok := object(n["d"])
	if !ok || len(d) != 2 {
		return false
	}
	_, id := d["id"]
	_, part := d["part"]
	return id && part
}

func Node(m Model, path string) (Model, error) {
	n, err := rawNode(m, path)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"f", "a", "b", "d", "p", "r", "t", "meta"} {
		if v, exists := n[key]; exists {
			if _, ok := object(v); !ok {
				return nil, fmt.Errorf("invalid module property %s", key)
			}
		}
	}
	n = clone(n).(Model)
	f, _ := object(n["f"])
	cachedRecommendation := cachedRecommendationFault(n, path, f)
	if v, exists := f["abstract"]; exists {
		if ref, ok := v.(string); !ok || ref == "" {
			return nil, fmt.Errorf("invalid abstract reference")
		}
	}
	if ref, ok := f["abstract"].(string); ok && ref != "" {
		p, e := resolve(path, ref)
		if e != nil {
			return nil, e
		}
		t, e := rawNode(m, p)
		if e != nil {
			return nil, e
		}
		if tf, _ := object(t["f"]); len(tf) > 0 {
			return nil, fmt.Errorf("unsupported nested abstract")
		}
		for _, k := range []string{"a", "b", "d", "p", "r", "t"} {
			merged := Model{}
			for _, source := range []any{t[k], n[k]} {
				if source == nil {
					continue
				}
				o, ok := object(source)
				if !ok {
					return nil, fmt.Errorf("invalid abstract property")
				}
				for key, v := range o {
					merged[key] = clone(v)
				}
			}
			n[k] = merged
		}
		for _, k := range []string{"c", "m"} {
			var a []any
			for _, source := range []any{t[k], n[k]} {
				if source == nil {
					continue
				}
				v, ok := source.([]any)
				if !ok {
					return nil, fmt.Errorf("invalid abstract list")
				}
				a = append(a, v...)
			}
			n[k] = a
		}
	}
	for k, v := range f {
		if cachedRecommendation && (k == "type" || k == "url") {
			continue
		}
		if k != "abstract" && truth(v) {
			return nil, fmt.Errorf("unsupported module fault %s", k)
		}
	}
	return n, nil
}
func SetData(m Model, path, key string, value any) error {
	n, err := rawNode(m, path)
	if err != nil {
		return err
	}
	d, ok := object(n["d"])
	if !ok {
		return fmt.Errorf("missing data at %s", path)
	}
	if _, ok := d[key]; !ok {
		return fmt.Errorf("missing data field %s.%s", path, key)
	}
	d[key] = value
	return nil
}
func setProperty(m Model, path string, value any, create bool) error {
	keys := strings.Split(path, ".")
	n := m
	for _, k := range keys[:len(keys)-1] {
		v, ok := object(n[k])
		if !ok {
			if !create || n[k] != nil {
				return fmt.Errorf("missing property parent %s", path)
			}
			v = Model{}
			n[k] = v
		}
		n = v
	}
	n[keys[len(keys)-1]] = clone(value)
	return nil
}
func stringList(v any) ([]string, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid model list")
	}
	r := []string{}
	seen := map[string]bool{}
	for _, v := range a {
		s, ok := v.(string)
		if !ok || s == "" || strings.Contains(s, ".") || seen[s] {
			return nil, fmt.Errorf("invalid or duplicate child")
		}
		seen[s] = true
		r = append(r, s)
	}
	return r, nil
}
func ApplyResponse(m, body Model) (Model, error) {
	r := clone(m).(Model)
	meta, ok := object(body["meta"])
	if !ok {
		return nil, fmt.Errorf("missing response metadata")
	}
	directives := false
	for _, op := range []string{"x", "l"} {
		v, exists := meta[op]
		if !exists {
			continue
		}
		directives = true
		a, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid directive %s", op)
		}
		for _, v := range a {
			ref, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("invalid directive reference")
			}
			p, e := resolve("", ref)
			if e != nil {
				return nil, e
			}
			i := strings.LastIndex(p, ".")
			parent, name := "", p
			if i >= 0 {
				parent, name = p[:i], p[i+1:]
			}
			pn, e := rawNode(r, parent)
			if e != nil {
				return nil, e
			}
			if op == "x" {
				old, exists := lookup(r, p)
				if !exists {
					continue
				}
				persist, _ := lookup(old, "meta.shouldPersist")
				if truth(persist) {
					return nil, fmt.Errorf("unsupported runtime persistence")
				}
				delete(pn, name)
			} else {
				replacement, e := rawNode(body, p)
				if e != nil {
					return nil, e
				}
				pn[name] = clone(replacement)
			}
			if parent != "" {
				names := []string{}
				if c, exists := pn["c"]; exists {
					names, e = stringList(c)
					if e != nil {
						return nil, e
					}
				}
				out := []any{}
				found := false
				for _, s := range names {
					if s == name {
						found = true
						if op == "x" {
							continue
						}
					}
					out = append(out, s)
				}
				if op == "l" && !found {
					out = append(out, name)
				}
				pn["c"] = out
			}
		}
	}
	if u, exists := meta["u"]; exists {
		directives = true
		updates, ok := object(u)
		if !ok {
			return nil, fmt.Errorf("invalid updates")
		}
		for ref, keys := range updates {
			p, e := resolve("", ref)
			if e != nil {
				return nil, e
			}
			n, e := rawNode(r, p)
			if e != nil {
				continue
			}
			a, ok := keys.([]any)
			if !ok {
				return nil, fmt.Errorf("invalid property updates")
			}
			for _, key := range a {
				k, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid update key")
				}
				parts := strings.Split(k, ".")
				valid := len(parts) == 2 && (parts[0] == "d" || parts[0] == "p") || len(parts) == 1 && parts[0] == "m" || parts[0] == "b" && (len(parts) == 3 || len(parts) == 4 && parts[2] == "p")
				if !valid {
					return nil, fmt.Errorf("unsupported property update %s", k)
				}
				v, exists := lookup(body, p+"."+k)
				if !exists {
					return nil, fmt.Errorf("missing update value")
				}
				if parts[0] == "b" {
					if _, exists := lookup(n, "b."+parts[1]); !exists {
						continue
					}
					if len(parts) == 3 {
						k = "b." + parts[1] + ".p." + parts[2]
					}
				}
				if e := setProperty(r, p+"."+k, v, true); e != nil {
					return nil, e
				}
			}
		}
	}
	if len(body) > 1 && !directives {
		return nil, fmt.Errorf("response has no supported directives")
	}
	current, _ := object(r["meta"])
	if current == nil {
		current = Model{}
		r["meta"] = current
	}
	for k, v := range meta {
		if k == "l" || k == "u" || k == "x" {
			continue
		}
		if a, ok := object(v); ok {
			if b, ok := object(current[k]); ok {
				for key, x := range a {
					b[key] = clone(x)
				}
				continue
			}
		}
		current[k] = clone(v)
	}
	return r, nil
}
func truth(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	case int:
		return x != 0
	case int64:
		return x != 0
	case json.Number:
		n, err := x.Float64()
		return err == nil && n != 0 && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	return true
}
func jsString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []any:
		a := []string{}
		for _, v := range x {
			a = append(a, jsString(v))
		}
		return strings.Join(a, ",")
	case Model, map[string]any:
		return "[object Object]"
	}
	return fmt.Sprint(v)
}
func empty(v any) bool { return strings.TrimSpace(jsString(v)) == "" }

type form struct {
	model      Model
	evaluating map[string]bool
	depth      int
}

func newForm(m Model) *form { return &form{model: m, evaluating: map[string]bool{}} }
func bind(v, value any) (any, error) {
	if m, ok := object(v); ok {
		if len(m) == 1 {
			if x, exists := m["val"]; exists {
				if x != "_" {
					return nil, fmt.Errorf("unsupported binding placeholder")
				}
				return value, nil
			}
		}
		r := Model{}
		for k, x := range m {
			v, e := bind(x, value)
			if e != nil {
				return nil, e
			}
			r[k] = v
		}
		return r, nil
	}
	if a, ok := v.([]any); ok {
		r := []any{}
		for _, x := range a {
			v, e := bind(x, value)
			if e != nil {
				return nil, e
			}
			r = append(r, v)
		}
		return r, nil
	}
	return v, nil
}
func (f *form) guarded(id string, fn func() (any, error)) (any, error) {
	if f.evaluating[id] {
		return nil, fmt.Errorf("circular predicate %s", id)
	}
	f.evaluating[id] = true
	defer delete(f.evaluating, id)
	return fn()
}
func (f *form) property(scope, path string, def any) (any, error) {
	return f.guarded(scope+":"+path, func() (any, error) {
		n, e := Node(f.model, scope)
		if e != nil {
			return nil, e
		}
		v, ok := lookup(n, path)
		if !ok {
			v = def
		}
		return f.eval(v, scope)
	})
}
func bindingProps(n Model, name string) (Model, error) {
	v, ok := lookup(n, "b."+name)
	b, yes := object(v)
	if !ok || !yes {
		return nil, fmt.Errorf("missing binding %s", name)
	}
	r := clone(b).(Model)
	if raw, exists := b["p"]; exists {
		p, ok := object(raw)
		if !ok {
			return nil, fmt.Errorf("invalid binding properties")
		}
		for k, v := range p {
			r[k] = v
		}
	}
	return r, nil
}
func (f *form) reference(scope, ref string) (any, error) {
	prefix := ""
	raw := ref
	if len(ref) > 0 && strings.ContainsRune("/^~", rune(ref[0])) {
		prefix, raw = ref[:1], ref[1:]
	}
	parts := strings.Split(raw, ".")
	i := -1
	for j, k := range parts {
		if strings.Contains("|f|a|b|c|d|m|meta|t|p|r|", "|"+k+"|") {
			i = j
			break
		}
	}
	if i < 0 {
		return nil, fmt.Errorf("unsupported reference")
	}
	module := scope
	if i > 0 || prefix != "" {
		var e error
		module, e = resolve(scope, prefix+strings.Join(parts[:i], "."))
		if e != nil {
			return nil, e
		}
	}
	parts = parts[i:]
	n, e := Node(f.model, module)
	if e != nil {
		return nil, e
	}
	if parts[0] == "b" && len(parts) >= 3 {
		props, e := bindingProps(n, parts[1])
		if e != nil {
			return nil, e
		}
		rest := parts[2:]
		if rest[0] == "p" {
			rest = rest[1:]
		}
		path := strings.Join(rest, ".")
		return f.guarded(module+":b."+parts[1]+"."+path, func() (any, error) {
			key, _ := props["key"].(string)
			if key == "" {
				key = "d." + parts[1]
			}
			value, e := f.reference(module, key)
			if e != nil {
				return nil, e
			}
			p, ok := lookup(props, path)
			if !ok {
				p = path == "active" || path == "enabled" || path == "visible"
			}
			p, e = bind(p, value)
			if e != nil {
				return nil, e
			}
			return f.eval(p, module)
		})
	}
	path := strings.Join(parts, ".")
	if parts[0] == "p" {
		def := any("")
		if parts[len(parts)-1] == "active" || parts[len(parts)-1] == "enabled" {
			def = true
		}
		return f.property(module, path, def)
	}
	v, _ := lookup(n, path)
	return v, nil
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		n, e := x.Float64()
		return n, e == nil
	}
	return 0, false
}
func (f *form) eval(v any, scope string) (any, error) {
	f.depth++
	defer func() { f.depth-- }()
	if f.depth > 128 {
		return nil, fmt.Errorf("model expression too deep")
	}
	m, ok := object(v)
	if !ok {
		return v, nil
	}
	if cond, exists := m["if"]; exists {
		for k := range m {
			if k != "if" && k != "then" && k != "else" {
				return nil, fmt.Errorf("unsupported conditional")
			}
		}
		c, e := f.eval(cond, scope)
		if e != nil {
			return nil, e
		}
		key := "else"
		if truth(c) {
			key = "then"
		}
		return f.eval(m[key], scope)
	}
	if ref, ok := m["get"].(string); ok && len(m) == 1 {
		return f.reference(scope, ref)
	}
	op, ok := m["op"].(string)
	if !ok {
		return nil, fmt.Errorf("unsupported expression")
	}
	for k := range m {
		if k != "op" && k != "args" {
			return nil, fmt.Errorf("unsupported operator property")
		}
	}
	args := []any{}
	if raw, exists := m["args"]; exists {
		a, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid operands")
		}
		for _, x := range a {
			x, e := f.eval(x, scope)
			if e != nil {
				return nil, e
			}
			args = append(args, x)
		}
	}
	neg := strings.HasPrefix(op, "!")
	op = strings.TrimPrefix(op, "!")
	result := false
	switch op {
	case "EMPTY":
		result = true
		for _, x := range args {
			result = result && empty(x)
		}
	case "AND":
		result = true
		for _, x := range args {
			result = result && truth(x)
		}
	case "OR":
		for _, x := range args {
			result = result || truth(x)
		}
	case "NOT":
		if len(args) != 1 {
			return nil, fmt.Errorf("invalid NOT")
		}
		result = !truth(args[0])
	case "EQ":
		if len(args) == 0 {
			return nil, fmt.Errorf("invalid EQ")
		}
		result = true
		for _, x := range args {
			a, aok := number(args[0])
			b, bok := number(x)
			result = result && (reflect.DeepEqual(args[0], x) || aok && bok && a == b)
		}
	case "LENGTH":
		if len(args) != 1 || neg {
			return nil, fmt.Errorf("invalid LENGTH")
		}
		switch x := args[0].(type) {
		case string:
			return len([]rune(x)), nil
		case []any:
			return len(x), nil
		}
		if _, ok := number(args[0]); ok {
			return len(jsString(args[0])), nil
		}
		return 0, nil
	case "ADD":
		if len(args) == 0 || neg {
			return nil, fmt.Errorf("invalid ADD")
		}
		r := args[0]
		for _, x := range args[1:] {
			_, as := r.(string)
			_, bs := x.(string)
			if as || bs {
				r = jsString(r) + jsString(x)
			} else {
				a, aok := number(r)
				b, bok := number(x)
				if !aok || !bok {
					return nil, fmt.Errorf("unsupported ADD")
				}
				r = a + b
			}
		}
		return r, nil
	case "JOIN":
		if len(args) < 2 || neg {
			return nil, fmt.Errorf("invalid JOIN")
		}
		a := []string{}
		for _, x := range args[2:] {
			if !truth(args[1]) || !empty(x) {
				a = append(a, jsString(x))
			}
		}
		return strings.Join(a, jsString(args[0])), nil
	case "MATCH":
		if len(args) == 0 {
			return nil, fmt.Errorf("invalid MATCH")
		}
		pattern, ok := args[0].(string)
		if !ok || strings.Contains(pattern, "(?") || regexp.MustCompile(`\\[puk]`).MatchString(pattern) {
			return nil, fmt.Errorf("unsupported regexp")
		}
		re, e := regexp.Compile(pattern)
		if e != nil {
			return nil, e
		}
		result = true
		for _, x := range args[1:] {
			result = result && re.MatchString(jsString(x))
		}
	default:
		return nil, fmt.Errorf("unsupported operator %s", op)
	}
	if neg {
		result = !result
	}
	return result, nil
}
func SubmitFields(m Model, scope string) (url.Values, error) {
	f := newForm(m)
	out := url.Values{}
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 128 {
			return fmt.Errorf("model tree too deep")
		}
		n, e := Node(m, path)
		if e != nil {
			return e
		}
		for _, key := range []string{"wasPersisted", "wasAncestorPersisted"} {
			v, _ := lookup(n, "meta."+key)
			if truth(v) {
				return fmt.Errorf("unsupported persisted module")
			}
		}
		for _, key := range []string{"active", "enabled"} {
			v, e := f.property(path, "p."+key, true)
			if e != nil {
				return e
			}
			if !truth(v) {
				return nil
			}
		}
		b, _ := object(n["b"])
		for name := range b {
			props, e := bindingProps(n, name)
			if e != nil {
				return e
			}
			ref, _ := props["key"].(string)
			if ref == "" {
				ref = "d." + name
			}
			if !strings.HasPrefix(ref, "d.") {
				if truth(props["submit"]) {
					return fmt.Errorf("unsupported form reference")
				}
				continue
			}
			value, e := f.reference(path, ref)
			if e != nil {
				return e
			}
			skip := false
			for _, key := range []string{"submit", "active"} {
				v, ok := props[key]
				if !ok {
					v = key == "active"
				}
				v, e = bind(v, value)
				if e != nil {
					return e
				}
				v, e = f.eval(v, path)
				if e != nil {
					return e
				}
				if !truth(v) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			if empty(value) {
				value = ""
			}
			switch x := value.(type) {
			case string, bool, int, int64, json.Number:
			case float64:
				if math.IsNaN(x) || math.IsInf(x, 0) {
					return fmt.Errorf("nonfinite form value")
				}
			default:
				return fmt.Errorf("nonscalar form value")
			}
			if truth(props["encryption"]) && truth(value) {
				return fmt.Errorf("encrypted form unsupported")
			}
			out.Set(path+"."+name, jsString(value))
		}
		if c, exists := n["c"]; exists {
			// The observed pickup-contact model lists its one invoice module
			// twice. Native serialization visits that child once. Keep this
			// exception local: all other duplicate child references still fail.
			if path == "checkout.pickupContact" {
				if raw, ok := c.([]any); ok {
					unique := make([]any, 0, len(raw))
					invoiceSeen := false
					for _, child := range raw {
						if name, ok := child.(string); ok && name == "eFapiaoSelector" {
							if invoiceSeen {
								continue
							}
							invoiceSeen = true
						}
						unique = append(unique, child)
					}
					c = unique
				}
			}
			names, e := stringList(c)
			if e != nil {
				return e
			}
			for _, name := range names {
				if e := visit(path+"."+name, depth+1); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := visit(strings.TrimPrefix(scope, "/"), 0); e != nil {
		return nil, e
	}
	return out, nil
}
func BindingAction(m Model, path, binding string) (string, error) {
	f := newForm(m)
	n, e := Node(m, path)
	if e != nil {
		return "", e
	}
	for _, key := range []string{"active", "enabled"} {
		v, e := f.property(path, "p."+key, true)
		if e != nil {
			return "", e
		}
		if !truth(v) {
			return "", fmt.Errorf("inactive action module")
		}
	}
	p, e := bindingProps(n, binding)
	if e != nil {
		return "", e
	}
	for _, key := range []string{"active", "enabled"} {
		v, ok := p[key]
		if !ok {
			v = true
		}
		v, e = f.eval(v, path)
		if e != nil {
			return "", e
		}
		if !truth(v) {
			return "", fmt.Errorf("disabled action")
		}
	}
	a, ok := p["events"].([]any)
	if !ok {
		return "", fmt.Errorf("missing action events")
	}
	action := ""
	for _, v := range a {
		event, ok := object(v)
		if !ok {
			return "", fmt.Errorf("invalid action event")
		}
		on, ok := event["on"].(string)
		if !ok {
			return "", fmt.Errorf("invalid event name")
		}
		if on != "click" {
			continue
		}
		// Analytics annotations are inert: validate their bounded declarative
		// shape, but never issue a metric request or execute a metric handler.
		for key := range event {
			if key != "on" && key != "do" && key != "metric" && key != "condition" {
				return "", fmt.Errorf("unsupported click event property")
			}
		}
		metric, hasMetric := event["metric"]
		if hasMetric {
			name, ok := metric.(string)
			if !ok || !regexp.MustCompile(`^t\.[A-Za-z_$][A-Za-z0-9_$]*$`).MatchString(name) {
				return "", fmt.Errorf("unsupported analytics metric")
			}
		}
		_, hasAction := event["do"]
		if !hasAction {
			if !hasMetric {
				return "", fmt.Errorf("click event has no supported effect")
			}
			if condition, exists := event["condition"]; exists {
				value, err := f.eval(condition, path)
				if err != nil {
					return "", err
				}
				if _, ok := value.(bool); !ok {
					return "", fmt.Errorf("unsupported analytics condition")
				}
			}
			continue
		}
		// Even a false condition cannot hide a second/conditional action.
		if _, conditional := event["condition"]; conditional {
			return "", fmt.Errorf("conditional action unsupported")
		}
		do, ok := event["do"].(string)
		if !ok || !regexp.MustCompile(`^a\.[A-Za-z_$][A-Za-z0-9_$]*$`).MatchString(do) || action != "" {
			return "", fmt.Errorf("unsupported or ambiguous click event")
		}
		action = do[2:]
	}
	if action == "" {
		return "", fmt.Errorf("missing click action")
	}
	return action, nil
}
