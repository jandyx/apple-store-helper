package services

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/golang-module/carbon"
	"github.com/tidwall/gjson"

	"apple-store-helper/model"
	"apple-store-helper/notify"
	"apple-store-helper/theme"
	"apple-store-helper/view"
)

const (
	StatusOutStock = "无货"
	StatusInStock  = "有货"
	StatusWait     = "等待"
	// StatusFailed 本轮没拿到该项结果（被拦截、超时或接口返回异常），不能当作无货
	StatusFailed = "查询失败"
	// StatusStoreClosed 开售前 Apple 会把整个商店切到预购封面页，所有接口返回 503，开放后自动恢复
	StatusStoreClosed = "商店维护中"

	Pause   = "暂停"
	Running = "监听中"

	DefaultIntervalMs = 3000
	// MinIntervalMs 自定义间隔下限，避免请求过密被风控
	MinIntervalMs  = 300
	blockedBackoff = 5 * time.Second
	// maxPartsPerRequest Apple pickup-message 单次请求最多返回的型号数
	maxPartsPerRequest = 20
	// 暂停或无监听项时用短间隔检查状态，保证点击“开始”后立即生效
	idleInterval = 200 * time.Millisecond
)

// IntervalOptions 可选的查询间隔（毫秒）。2 秒以内较激进，容易触发 Apple 风控（被拦截后会停 5 秒）；30/60 秒适合长期挂机
var IntervalOptions = []int{500, 1000, 2000, 3000, 5000, 10000, 30000, 60000}

// IntervalLabel 毫秒数转下拉框文案
func IntervalLabel(ms int) string {
	label := fmt.Sprintf("%g 秒", float64(ms)/1000)
	switch {
	case ms == DefaultIntervalMs:
		label += " (默认)"
	case ms < 2000:
		label += " (易被风控)"
	}
	return label
}

// IntervalLabels 下拉框选项
func IntervalLabels() []string {
	labels := make([]string, 0, len(IntervalOptions))
	for _, ms := range IntervalOptions {
		labels = append(labels, IntervalLabel(ms))
	}
	return labels
}

var intervalNumberRe = regexp.MustCompile(`\d+(\.\d+)?`)

// ParseInterval 解析下拉框文案或自定义输入，如 "1.5"、"1.5 秒"、"800ms"、"800 毫秒"；
// 默认单位为秒，低于 MinIntervalMs 时取下限
func ParseInterval(text string) (int, bool) {
	text = strings.TrimSpace(text)
	for _, ms := range IntervalOptions {
		if IntervalLabel(ms) == text {
			return ms, true
		}
	}

	number := intervalNumberRe.FindString(text)
	if number == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, false
	}

	lower := strings.ToLower(text)
	ms := int(value * 1000)
	if strings.Contains(lower, "ms") || strings.Contains(text, "毫秒") {
		ms = int(value)
	}
	if ms < MinIntervalMs {
		ms = MinIntervalMs
	}
	return ms, true
}

// ParseIntervalLabel 同 ParseInterval，无法识别时回退默认值
func ParseIntervalLabel(label string) int {
	if ms, ok := ParseInterval(label); ok {
		return ms
	}
	return DefaultIntervalMs
}

var Listen = listenService{
	items:      map[string]ListenItem{},
	Status:     binding.NewString(),
	Area:       model.Areas[0],
	Logs:       widget.NewLabel(""),
	IntervalMs: binding.NewInt(),
}

type listenService struct {
	// mu 保护 items：界面线程（添加 / 清空）与监听协程会同时读写
	mu     sync.Mutex
	items  map[string]ListenItem
	Status binding.String
	Area   model.Area
	Logs   *widget.Label
	// PushUrls 手机推送地址，每行一个，渠道按域名自动识别
	PushUrls   string
	IntervalMs binding.Int
}

type ListenItem struct {
	Store   model.Store
	Product model.Product
	Status  string
	Time    carbon.DateTime
}

func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string) {

	store := Store.GetStore(areaTitle, storeTitle)
	product := Product.GetProduct(areaTitle, productTitle)

	uniqKey := store.StoreNumber + "." + product.Code

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.items[uniqKey].Store.StoreNumber == "" {
		s.items[uniqKey] = ListenItem{
			Store:   store,
			Product: product,
			Status:  StatusWait,
		}
	}

	s.updateLogStrLocked()
}

func (s *listenService) Clean() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]ListenItem{}
	s.updateLogStrLocked()
}

func (s *listenService) SetListenItems(items map[string]ListenItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]ListenItem{}
	for key, item := range items {
		s.items[key] = item
	}
	s.updateLogStrLocked()
}

// GetListenItems 返回监听项的副本，调用方可放心遍历或序列化
func (s *listenService) GetListenItems() map[string]ListenItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make(map[string]ListenItem, len(s.items))
	for key, item := range s.items {
		items[key] = item
	}
	return items
}

func (s *listenService) UpdateLogStr() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateLogStrLocked()
}

// sortedKeysLocked 按门店、型号排序，避免列表每轮刷新顺序乱跳
func (s *listenService) sortedKeysLocked() []string {
	keys := make([]string, 0, len(s.items))
	for key := range s.items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.items[keys[i]], s.items[keys[j]]
		if a.Store.CityStoreName != b.Store.CityStoreName {
			return a.Store.CityStoreName < b.Store.CityStoreName
		}
		return a.Product.Title < b.Product.Title
	})
	return keys
}

func (s *listenService) updateLogStrLocked() {
	var str string

	for _, key := range s.sortedKeysLocked() {
		item := s.items[key]

		str += fmt.Sprintf(
			"[%s] %s %s %s %s",
			item.Status,
			item.Time,
			item.Store.CityStoreName,
			item.Product.Title,
			"\n",
		)
	}

	s.Logs.SetText(str)
}

func (s *listenService) UpdateStatus(uniqKey string, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateStatusLocked(uniqKey, status)
}

func (s *listenService) updateStatusLocked(uniqKey string, status string) {
	item, exists := s.items[uniqKey]
	if !exists {
		// 查询期间被清空或删除的项不再写回
		return
	}
	item.Time = carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}
	item.Status = status
	s.items[uniqKey] = item
}

// IntervalDuration 当前查询间隔，未设置或非法时回退默认值
func (s *listenService) IntervalDuration() time.Duration {
	ms, err := s.IntervalMs.Get()
	if err != nil || ms <= 0 {
		ms = DefaultIntervalMs
	}
	return time.Duration(ms) * time.Millisecond
}

func (s *listenService) Run() {
	s.Status.Set(Pause)

	go func() {
		for {
			items := s.GetListenItems()
			if stats, ok := s.Status.Get(); ok == nil && stats == Running && len(items) > 0 {
				result := s.groupByStore(items)

				// 同一轮可能多项同时有货，全部收集后一起提醒，避免漏报
				failedStatus := StatusFailed
				if result.storeClosed {
					failedStatus = StatusStoreClosed
				}

				var hits []string
				s.mu.Lock()
				for _, key := range s.sortedKeysLocked() {
					item := s.items[key]
					status, ok := result.skus[item.Store.StoreNumber+"."+item.Product.Code]
					switch {
					case !ok:
						s.updateStatusLocked(key, failedStatus)
					case status:
						s.updateStatusLocked(key, StatusInStock)
						hits = append(hits, fmt.Sprintf("%s %s", item.Store.CityStoreName, item.Product.Title))
					default:
						s.updateStatusLocked(key, StatusOutStock)
					}
				}
				s.updateLogStrLocked()
				s.mu.Unlock()

				if len(hits) > 0 {
					s.Status.Set(Pause)
					s.alertInStock(hits)
				}

				interval := s.IntervalDuration()
				// 商店封面期间不退避，开放后第一时间查到；其他拦截多半是限流，放慢一点
				if result.blocked && !result.storeClosed && interval < blockedBackoff {
					interval = blockedBackoff
				}
				time.Sleep(interval)
				continue
			}

			time.Sleep(idleInterval)
		}
	}()
}

type skuFetch struct {
	skus    map[string]bool
	blocked bool
	// storeClosed 命中了预购前的商店封面页
	storeClosed bool
}

var storeCoverRe = regexp.MustCompile(`"cv"\s*:\s*"preorder"`)

// alertInStock 打开购物车并通过弹窗、系统通知、提示音、Bark 提醒
func (s *listenService) alertInStock(hits []string) {
	bagUrl := s.BagURL()
	s.openBrowser(bagUrl)

	msg := strings.Join(hits, "\n") + "\n有货"
	title := "有货提醒"
	if len(hits) > 1 {
		title = fmt.Sprintf("有货提醒（%d 项）", len(hits))
	}
	dialog.ShowInformation("匹配成功", msg, view.Window)
	notify.Send(title, msg)
	go s.AlertMp3()
	go s.SendPush(title, msg, bagUrl)
}

func (s *listenService) groupByStore(items map[string]ListenItem) skuFetch {
	result := skuFetch{skus: map[string]bool{}}

	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	group := map[string][]ListenItem{}
	for _, item := range items {
		group[item.Store.StoreNumber] = append(group[item.Store.StoreNumber], item)
	}

	// pickup-message 每次最多返回 maxPartsPerRequest 个型号，超出的分批请求
	type batch struct {
		storeNumber string
		items       []ListenItem
	}
	var batches []batch
	for storeNumber, items := range group {
		for start := 0; start < len(items); start += maxPartsPerRequest {
			end := min(start+maxPartsPerRequest, len(items))
			batches = append(batches, batch{storeNumber, items[start:end]})
		}
	}

	count := len(batches)
	if count < 1 {
		return result
	}

	ch := make(chan skuFetch, count)
	for _, b := range batches {
		link := s.fulfillmentURL(b.storeNumber, b.items)
		referer := productBuyURL(s.Area, b.items[0].Product)
		go s.getSkuByLink(ch, link, referer)
	}

	for i := 0; i < count; i++ {
		part := <-ch
		if part.blocked {
			result.blocked = true
		}
		if part.storeClosed {
			result.storeClosed = true
		}
		for key, v := range part.skus {
			result.skus[key] = v
		}
	}

	return result
}

func (s *listenService) fulfillmentURL(storeNumber string, items []ListenItem) string {
	var b strings.Builder
	b.WriteString(s.Area.ShopOrigin())
	b.WriteString("/shop/retail/pickup-message?little=true&store=")
	b.WriteString(storeNumber)

	for index, item := range items {
		b.WriteString("&parts.")
		b.WriteString(strconv.Itoa(index))
		b.WriteByte('=')
		b.WriteString(item.Product.Code)
	}

	return b.String()
}

func familyPath(familyType string) string {
	switch familyType {
	case "iphone18pro", "iphone18promax":
		return "iphone-18-pro"
	case "iphoneduo":
		return "iphone-duo"
	case "iphoneair":
		return "iphone-air"
	case "iphone17":
		return "iphone-17"
	default:
		if strings.HasPrefix(familyType, "iphone") {
			return "iphone-" + strings.TrimPrefix(familyType, "iphone")
		}
		return familyType
	}
}

func productBuyURL(area model.Area, product model.Product) string {
	return fmt.Sprintf("%s/shop/buy-iphone/%s/%s", area.ShopOrigin(), familyPath(product.Type), strings.ToLower(product.Code))
}

func fulfillmentAvailable(availability gjson.Result) bool {
	if availability.Get("pickupDisplay").String() == "available" {
		return true
	}
	if availability.Get("messageTypes.regular.storeSelectionEnabled").Bool() {
		return true
	}
	return availability.Get("messageTypes.compact.storeSelectionEnabled").Bool()
}

func (s *listenService) getSkuByLink(ch chan skuFetch, skUrl string, referer string) {
	result := skuFetch{skus: map[string]bool{}}

	warmupShopPage(referer, s.Area.AcceptLanguage())

	resp, err := shopHTTP().R().
		SetHeader("accept", "*/*").
		SetHeader("accept-language", s.Area.AcceptLanguage()).
		SetHeader("cache-control", "no-cache").
		SetHeader("pragma", "no-cache").
		SetHeader("referer", referer).
		SetHeader("sec-fetch-dest", "empty").
		SetHeader("sec-fetch-mode", "cors").
		SetHeader("sec-fetch-site", "same-origin").
		Get(skUrl)
	if err != nil {
		log.Println(err)
		ch <- result
		return
	}

	body := resp.String()
	log.Println(resp.GetStatusCode(), skUrl)

	if resp.GetStatusCode() == 541 || resp.GetStatusCode() >= 400 {
		result.blocked = true
		result.storeClosed = storeCoverRe.MatchString(body)
		resetShopSession()
		preview := body
		if len(preview) > 160 {
			preview = preview[:160]
		}
		log.Println("fulfillment blocked", preview)
		ch <- result
		return
	}

	stores := gjson.Get(body, "body.stores")
	if !stores.Exists() || stores.Get("#").Int() == 0 {
		stores = gjson.Get(body, "body.content.pickupMessage.stores")
	}
	if !stores.Exists() || stores.Get("#").Int() == 0 {
		preview := body
		if len(preview) > 200 {
			preview = preview[:200]
		}
		log.Println("fulfillment unexpected body", preview)
		ch <- result
		return
	}

	for _, store := range stores.Array() {
		for productCode, availability := range store.Get("partsAvailability").Map() {
			uniqKey := fmt.Sprintf("%s.%s", store.Get("storeNumber").String(), productCode)
			result.skus[uniqKey] = fulfillmentAvailable(availability)
		}
	}

	ch <- result
}

// 型号对应预约地址
//func (s *listenService) model2Url(productType string) string {
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16
//	// https://www.apple.com.cn/shop/buy-iphone/iphone-16-pro
//
//	var t string
//	switch productType {
//	case "iphone16promax", "iphone16pro":
//		t = "iphone-16-pro"
//	case "iphone16":
//		t = "iphone-16"
//	}
//
//	return fmt.Sprintf(
//		"https://www.apple.com/%s/shop/buy-iphone/%s",
//		s.Area.ShortCode,
//		t,
//	)
//}

// BagURL 当前地区购物袋地址
func (s *listenService) BagURL() string {
	return fmt.Sprintf("%s/shop/bag", s.Area.ShopOrigin())
}

// OpenBag 用默认浏览器打开当前地区购物袋，与有货时的行为一致
func (s *listenService) OpenBag() {
	s.openBrowser(s.BagURL())
}

func (s *listenService) openBrowser(link string) {
	parse, err := url.Parse(link)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}

	err = view.App.OpenURL(parse)
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
}

func (s *listenService) AlertMp3() {
	reader := bytes.NewReader(theme.Mp3().Content())
	streamer, _, err := mp3.Decode(ioutil.NopCloser(reader))
	if err != nil {
		panic(err)
	}
	defer streamer.Close()

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}

// SendPush 推送到所有已配置的手机推送地址。失败只记日志，不影响弹窗、提示音和打开购物车
func (s *listenService) SendPush(title string, content string, link string) []PushResult {
	results := Push(s.PushUrls, PushMessage{Title: title, Content: content, URL: link})
	for _, r := range results {
		if r.Err != nil {
			log.Printf("推送失败 [%s] %s: %v", r.Channel, MaskTarget(r.Target), r.Err)
		}
	}
	return results
}
