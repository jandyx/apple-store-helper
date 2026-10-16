package services

import (
	"strings"
	"testing"

	"apple-store-helper/model"
)

func duo(color, capacity string) model.Product {
	return model.Product{Type: "iphoneduo", Title: "iphoneduo - " + color + " - " + capacity}
}

func TestProductName(t *testing.T) {
	if got := ProductName(duo("夜空色", "256gb")); got != "iPhone Duo 夜空色 256GB" {
		t.Errorf("got %q", got)
	}
	if got := ProductName(model.Product{Type: "iphone18promax", Title: "iphone18promax - 星辰银 - 1tb"}); got != "iPhone 18 Pro Max 星辰银 1TB" {
		t.Errorf("got %q", got)
	}
	if got := FamilyName("iphone19"); got != "iPhone 19" {
		t.Errorf("unknown family got %q", got)
	}
}

func TestGroupHits(t *testing.T) {
	cn, hk := model.Areas[0], model.Areas[1]
	tianhuan := model.Store{StoreNumber: "R577", CityStoreName: "广东-天环广场"}
	zhujiang := model.Store{StoreNumber: "R639", CityStoreName: "广东-珠江新城"}
	hits := []ListenItem{
		{Area: cn, Store: tianhuan, Product: duo("夜空色", "2tb")},
		{Area: cn, Store: tianhuan, Product: duo("夜空色", "256gb")},
		{Area: cn, Store: tianhuan, Product: duo("星光白色", "512gb")},
		{Area: cn, Store: zhujiang, Product: duo("夜空色", "1tb")},
	}
	got := strings.Join(GroupHits(hits), "\n")
	want := strings.Join([]string{
		"广东-天环广场",
		"  iPhone Duo 夜空色 256GB / 2TB",
		"  iPhone Duo 星光白色 512GB",
		"广东-珠江新城",
		"  iPhone Duo 夜空色 1TB",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	multi := GroupHits([]ListenItem{hits[0], {Area: hk, Store: model.Store{CityStoreName: "香港-ifc mall"}, Product: duo("夜空色", "256gb")}})
	if multi[0] != "中国大陆 广东-天环广场" || !strings.HasPrefix(multi[2], "中国香港 ") {
		t.Errorf("multi-area headers: %q", multi)
	}
}

func TestTitleName(t *testing.T) {
	if got := TitleName("iphone18pro - 宇宙橙色 - 512gb"); got != "iPhone 18 Pro 宇宙橙色 512GB" {
		t.Errorf("got %q", got)
	}
}
