// apple-store-helper-cli 命令行版：不需要桌面环境，适合放在服务器或 NAS 上长期运行，配合手机推送使用。
//
// 用法示例：
//
//	# 列出地区、门店、型号
//	apple-store-helper-cli -list-stores -area cn
//	apple-store-helper-cli -list-products -area cn -product duo
//
//	# 监听广州两家店的全部 iPhone Duo，3 秒一轮，有货推送到 Server酱
//	apple-store-helper-cli -area cn -store 天环,珠江新城 -product duo -interval 3s \
//	    -push https://sctapi.ftqq.com/<SendKey>.send
//
//	# 直接使用图形版保存的监听列表、频率和推送地址
//	apple-store-helper-cli -use-gui-config
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"apple-store-helper/checkout"
	"apple-store-helper/common"
	"apple-store-helper/model"
	"apple-store-helper/services"
)

// multiFlag 可重复的参数，也接受逗号分隔
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*m = append(*m, part)
		}
	}
	return nil
}

type options struct {
	area                string
	stores              multiFlag
	products            multiFlag
	push                multiFlag
	interval            string
	useGUIConfig        bool
	once                bool
	stopOnHit           bool
	listStores          bool
	listProducts        bool
	verbose             bool
	version             bool
	autoOrderConfig     string
	autoOrderMode       string
	checkOrderSession   string
	prepareOrderSession string
	prepareOrderSKU     string
	prepareOrderCap     int64
}

func main() {
	var opt options
	flag.StringVar(&opt.area, "area", "cn", "地区：cn / hk / tw / sg / jp / au / my，或中文名如「中国大陆」")
	flag.Var(&opt.stores, "store", "门店：门店编号（如 R639）或名称关键字（如 天环），可重复或逗号分隔")
	flag.Var(&opt.products, "product", "型号：型号编号（如 MK2N4CH/A）或关键字（如 duo、夜空色、512gb），多个关键字用空格分隔表示同时满足；可重复或逗号分隔")
	flag.Var(&opt.push, "push", "手机推送地址：Bark / Server酱 / 企业微信机器人 / Telegram / Webhook，可重复")
	flag.StringVar(&opt.interval, "interval", "3s", "查询间隔，如 3s、1.5s、800ms，最低 300ms")
	flag.BoolVar(&opt.useGUIConfig, "use-gui-config", false, "使用图形版保存的监听列表、查询频率和推送地址")
	flag.BoolVar(&opt.once, "once", false, "只查询一轮并退出：有货退出码 0，无货 1，查询失败 2")
	flag.BoolVar(&opt.stopOnHit, "stop-on-hit", false, "有货后退出（默认继续监听，只在变成有货时推送）")
	flag.BoolVar(&opt.listStores, "list-stores", false, "列出地区的门店并退出（可配合 -store 过滤）")
	flag.BoolVar(&opt.listProducts, "list-products", false, "列出地区的型号并退出（可配合 -product 过滤）")
	flag.BoolVar(&opt.verbose, "v", false, "同时把诊断日志输出到终端")
	flag.BoolVar(&opt.version, "version", false, "显示版本")
	flag.StringVar(&opt.autoOrderConfig, "auto-order-config", "", "私有 API 下单配置 JSON 路径（不会读取浏览器数据库）")
	flag.StringVar(&opt.autoOrderMode, "auto-order-mode", services.AutoOrderOff, "API 模式：off（默认）/ review（会进入结账但不提交）/ submit（创建真实订单，不付款）")
	flag.StringVar(&opt.checkOrderSession, "check-order-session", "", "只读检查私有会话 JSON 能否读取购物袋；不检查库存，不加购、不结账、不下单")
	flag.StringVar(&opt.prepareOrderSession, "prepare-order-session", "", "协议诊断：会话 JSON 路径；只请求进入现有单件购物袋的结账会话，不选店、不提交、不付款")
	flag.StringVar(&opt.prepareOrderSKU, "prepare-order-sku", "", "协议诊断的精确 SKU（必须与现有单件购物袋相同）")
	flag.Int64Var(&opt.prepareOrderCap, "prepare-order-cap-cents", 0, "协议诊断的金额上限，单位分；必须显式给出")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Apple Store Helper 命令行版 %s\n\n用法：apple-store-helper-cli [参数]\n\n", common.VERSION)
		flag.PrintDefaults()
	}
	flag.Parse()

	if opt.version {
		fmt.Println(common.VERSION)
		return
	}

	if path, err := services.SetupLogging(opt.verbose); err == nil {
		log.Println("命令行版启动", common.VERSION)
		defer fmt.Fprintln(os.Stderr, "诊断日志:", path)
	}

	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(2)
	}
}

func run(opt options) error {
	if opt.checkOrderSession != "" && opt.prepareOrderSession != "" {
		return errors.New("只读自检与进入结账诊断不能同时运行")
	}
	if opt.prepareOrderSession != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		result, err := checkout.PrepareCheckoutSession(ctx, opt.prepareOrderSession, opt.prepareOrderSKU, opt.prepareOrderCap)
		if err != nil {
			return fmt.Errorf("进入结账诊断已停止（未提交订单）：%w", err)
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if opt.checkOrderSession != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := checkout.ProbeSession(ctx, opt.checkOrderSession)
		if err != nil {
			// checkout errors are deliberately fixed/redacted; preserve that useful
			// cause rather than hiding HTTP/schema/session failures behind one line.
			return fmt.Errorf("Apple 会话只读自检失败（未尝试下单）：%w", err)
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	area, err := resolveArea(opt.area)
	if err != nil {
		return err
	}

	if opt.listStores {
		for _, store := range filterStores(services.Store.ByArea(area), opt.stores) {
			fmt.Printf("%-6s %s\n", store.StoreNumber, store.CityStoreName)
		}
		return nil
	}
	if opt.listProducts {
		for _, product := range filterProducts(services.Area.ProductsByCode(area.Locale), opt.products) {
			fmt.Printf("%-12s %s\n", product.Code, services.ProductName(product))
		}
		return nil
	}

	listen := services.Listen
	defer func() {
		listen.CancelAutoOrder()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = listen.WaitAutoOrder(ctx)
		listen.SetStatus(services.Pause)
	}()
	intervalText := opt.interval
	pushText := strings.Join(opt.push, "\n")

	if opt.useGUIConfig {
		settings, err := services.LoadSettings()
		if err != nil {
			return fmt.Errorf("读取图形版配置失败（先在图形版里添加监听项）: %w", err)
		}
		listen.SetListenItems(settings.ListenItems, services.Area.GetArea(settings.SelectedArea))
		if err := settings.Purchase.Validate(); err != nil {
			return err
		}
		listen.SetPurchaseOptions(settings.Purchase)
		if opt.autoOrderConfig == "" {
			opt.autoOrderConfig = settings.AutoOrder.ConfigPath
		}
		if !flagSet("interval") && settings.IntervalMs() > 0 {
			intervalText = fmt.Sprintf("%dms", settings.IntervalMs())
		}
		if !flagSet("push") {
			pushText = settings.Pushes()
		}
	}

	if len(opt.stores) > 0 || len(opt.products) > 0 {
		if len(opt.stores) == 0 || len(opt.products) == 0 {
			return errors.New("-store 和 -product 需要同时指定")
		}
		stores := filterStores(services.Store.ByArea(area), opt.stores)
		products := filterProducts(services.Area.ProductsByCode(area.Locale), opt.products)
		if len(stores) == 0 {
			return fmt.Errorf("%s 没有匹配 %q 的门店，用 -list-stores 查看", area.Title, opt.stores.String())
		}
		if len(products) == 0 {
			return fmt.Errorf("%s 没有匹配 %q 的型号，用 -list-products 查看", area.Title, opt.products.String())
		}
		for _, store := range stores {
			for _, product := range products {
				listen.AddItem(area, store, product)
			}
		}
	}

	total, active := listen.Counts()
	if active == 0 {
		return errors.New("没有监听项：用 -store 和 -product 指定，或加 -use-gui-config 使用图形版的监听列表")
	}

	ms, ok := services.ParseInterval(intervalText)
	if !ok {
		return fmt.Errorf("无法识别的查询间隔 %q", intervalText)
	}
	listen.SetIntervalMs(ms)
	listen.SetPushUrls(pushText)
	listen.SetStopOnHit(opt.stopOnHit)
	autoOptions := services.AutoOrderOptions{ConfigPath: opt.autoOrderConfig, Mode: opt.autoOrderMode}.Normalized()
	if err := validateAutoOrderInvocation(opt.once, autoOptions); err != nil {
		return err
	}
	if err := listen.ConfigureAutoOrder(autoOptions); err != nil {
		return err
	}
	orderEnabled := autoOptions.Mode != services.AutoOrderOff

	fmt.Printf("监听 %d 项（启用 %d），每 %s 查询一轮，手机推送 %d 个地址\n",
		total, active, time.Duration(ms)*time.Millisecond, len(services.SplitPushTargets(pushText)))
	for _, row := range listen.Rows() {
		fmt.Printf("  %s  %s  %s\n", row.Item.Area.Title, row.Item.Store.CityStoreName, services.ProductName(row.Item.Product))
	}

	if opt.once {
		listen.RunOnce()
		printRows(listen.Rows())
		os.Exit(onceExitCode(listen.Rows()))
	}

	hit := make(chan struct{}, 1)
	orderCompleted := make(chan struct{}, 1)
	listen.OnAutoOrder(func(event services.AutoOrderEvent) {
		fmt.Printf("\n%s API 下单：%s\n型号：%s 门店：%s 阶段：%s\n", now(), event.Message, event.SKU, event.StoreID, event.Stage)
		select {
		case orderCompleted <- struct{}{}:
		default:
		}
	})
	listen.OnInStock(func(items []services.ListenItem, purchase services.PurchaseOptions) {
		title, msg := services.HitMessage(items)
		fmt.Printf("\a\n%s %s\n%s\n购买入口：%s\n\n", now(), title, msg, strings.Join(services.PurchaseLinks(items, purchase), "\n"))
		if orderEnabled && !listen.AutoOrderHandles(items) {
			return
		}
		select {
		case hit <- struct{}{}:
		default:
		}
	})
	listen.OnWarning(func(title, msg string) {
		fmt.Printf("\a%s %s：%s\n", now(), title, msg)
	})
	printer := newChangePrinter()
	listen.OnChange(func() { printer.print(listen.Rows()) })

	listen.SetStatus(services.Running)
	listen.Run()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-stop:
			fmt.Println("\n已停止")
			return nil
		case <-hit:
			if opt.stopOnHit {
				if err := waitForOrderAndPush(listen, stop); err != nil {
					return err
				}
				return nil
			}
		case <-orderCompleted:
			if err := waitForOrderAndPush(listen, stop); err != nil {
				return err
			}
			return nil
		}
	}
}

func validateAutoOrderInvocation(once bool, options services.AutoOrderOptions) error {
	if once && options.Normalized().Mode != services.AutoOrderOff {
		return errors.New("-once 仅用于库存查询，不能与 API 下单模式同时使用")
	}
	return options.Validate()
}

type orderWaiter interface {
	WaitAutoOrder(context.Context) error
	CancelAutoOrder()
}

func waitForOrderAndPush(waiter orderWaiter, stop <-chan os.Signal) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute+5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- waiter.WaitAutoOrder(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			waiter.CancelAutoOrder()
			return errors.New("API 下单等待超时；请先核对 Apple 订单记录")
		}
	case <-stop:
		waiter.CancelAutoOrder()
		return nil
	}
	// Preserve the existing push-delivery grace period, but remain interruptible.
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-stop:
	}
	return nil
}

// flagSet 命令行是否显式传了某个参数
func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func resolveArea(value string) (model.Area, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	for _, area := range model.Areas {
		short := strings.SplitN(area.ShortCode, "-", 2)[0]
		if v == strings.ToLower(area.Title) || v == short || v == strings.ToLower(area.Locale) || v == area.ShortCode {
			return area, nil
		}
	}
	var names []string
	for _, area := range model.Areas {
		names = append(names, strings.SplitN(area.ShortCode, "-", 2)[0]+"（"+area.Title+"）")
	}
	return model.Area{}, fmt.Errorf("未知地区 %q，可选：%s", value, strings.Join(names, "、"))
}

// filterStores 按门店编号或名称关键字过滤，未指定时返回全部
func filterStores(stores []model.Store, keys []string) []model.Store {
	sort.Slice(stores, func(i, j int) bool { return stores[i].CityStoreName < stores[j].CityStoreName })
	if len(keys) == 0 {
		return stores
	}
	var out []model.Store
	for _, store := range stores {
		for _, key := range keys {
			if strings.EqualFold(store.StoreNumber, key) || containsFold(store.CityStoreName, key) {
				out = append(out, store)
				break
			}
		}
	}
	return out
}

// filterProducts 按型号编号或关键字过滤。一个参数里用空格分隔的多个关键字需同时满足
func filterProducts(products []model.Product, keys []string) []model.Product {
	if len(keys) == 0 {
		return products
	}
	var out []model.Product
	for _, product := range products {
		for _, key := range keys {
			if strings.EqualFold(product.Code, key) || matchAll(product.Title+" "+product.Type, strings.Fields(key)) {
				out = append(out, product)
				break
			}
		}
	}
	return out
}

func matchAll(text string, words []string) bool {
	for _, w := range words {
		if !containsFold(text, w) {
			return false
		}
	}
	return len(words) > 0
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func now() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func printRows(rows []services.Row) {
	for _, row := range rows {
		fmt.Printf("[%s] %s %s %s\n", row.Item.Status, row.Item.Area.Title, row.Item.Store.CityStoreName, services.ProductName(row.Item.Product))
	}
}

// onceExitCode 单次模式的退出码：有货 0，全部无货 1，有查询失败 2
func onceExitCode(rows []services.Row) int {
	code := 1
	for _, row := range rows {
		switch {
		case row.Item.Status == services.StatusInStock:
			return 0
		case row.Item.Status != services.StatusOutStock && !row.Item.Disabled:
			code = 2
		}
	}
	return code
}

// changePrinter 只打印状态有变化的项，另外每分钟打印一行心跳，避免长期运行时刷屏
type changePrinter struct {
	last      map[string]string
	rounds    int
	lastBeat  time.Time
	beatEvery time.Duration
}

func newChangePrinter() *changePrinter {
	return &changePrinter{last: map[string]string{}, lastBeat: time.Now(), beatEvery: time.Minute}
}

func (p *changePrinter) print(rows []services.Row) {
	p.rounds++
	counts := map[string]int{}
	for _, row := range rows {
		status := row.Item.Status
		counts[strings.SplitN(status, "：", 2)[0]]++
		if p.last[row.Key] != status {
			if status != services.StatusWait {
				fmt.Printf("%s [%s] %s %s %s\n", now(), status, row.Item.Area.Title, row.Item.Store.CityStoreName, services.ProductName(row.Item.Product))
			}
			p.last[row.Key] = status
		}
	}

	if time.Since(p.lastBeat) >= p.beatEvery {
		p.lastBeat = time.Now()
		var parts []string
		for status, n := range counts {
			parts = append(parts, fmt.Sprintf("%s %d", status, n))
		}
		sort.Strings(parts)
		fmt.Printf("%s 运行中：%s\n", now(), strings.Join(parts, "，"))
	}
}
