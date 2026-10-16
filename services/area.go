package services

import (
	"apple-store-helper/config"
	"apple-store-helper/model"
	"fmt"

	"github.com/thoas/go-funk"
	"github.com/tidwall/gjson"
)

var Area = areaService{}

type areaService struct{}

func (s *areaService) ProductsByCode(local string) []model.Product {

	areaInterface := funk.Find(model.Areas, func(x model.Area) bool {
		return x.Locale == local
	})
	if areaInterface == nil {
		return []model.Product{}
	}
	area := areaInterface.(model.Area)

	var products []model.Product
	productsJson := gjson.ParseBytes(config.MustReadConfigFile(fmt.Sprintf("products_%s.json", area.Locale)))

	// 日本等地区同一型号号码会按运营商（SoftBank / docomo / SIM 免费版）重复出现，按型号号码去重
	seen := map[string]bool{}
	for _, json := range productsJson.Array() {
		for _, result := range json.Get("products").Array() {
			partNumber := result.Get("partNumber").String()
			if seen[partNumber] {
				continue
			}
			seen[partNumber] = true

			color := json.Get(fmt.Sprintf("displayValues.dimensionColor.%s.value", result.Get("dimensionColor")))
			products = append(products, model.Product{
				Title: fmt.Sprintf("%s - %s - %s", result.Get("familyType"), color, result.Get("dimensionCapacity")),
				Type:  result.Get("familyType").String(),
				Code:  result.Get("partNumber").String(),
			})
		}
	}

	return products
}

func (s *areaService) ForOptions() []string {
	return funk.Get(model.Areas, "Title").([]string)
}

func (s *areaService) Title2Code(title string) string {
	areaInterface := funk.Find(model.Areas, func(x model.Area) bool {
		return x.Title == title
	})
	if areaInterface == nil {
		return ""
	}
	area := areaInterface.(model.Area)

	return area.Locale
}

func (s *areaService) GetArea(title string) model.Area {
	areaInterface := funk.Find(model.Areas, func(x model.Area) bool {
		return x.Title == title
	})
	if areaInterface == nil {
		// 返回默认地区或空地区
		if len(model.Areas) > 0 {
			return model.Areas[0]
		}
		return model.Area{}
	}
	area := areaInterface.(model.Area)

	return area
}
