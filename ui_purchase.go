package main

import (
	"apple-store-helper/services"
	"apple-store-helper/view"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var purchaseMode *widget.Select
var favoritesURL *widget.Entry

var purchaseLabels = []string{"购物车（补货）", "型号购买页（首发）", "收藏页（首发）"}
var purchaseModes = []string{services.OpenBag, services.OpenProduct, services.OpenFavorites}

func currentPurchaseOptions() services.PurchaseOptions {
	mode := services.OpenBag
	if i := purchaseMode.SelectedIndex(); i >= 0 && i < len(purchaseModes) {
		mode = purchaseModes[i]
	}
	return services.PurchaseOptions{Mode: mode, FavoritesURL: favoritesURL.Text}
}

func createPurchaseControls() fyne.CanvasObject {
	favoritesURL = widget.NewEntry()
	favoritesURL.SetPlaceHolder("选填：自定义 Apple 收藏页地址；留空使用对应地区的「你的收藏」")
	favoritesURL.Validator = func(value string) error {
		return (services.PurchaseOptions{Mode: services.OpenFavorites, FavoritesURL: value}).Validate()
	}
	favoritesURL.Hide()
	purchaseMode = widget.NewSelect(purchaseLabels, func(_ string) {
		if currentPurchaseOptions().Mode == services.OpenFavorites {
			favoritesURL.Show()
		} else {
			favoritesURL.Hide()
		}
		applyPurchaseOptions()
		saveSettings()
	})
	favoritesURL.OnChanged = func(_ string) {
		applyPurchaseOptions()
		saveSettings()
	}
	purchaseMode.SetSelectedIndex(0)
	return container.NewVBox(purchaseMode, favoritesURL, createAutoOrderControls())
}

func loadPurchaseOptions(options services.PurchaseOptions) {
	options = options.Normalized()
	favoritesURL.SetText(options.FavoritesURL)
	for i, mode := range purchaseModes {
		if mode == options.Mode {
			purchaseMode.SetSelectedIndex(i)
			break
		}
	}
}

// Test only one chosen product; never launch all selected SKU tabs during self-check.
func testPurchaseURL() (string, error) {
	options := currentPurchaseOptions()
	if err := options.Validate(); err != nil {
		return "", err
	}
	item := services.ListenItem{Area: services.Area.GetArea(areaWidget.Selected)}
	if options.Mode == services.OpenProduct {
		selected := productWidget.Selected()
		if len(selected) == 0 {
			return "", fmt.Errorf("请先选择型号；测试仅打开第一个选中型号")
		}
		item.Product = services.Product.GetProduct(areaWidget.Selected, selected[0])
	}
	return options.URL(item), nil
}

func testOpenPurchase() {
	link, err := testPurchaseURL()
	if err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	openBrowser(link)
}

func startListening() {
	if err := currentPurchaseOptions().Validate(); err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	options := currentAutoOrderOptions()
	if err := options.Validate(); err != nil {
		dialog.ShowError(err, view.Window)
		return
	}
	if options.Mode == "submit" {
		confirmAutoOrderStart(options)
		return
	}
	beginListening(options)
}

// Do not change an active monitor's destination while a custom URL is incomplete.
// The Entry shows its validation error; start/test also reject the invalid draft.
func applyPurchaseOptions() {
	options := currentPurchaseOptions()
	if options.Validate() == nil {
		services.Listen.SetPurchaseOptions(options)
	}
}
