package main

import (
	"testing"

	"apple-store-helper/model"
	"apple-store-helper/services"
	"fyne.io/fyne/v2/widget"
)

func TestPurchaseControlsAndTestTarget(t *testing.T) {
	w := setupTestWindow(t)
	settingsLoaded = false
	w.SetContent(createPurchaseControls())
	areaWidget = newRadioRow(services.Area.ForOptions(), nil)
	areaWidget.SetSelected(model.Areas[0].Title)
	products := services.Product.ByAreaTitleForOptions(model.Areas[0].Title)
	productWidget = newMultiPicker("型号", "请选择", "个", products)
	if currentPurchaseOptions().Mode != services.OpenBag || favoritesURL.Visible() {
		t.Fatal("default must preserve bag, with favorites hidden")
	}
	link, err := testPurchaseURL()
	if err != nil || link != services.BagURL(model.Areas[0]) {
		t.Fatalf("%s %v", link, err)
	}
	purchaseMode.SetSelectedIndex(1)
	if _, err := testPurchaseURL(); err == nil {
		t.Fatal("missing product must be explicit")
	}
	productWidget.SetSelected(products[:2])
	link, err = testPurchaseURL()
	if err != nil || link != services.ProductURL(model.Areas[0], services.Product.GetProduct(model.Areas[0].Title, products[0])) {
		t.Fatalf("%s %v", link, err)
	}
	purchaseMode.SetSelectedIndex(2)
	if !favoritesURL.Visible() {
		t.Fatal("favorites entry should be visible")
	}
	if link, err := testPurchaseURL(); err != nil || link != model.Areas[0].ShopOrigin()+"/shop/yoursaves" {
		t.Fatal("empty favorites must use regional saved-items page", link, err)
	}
	loadPurchaseOptions(services.PurchaseOptions{Mode: services.OpenFavorites, FavoritesURL: "https://www.apple.com.cn/shop/yoursaves"})
	link, err = testPurchaseURL()
	if err != nil || link != favoritesURL.Text {
		t.Fatalf("%s %v", link, err)
	}
}

func TestSelfCheckButtonsUseEqualColumns(t *testing.T) {
	w := setupTestWindow(t)
	buttons := createCheckButtons()
	w.SetContent(buttons)
	if len(buttons.Objects) != 4 {
		t.Fatal("expected four self-checks")
	}
	width := buttons.Objects[0].Size().Width
	for _, obj := range buttons.Objects {
		button := obj.(*widget.Button)
		if button.Size().Width != width || button.Size().Width < button.MinSize().Width {
			t.Fatalf("self-check button clipped: %s", button.Text)
		}
	}
}

func TestInvalidPurchaseDraftKeepsLastValidDestination(t *testing.T) {
	setupTestWindow(t)
	settingsLoaded = false
	createPurchaseControls()
	valid := services.PurchaseOptions{Mode: services.OpenFavorites, FavoritesURL: "https://www.apple.com.cn/shop/yoursaves"}
	loadPurchaseOptions(valid)
	favoritesURL.SetText("https://")
	if got := services.Listen.PurchaseOptions(); got != valid {
		t.Fatalf("invalid draft changed live destination: %+v", got)
	}
	if currentPurchaseOptions().Validate() == nil {
		t.Fatal("draft must fail self-check/start validation")
	}
}
