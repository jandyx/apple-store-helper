package services

import (
	"os"
	"strings"
	"testing"

	"apple-store-helper/model"
)

// TestLiveEndpointHealth 用真实接口检查每个地区能否查到库存，供每日健康检查 CI 使用。
// 参考 dashug/apple-store-helper：接口被拦截或改版时，界面不会自己报错，只能靠定时检查发现。
// 需要设置 LIVE_CHECK=1 才会运行。商店维护中（开售前的预购封面页）不算失败。
func TestLiveEndpointHealth(t *testing.T) {
	if os.Getenv("LIVE_CHECK") != "1" {
		t.Skip("设置 LIVE_CHECK=1 后才访问真实接口")
	}

	var failed []string
	for _, area := range model.Areas {
		stores := Store.ByArea(area)
		products := Area.ProductsByCode(area.Locale)
		if len(stores) == 0 || len(products) == 0 {
			failed = append(failed, area.Title+": 没有门店或型号数据")
			continue
		}

		item := ListenItem{Area: area, Store: stores[0], Product: products[0]}
		result := queryItems([]ListenItem{item})

		switch {
		case result.closed[item.Store.StoreNumber]:
			t.Logf("%s: 商店维护中（预购封面页），跳过", area.Title)
		case len(result.skus) > 0:
			t.Logf("%s: 正常（%s %s）", area.Title, item.Store.CityStoreName, item.Product.Code)
		default:
			reason := result.failures[item.Store.StoreNumber]
			if reason == "" {
				reason = "没有返回库存数据"
			}
			failed = append(failed, area.Title+": 查询 "+item.Store.CityStoreName+" 失败 —— "+reason)
		}
	}

	if len(failed) > 0 {
		t.Fatalf("接口健康检查未通过：\n  %s", strings.Join(failed, "\n  "))
	}
}
