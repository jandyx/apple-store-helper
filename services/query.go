package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"apple-store-helper/model"
)

// shopOrigin 站点根地址，测试中替换成本地服务。
// 设置环境变量 ASH_SHOP_ORIGIN（如 http://127.0.0.1:8765）可把库存查询指向本地模拟服务，用于演练有货提醒
var shopOrigin = func(area model.Area) string {
	if origin := os.Getenv("ASH_SHOP_ORIGIN"); origin != "" {
		return strings.TrimRight(origin, "/") + "/" + area.ShortCode
	}
	return area.ShopOrigin()
}

// storeCoverRe 开售前 Apple 会把整个商店切到预购封面页，页面里带有这个标记
var storeCoverRe = regexp.MustCompile(`"cv"\s*:\s*"preorder"`)

// skuFetch 一次或一轮查询的结果
type skuFetch struct {
	// skus 门店号.型号 -> 是否可自提
	skus map[string]bool
	// blocked 有请求被拦截（非 200）
	blocked bool
	// failures 查询失败的门店及原因（门店号 -> 原因）
	failures map[string]string
	// closed 命中预购封面页的门店
	closed map[string]bool
}

func newSkuFetch() skuFetch {
	return skuFetch{skus: map[string]bool{}, failures: map[string]string{}, closed: map[string]bool{}}
}

func (r *skuFetch) merge(part skuFetch) {
	r.blocked = r.blocked || part.blocked
	for k, v := range part.skus {
		r.skus[k] = v
	}
	for k, v := range part.failures {
		r.failures[k] = v
	}
	for k, v := range part.closed {
		r.closed[k] = v
	}
}

// storeClosed 本轮是否有门店处于商店维护中
func (r skuFetch) storeClosed() bool {
	return len(r.closed) > 0
}

// withJitter 给间隔叠加 ±jitterRatio 的随机抖动
func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := float64(d) * jitterRatio
	return time.Duration(float64(d) - delta + rand.Float64()*2*delta)
}

// queryItems 按地区和门店合并请求，每次最多 maxPartsPerRequest 个型号，同时最多 maxConcurrentRequests 个请求
func queryItems(items []ListenItem) skuFetch {
	result := newSkuFetch()

	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	type batch struct {
		area        model.Area
		storeNumber string
		items       []ListenItem
	}

	groups := map[string][]ListenItem{}
	var order []string
	for _, item := range items {
		key := item.Area.Locale + "|" + item.Store.StoreNumber
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], item)
	}

	var batches []batch
	for _, key := range order {
		group := groups[key]
		for start := 0; start < len(group); start += maxPartsPerRequest {
			end := min(start+maxPartsPerRequest, len(group))
			batches = append(batches, batch{group[0].Area, group[0].Store.StoreNumber, group[start:end]})
		}
	}

	if len(batches) == 0 {
		return result
	}

	ch := make(chan skuFetch, len(batches))
	sem := make(chan struct{}, maxConcurrentRequests)
	for _, b := range batches {
		go func(b batch) {
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- fetchStock(b.area, b.storeNumber, b.items)
		}(b)
	}

	for range batches {
		result.merge(<-ch)
	}
	return result
}

func fulfillmentURL(area model.Area, storeNumber string, items []ListenItem) string {
	var b strings.Builder
	b.WriteString(shopOrigin(area))
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
	return fmt.Sprintf("%s/shop/buy-iphone/%s/%s", shopOrigin(area), familyPath(product.Type), strings.ToLower(product.Code))
}

// BagURL 地区购物袋地址
func BagURL(area model.Area) string {
	return fmt.Sprintf("%s/shop/bag", area.ShopOrigin())
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

// fetchStock 查询一家门店的一批型号
func fetchStock(area model.Area, storeNumber string, items []ListenItem) skuFetch {
	result := newSkuFetch()
	skUrl := fulfillmentURL(area, storeNumber, items)
	referer := productBuyURL(area, items[0].Product)

	warmupShopPage(referer, area.AcceptLanguage())

	resp, err := shopHTTP().R().
		SetHeader("accept", "*/*").
		SetHeader("accept-language", area.AcceptLanguage()).
		SetHeader("cache-control", "no-cache").
		SetHeader("pragma", "no-cache").
		SetHeader("referer", referer).
		SetHeader("sec-fetch-dest", "empty").
		SetHeader("sec-fetch-mode", "cors").
		SetHeader("sec-fetch-site", "same-origin").
		Get(skUrl)
	if err != nil {
		log.Println(err)
		result.failures[storeNumber] = "网络错误"
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			result.failures[storeNumber] = "请求超时"
		}
		return result
	}

	body := resp.String()
	log.Println(resp.GetStatusCode(), skUrl)

	if resp.GetStatusCode() == 541 || resp.GetStatusCode() >= 400 {
		result.blocked = true
		if storeCoverRe.MatchString(body) {
			result.closed[storeNumber] = true
		}
		result.failures[storeNumber] = fmt.Sprintf("HTTP %d", resp.GetStatusCode())
		resetShopSession()
		preview := body
		if len(preview) > 160 {
			preview = preview[:160]
		}
		log.Println("fulfillment blocked", preview)
		return result
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
		result.failures[storeNumber] = "响应中没有库存数据"
		if msg := gjson.Get(body, "body.errorMessage").String(); msg != "" {
			result.failures[storeNumber] = msg
			if strings.Contains(msg, "no store associated") {
				result.failures[storeNumber] = "门店编号已失效，请换一家门店"
			}
		}
		return result
	}

	for _, store := range stores.Array() {
		for productCode, availability := range store.Get("partsAvailability").Map() {
			uniqKey := fmt.Sprintf("%s.%s", store.Get("storeNumber").String(), productCode)
			result.skus[uniqKey] = fulfillmentAvailable(availability)
		}
	}

	return result
}
