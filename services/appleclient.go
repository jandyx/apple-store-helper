package services

import (
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"github.com/imroc/req/v3"
)

var (
	appleClientOnce sync.Once
	appleHTTP       *req.Client
	appleWarmMu     sync.Mutex
	// appleWarmedAt 已预热的商品页及时间。按页面记录，多地区、多机型同时监听时不必反复预热
	appleWarmedAt = map[string]time.Time{}
)

// resettableJar 可清空的 cookie jar。
// req 的 ClearCookies 会直接替换客户端内部的 jar 字段，与并发中的请求构成数据竞争，
// 这里改为在 jar 内部加锁替换。
type resettableJar struct {
	mu  sync.RWMutex
	jar http.CookieJar
}

func newResettableJar() *resettableJar {
	jar, _ := cookiejar.New(nil)
	return &resettableJar{jar: jar}
}

func (j *resettableJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	j.jar.SetCookies(u, cookies)
}

func (j *resettableJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.jar.Cookies(u)
}

func (j *resettableJar) Reset() {
	jar, _ := cookiejar.New(nil)
	j.mu.Lock()
	j.jar = jar
	j.mu.Unlock()
}

var appleJar = newResettableJar()

func shopHTTP() *req.Client {
	appleClientOnce.Do(func() {
		appleHTTP = req.C().
			ImpersonateChrome().
			SetCookieJar(appleJar).
			SetTimeout(12 * time.Second)
	})
	return appleHTTP
}

func warmupShopPage(pageURL, acceptLanguage string) {
	appleWarmMu.Lock()
	defer appleWarmMu.Unlock()

	if pageURL == "" {
		return
	}
	if warmed, ok := appleWarmedAt[pageURL]; ok && time.Since(warmed) < 20*time.Minute {
		return
	}

	resp, err := shopHTTP().R().
		SetHeader("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8").
		SetHeader("accept-language", acceptLanguage).
		Get(pageURL)
	if err != nil {
		log.Println("warmup shop page failed", err)
		return
	}
	if resp.GetStatusCode() >= 400 {
		log.Println("warmup shop page status", resp.GetStatusCode(), pageURL)
		return
	}

	appleWarmedAt[pageURL] = time.Now()
}

func resetShopSession() {
	appleWarmMu.Lock()
	defer appleWarmMu.Unlock()
	appleWarmedAt = map[string]time.Time{}
	appleJar.Reset()
}
