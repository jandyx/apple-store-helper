package main

import (
	"testing"

	"apple-store-helper/model"
	"apple-store-helper/services"
)

func TestResolveArea(t *testing.T) {
	for input, want := range map[string]string{"cn": "zh_CN", "中国香港": "zh_HK", "hk": "zh_HK", "JP": "ja_JP", "en_au": "en_AU"} {
		area, err := resolveArea(input)
		if err != nil || area.Locale != want {
			t.Errorf("resolveArea(%q) = %v, %v; want %s", input, area.Locale, err, want)
		}
	}
	if _, err := resolveArea("xx"); err == nil {
		t.Error("unknown area should fail")
	}
}

func TestFilters(t *testing.T) {
	stores := []model.Store{{StoreNumber: "R639", CityStoreName: "广东-珠江新城"}, {StoreNumber: "R577", CityStoreName: "广东-天环广场"}, {StoreNumber: "R320", CityStoreName: "北京-三里屯"}}
	if got := filterStores(stores, []string{"r639", "天环"}); len(got) != 2 {
		t.Fatalf("store filter got %+v", got)
	}

	products := []model.Product{
		{Code: "MK2N4CH/A", Title: "iphoneduo - 夜空色 - 256gb", Type: "iphoneduo"},
		{Code: "MK2Q4CH/A", Title: "iphoneduo - 夜空色 - 512gb", Type: "iphoneduo"},
		{Code: "MG6Y4CH/A", Title: "iphone17 - 黑色 - 256gb", Type: "iphone17"},
	}
	if got := filterProducts(products, []string{"duo 256gb"}); len(got) != 1 || got[0].Code != "MK2N4CH/A" {
		t.Fatalf("all-words match got %+v", got)
	}
	if got := filterProducts(products, []string{"duo", "mg6y4ch/a"}); len(got) != 3 {
		t.Fatalf("any-of match got %+v", got)
	}
}

func TestOnceExitCode(t *testing.T) {
	row := func(status string) services.Row { return services.Row{Item: services.ListenItem{Status: status}} }
	if c := onceExitCode([]services.Row{row(services.StatusOutStock), row(services.StatusInStock)}); c != 0 {
		t.Errorf("in stock should be 0, got %d", c)
	}
	if c := onceExitCode([]services.Row{row(services.StatusOutStock)}); c != 1 {
		t.Errorf("out of stock should be 1, got %d", c)
	}
	if c := onceExitCode([]services.Row{row(services.StatusOutStock), row(services.StatusStoreClosed)}); c != 2 {
		t.Errorf("failure should be 2, got %d", c)
	}
}
