package services

import (
	"fmt"
	"sort"
	"strings"

	"apple-store-helper/model"
)

// familyNames 机型代号对应的展示名
var familyNames = map[string]string{
	"iphoneduo":      "iPhone Duo",
	"iphoneair":      "iPhone Air",
	"iphone17":       "iPhone 17",
	"iphone17e":      "iPhone 17e",
	"iphone18pro":    "iPhone 18 Pro",
	"iphone18promax": "iPhone 18 Pro Max",
}

// FamilyName 机型展示名，如 iphoneduo -> iPhone Duo；未收录的按 iphoneXX -> iPhone XX 处理
func FamilyName(familyType string) string {
	if name, ok := familyNames[familyType]; ok {
		return name
	}
	if rest := strings.TrimPrefix(familyType, "iphone"); rest != familyType {
		return "iPhone " + rest
	}
	return familyType
}

// productParts 拆出型号的机型、颜色、容量。Title 形如「iphoneduo - 夜空色 - 256gb」
func productParts(p model.Product) (family, color, capacity string) {
	parts := strings.Split(p.Title, " - ")
	family = FamilyName(p.Type)
	if p.Type == "" && len(parts) > 0 {
		family = FamilyName(parts[0])
	}
	if len(parts) > 1 {
		color = parts[1]
	}
	if len(parts) > 2 {
		capacity = strings.ToUpper(parts[2])
	}
	return family, color, capacity
}

// TitleName 由型号标题得到展示名，用于只有标题的场合（如型号选择框）
func TitleName(title string) string {
	parts := strings.SplitN(title, " - ", 2)
	return ProductName(model.Product{Type: parts[0], Title: title})
}

// ProductName 型号展示名，如「iPhone Duo 夜空色 256GB」。Title 仍作为保存与匹配的键，不改动
func ProductName(p model.Product) string {
	family, color, capacity := productParts(p)
	return strings.Join(strings.Fields(strings.Join([]string{family, color, capacity}, " ")), " ")
}

// capacityRank 容量排序：256GB < 512GB < 1TB < 2TB
func capacityRank(capacity string) int {
	var n int
	fmt.Sscanf(capacity, "%d", &n)
	if strings.HasSuffix(capacity, "TB") {
		n *= 1024
	}
	return n
}

// GroupHits 按地区、门店分组，同机型同颜色的容量合并成一行，如
//
//	广东-天环广场
//	  iPhone Duo 夜空色 256GB / 512GB / 2TB
func GroupHits(hits []ListenItem) []string {
	type storeKey struct{ area, store string }
	var storeOrder []storeKey
	lines := map[storeKey]map[string][]string{}
	lineOrder := map[storeKey][]string{}

	multiArea := false
	for _, hit := range hits {
		if hit.Area.Locale != hits[0].Area.Locale {
			multiArea = true
		}
	}

	for _, hit := range hits {
		sk := storeKey{hit.Area.Title, hit.Store.CityStoreName}
		if _, ok := lines[sk]; !ok {
			storeOrder = append(storeOrder, sk)
			lines[sk] = map[string][]string{}
		}
		family, color, capacity := productParts(hit.Product)
		name := strings.TrimSpace(family + " " + color)
		if _, ok := lines[sk][name]; !ok {
			lineOrder[sk] = append(lineOrder[sk], name)
		}
		lines[sk][name] = append(lines[sk][name], capacity)
	}

	var out []string
	for _, sk := range storeOrder {
		header := sk.store
		if multiArea {
			header = sk.area + " " + sk.store
		}
		out = append(out, header)
		for _, name := range lineOrder[sk] {
			caps := lines[sk][name]
			sort.Slice(caps, func(i, j int) bool { return capacityRank(caps[i]) < capacityRank(caps[j]) })
			out = append(out, "  "+strings.TrimSpace(name+" "+strings.Join(caps, " / ")))
		}
	}
	return out
}
