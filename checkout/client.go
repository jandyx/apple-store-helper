package checkout

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func problem(s string) error { return errors.New("checkout: " + s) }
func (c Config) Validate() error {
	if runtime.GOOS == "windows" {
		return problem("API checkout unsupported on Windows pending native security validation")
	}
	if c.SessionFile == "" || c.ContactFile == "" || c.StateDir == "" || c.SKU == "" || len(c.StoreIDs) == 0 || c.Province == "" || c.City == "" || c.District == "" || c.MaxTotalCents <= 0 {
		return problem("invalid configuration")
	}
	if c.Payment != "WECHAT" && c.Payment != "ALIPAY" {
		return problem("unsupported payment option")
	}
	if !supportedSKU(c.SKU) {
		return problem("invalid product")
	}
	seen := map[string]bool{}
	for _, s := range c.StoreIDs {
		if s == "" || seen[s] {
			return problem("invalid stores")
		}
		seen[s] = true
	}
	return nil
}
func privateJSON(path string, dst any) error {
	f, e := os.Open(path)
	if e != nil {
		return problem("private file unavailable")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 1<<20 {
		return problem("private file permissions or size invalid")
	}
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return problem("invalid private JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return problem("invalid private JSON")
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	var c Config
	if e := privateJSON(path, &c); e != nil {
		return c, e
	}
	base := filepath.Dir(path)
	if c.ContactFile == "" {
		var err error
		c.ContactFile, err = DefaultContactPath()
		if err != nil {
			return c, err
		}
	}
	for _, p := range []*string{&c.SessionFile, &c.ContactFile, &c.StateDir} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	return c, c.Validate()
}
func LoadContact(path string) (Contact, error) {
	var c Contact
	if e := privateJSON(path, &c); e != nil {
		return c, e
	}
	return c, validatePrivateContact(c)
}
func validatePrivateContact(c Contact) error {
	for _, s := range []string{c.FirstName, c.LastName, c.Email, c.Phone} {
		if strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\r\n\x00") {
			return problem("invalid pickup contact")
		}
	}
	if !strings.Contains(c.Email, "@") {
		return problem("invalid pickup contact")
	}
	if c.IDLast4 != "" && !validIDLast4(c.IDLast4) {
		return problem("invalid pickup identification suffix")
	}
	return nil
}

func validIDLast4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := range s {
		b := s[i]
		if !(b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z') {
			return false
		}
	}
	return true
}
func LoadSession(path string) (Session, error) {
	var s Session
	if e := privateJSON(path, &s); e != nil {
		return s, e
	}
	if _, e := sessionClient(s); e != nil {
		return s, e
	}
	return s, nil
}

var secureAppleHost = regexp.MustCompile(`^secure[0-9]*\.www\.apple\.com\.cn$`)

func appleHost(h string) bool {
	return h == "www.apple.com.cn" || h == "secure.store.apple.com.cn" || h == "secure1.store.apple.com.cn" || h == "secure2.store.apple.com.cn" || secureAppleHost.MatchString(h)
}
func appleURL(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && appleHost(u.Hostname())
}
func sessionClient(s Session) (*http.Client, error) {
	if s.UserAgent == "" || strings.ContainsAny(s.UserAgent, "\r\n") || len(s.Cookies) == 0 {
		return nil, problem("invalid exported session")
	}
	jar, _ := cookiejar.New(nil)
	for _, c := range s.Cookies {
		domain := strings.TrimPrefix(c.Domain, ".")
		if domain != "apple.com.cn" && !appleHost(domain) {
			return nil, problem("invalid cookie scope")
		}
		if c.Name == "" || !c.Secure || !strings.HasPrefix(c.Path, "/") {
			return nil, problem("invalid exported cookie")
		}
		cookieDomain := ""
		if strings.HasPrefix(c.Domain, ".") {
			cookieDomain = c.Domain
		}
		hc := &http.Cookie{Name: c.Name, Value: c.Value, Domain: cookieDomain, Path: c.Path, Secure: c.Secure, HttpOnly: c.HTTPOnly}
		if hc.Valid() != nil {
			return nil, problem("invalid exported cookie")
		}
		if c.Expires > 0 {
			hc.Expires = time.Unix(c.Expires, 0)
		}
		u := &url.URL{Scheme: "https", Host: domain, Path: "/"}
		jar.SetCookies(u, []*http.Cookie{hc})
	}
	usable := false
	for _, c := range s.Cookies {
		u := &url.URL{Scheme: "https", Host: strings.TrimPrefix(c.Domain, "."), Path: c.Path}
		if len(jar.Cookies(u)) > 0 {
			usable = true
			break
		}
	}
	if !usable {
		return nil, problem("exported session has no usable cookies")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	return &http.Client{Transport: transport, Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !appleURL(req.URL) || len(via) > 5 {
			return problem("redirect refused")
		}
		return http.ErrUseLastResponse
	}}, nil
}
func decodeObject(b []byte) (Model, error) {
	var m Model
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&m) != nil || m == nil {
		return nil, problem("invalid response model")
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return nil, problem("invalid response model")
	}
	return m, nil
}
