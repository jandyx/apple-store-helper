package services

import (
	"testing"

	"apple-store-helper/model"
)

// 自动更新的机型与门店数据合并前，确认每个地区都能解析出可用的型号和门店
func TestDataParsesForAllAreas(t *testing.T) {
	for _, area := range model.Areas {
		products := Area.ProductsByCode(area.Locale)
		if len(products) == 0 {
			t.Errorf("%s: 没有解析出任何型号", area.Title)
		}
		for _, p := range products {
			if p.Code == "" || p.Type == "" {
				t.Errorf("%s: 型号数据不完整 %+v", area.Title, p)
				break
			}
		}

		stores := Store.ByArea(area)
		if len(stores) == 0 {
			t.Errorf("%s: 没有解析出任何门店", area.Title)
		}
		for _, s := range stores {
			if s.StoreNumber == "" || s.CityStoreName == "" {
				t.Errorf("%s: 门店数据不完整 %+v", area.Title, s)
				break
			}
		}
	}
}
