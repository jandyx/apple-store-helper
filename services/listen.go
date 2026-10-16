package services

// 监听服务：维护监听列表、按频率查询库存、发现有货后记录历史并推送。
// 不依赖任何图形或音频库，图形界面和命令行版都通过回调接入。

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang-module/carbon"

	"apple-store-helper/model"
)

const (
	StatusOutStock = "无货"
	StatusInStock  = "有货"
	StatusWait     = "等待"
	// StatusFailed 本轮没拿到该项结果（被拦截、超时或接口返回异常），不能当作无货
	StatusFailed = "查询失败"
	// StatusStoreClosed 开售前 Apple 会把整个商店切到预购封面页，所有接口返回 503，开放后自动恢复
	StatusStoreClosed = "商店维护中"
	// StatusDisabled 用户临时停用的项，不参与查询
	StatusDisabled = "已停用"

	Pause   = "暂停"
	Running = "监听中"

	DefaultIntervalMs = 3000
	// MinIntervalMs 自定义间隔下限，避免请求过密被风控
	MinIntervalMs  = 300
	blockedBackoff = 5 * time.Second
	// maxPartsPerRequest Apple pickup-message 单次请求最多返回的型号数
	maxPartsPerRequest = 20
	// maxConcurrentRequests 同时在飞的请求上限，门店多时不至于瞬间打出几十个连接
	maxConcurrentRequests = 4
	// jitterRatio 查询间隔叠加 ±25% 随机抖动，避免固定周期请求更容易被识别和限流（参考 dashug fork）
	jitterRatio = 0.25
	// 暂停或无监听项时用短间隔检查状态，保证点击“开始”后立即生效
	idleInterval = 200 * time.Millisecond
)

var Listen = newListenService()

func newListenService() *listenService {
	return &listenService{
		items:      map[string]ListenItem{},
		status:     Pause,
		intervalMs: DefaultIntervalMs,
		stopOnHit:  true,
	}
}

type listenService struct {
	// mu 保护以下全部字段：界面线程与监听协程会同时读写
	mu         sync.Mutex
	items      map[string]ListenItem
	status     string
	intervalMs int
	// pushUrls 手机推送地址，每行一个，渠道按域名自动识别
	pushUrls string
	// stopOnHit 有货后暂停监听。图形界面需要（用户去下单），命令行版长期运行时关闭
	stopOnHit bool

	onChange  func()
	onStatus  func(string)
	onInStock func([]ListenItem)
}

type ListenItem struct {
	// Area 所属地区。旧版配置没有该字段，加载时按当时选中的地区补上
	Area     model.Area
	Store    model.Store
	Product  model.Product
	Status   string
	Time     carbon.DateTime
	Disabled bool `json:",omitempty"`
}

// ItemKey 监听项的唯一键。门店编号全球唯一，型号编号按地区区分，两者组合即可
func ItemKey(store model.Store, product model.Product) string {
	return store.StoreNumber + "." + product.Code
}

// Row 排好序的一行监听项
type Row struct {
	Key  string
	Item ListenItem
}

// OnChange 监听列表或其状态变化时回调
func (s *listenService) OnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// OnStatusChange 开始 / 暂停状态变化时回调
func (s *listenService) OnStatusChange(fn func(string)) {
	s.mu.Lock()
	s.onStatus = fn
	s.mu.Unlock()
}

// OnInStock 检测到有货时回调，参数为本轮全部命中项（已排序）。推送与历史记录由服务自身完成
func (s *listenService) OnInStock(fn func([]ListenItem)) {
	s.mu.Lock()
	s.onInStock = fn
	s.mu.Unlock()
}

func (s *listenService) notifyChange() {
	s.mu.Lock()
	fn := s.onChange
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// SetStatus 设置开始 / 暂停
func (s *listenService) SetStatus(status string) {
	s.mu.Lock()
	changed := s.status != status
	s.status = status
	fn := s.onStatus
	s.mu.Unlock()
	if changed && fn != nil {
		fn(status)
	}
}

// GetStatus 当前开始 / 暂停状态
func (s *listenService) GetStatus() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// SetIntervalMs 设置查询间隔（毫秒）
func (s *listenService) SetIntervalMs(ms int) {
	s.mu.Lock()
	s.intervalMs = ms
	s.mu.Unlock()
}

// IntervalDuration 当前查询间隔，未设置或非法时回退默认值
func (s *listenService) IntervalDuration() time.Duration {
	s.mu.Lock()
	ms := s.intervalMs
	s.mu.Unlock()
	if ms <= 0 {
		ms = DefaultIntervalMs
	}
	return time.Duration(ms) * time.Millisecond
}

// SetPushUrls 设置手机推送地址
func (s *listenService) SetPushUrls(urls string) {
	s.mu.Lock()
	s.pushUrls = urls
	s.mu.Unlock()
}

// PushUrls 手机推送地址
func (s *listenService) PushUrls() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pushUrls
}

// SetStopOnHit 有货后是否暂停监听
func (s *listenService) SetStopOnHit(stop bool) {
	s.mu.Lock()
	s.stopOnHit = stop
	s.mu.Unlock()
}

// Add 按地区、门店名、型号名添加监听项，已存在时忽略。返回是否新增
func (s *listenService) Add(areaTitle string, storeTitle string, productTitle string) bool {
	area := Area.GetArea(areaTitle)
	store := Store.GetStore(areaTitle, storeTitle)
	product := Product.GetProduct(areaTitle, productTitle)
	return s.AddItem(area, store, product)
}

// AddItem 添加监听项，已存在时忽略。返回是否新增
func (s *listenService) AddItem(area model.Area, store model.Store, product model.Product) bool {
	key := ItemKey(store, product)

	s.mu.Lock()
	_, exists := s.items[key]
	if !exists {
		s.items[key] = ListenItem{
			Area:    area,
			Store:   store,
			Product: product,
			Status:  StatusWait,
		}
	}
	s.mu.Unlock()

	if !exists {
		s.notifyChange()
	}
	return !exists
}

// Remove 删除一项
func (s *listenService) Remove(key string) {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
	s.notifyChange()
}

// SetDisabled 临时停用 / 恢复一项。停用的项保留在列表里，但不参与查询
func (s *listenService) SetDisabled(key string, disabled bool) {
	s.mu.Lock()
	item, ok := s.items[key]
	if ok {
		item.Disabled = disabled
		if disabled {
			item.Status = StatusDisabled
		} else {
			item.Status = StatusWait
		}
		s.items[key] = item
	}
	s.mu.Unlock()
	if ok {
		s.notifyChange()
	}
}

// Clean 清空全部监听项
func (s *listenService) Clean() {
	s.mu.Lock()
	s.items = map[string]ListenItem{}
	s.mu.Unlock()
	s.notifyChange()
}

// SetListenItems 用保存的配置恢复监听列表。fallbackArea 用于补全旧版配置里缺失的地区
func (s *listenService) SetListenItems(items map[string]ListenItem, fallbackArea model.Area) {
	s.mu.Lock()
	s.items = map[string]ListenItem{}
	for key, item := range items {
		if item.Area.Locale == "" {
			item.Area = fallbackArea
		}
		s.items[key] = item
	}
	s.mu.Unlock()
	s.notifyChange()
}

// GetListenItems 返回监听项的副本，调用方可放心遍历或序列化
func (s *listenService) GetListenItems() map[string]ListenItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make(map[string]ListenItem, len(s.items))
	for key, item := range s.items {
		items[key] = item
	}
	return items
}

// Counts 监听项总数与其中启用的数量
func (s *listenService) Counts() (total int, active int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		total++
		if !item.Disabled {
			active++
		}
	}
	return total, active
}

// areaOrder 地区在 model.Areas 里的顺序，用于排序
func areaOrder(area model.Area) int {
	for i, a := range model.Areas {
		if a.Locale == area.Locale {
			return i
		}
	}
	return len(model.Areas)
}

func lessItem(a, b ListenItem) bool {
	if oa, ob := areaOrder(a.Area), areaOrder(b.Area); oa != ob {
		return oa < ob
	}
	if a.Store.CityStoreName != b.Store.CityStoreName {
		return a.Store.CityStoreName < b.Store.CityStoreName
	}
	return a.Product.Title < b.Product.Title
}

// Rows 按地区、门店、型号排序的监听项，避免列表每轮刷新顺序乱跳
func (s *listenService) Rows() []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rowsLocked()
}

func (s *listenService) rowsLocked() []Row {
	rows := make([]Row, 0, len(s.items))
	for key, item := range s.items {
		rows = append(rows, Row{Key: key, Item: item})
	}
	sort.Slice(rows, func(i, j int) bool { return lessItem(rows[i].Item, rows[j].Item) })
	return rows
}

// RunOnce 立即查询一轮，供命令行版的单次模式使用
func (s *listenService) RunOnce() {
	s.tick()
}

// tick 查询一轮库存并更新状态，返回本轮结果。有货时记录历史、推送并回调 OnInStock
func (s *listenService) tick() skuFetch {
	var active []ListenItem
	for _, row := range s.Rows() {
		if !row.Item.Disabled {
			active = append(active, row.Item)
		}
	}
	if len(active) == 0 {
		return newSkuFetch()
	}

	result := queryItems(active)
	now := carbon.DateTime{Carbon: carbon.Now(carbon.Shanghai)}

	var hits []ListenItem
	s.mu.Lock()
	stopOnHit := s.stopOnHit
	for _, row := range s.rowsLocked() {
		item := row.Item
		if item.Disabled {
			continue
		}
		previous := item.Status
		available, ok := result.skus[ItemKey(item.Store, item.Product)]
		switch {
		case !ok && result.closed[item.Store.StoreNumber]:
			item.Status = StatusStoreClosed
		case !ok:
			item.Status = StatusFailed
			if reason := result.failures[item.Store.StoreNumber]; reason != "" {
				item.Status += "：" + reason
			}
		case available:
			item.Status = StatusInStock
			// 持续监听时只在「变成有货」那一刻提醒，否则每轮都会重复推送
			if stopOnHit || previous != StatusInStock {
				hits = append(hits, item)
			}
		default:
			item.Status = StatusOutStock
		}
		item.Time = now
		s.items[row.Key] = item
	}
	onInStock := s.onInStock
	s.mu.Unlock()

	s.notifyChange()

	if len(hits) > 0 {
		if stopOnHit {
			s.SetStatus(Pause)
		}
		if _, err := RecordHistory(hits, time.Now()); err != nil {
			log.Println("记录有货历史失败:", err)
		}
		title, msg := HitMessage(hits)
		go s.SendPush(title, msg+bagLinks(hits), BagURL(hits[0].Area))
		if onInStock != nil {
			onInStock(hits)
		}
	}

	return result
}

// HitMessage 有货提醒的标题和正文
func HitMessage(hits []ListenItem) (string, string) {
	multiArea := false
	for _, hit := range hits {
		if hit.Area.Locale != hits[0].Area.Locale {
			multiArea = true
		}
	}

	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		line := fmt.Sprintf("%s %s", hit.Store.CityStoreName, hit.Product.Title)
		if multiArea {
			line = hit.Area.Title + " " + line
		}
		lines = append(lines, line)
	}

	title := "有货提醒"
	if len(hits) > 1 {
		title = fmt.Sprintf("有货提醒（%d 项）", len(hits))
	}
	return title, strings.Join(lines, "\n") + "\n有货"
}

// bagLinks 命中跨地区时，在推送正文里附上其余地区的购物车地址；第一个地区的地址由各渠道的链接字段携带
func bagLinks(hits []ListenItem) string {
	seen := map[string]bool{BagURL(hits[0].Area): true}
	var lines []string
	for _, hit := range hits[1:] {
		bag := BagURL(hit.Area)
		if !seen[bag] {
			seen[bag] = true
			lines = append(lines, fmt.Sprintf("%s购物车：%s", hit.Area.Title, bag))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

// nextDelay 根据本轮结果计算下一轮的等待时间
func (s *listenService) nextDelay(result skuFetch) time.Duration {
	interval := s.IntervalDuration()
	// 商店封面期间不退避，开放后第一时间查到；其他拦截多半是限流，放慢一点
	if result.blocked && !result.storeClosed() && interval < blockedBackoff {
		interval = blockedBackoff
	}
	return withJitter(interval)
}

// Run 启动监听协程。状态为「监听中」且有启用的监听项时按频率查询
func (s *listenService) Run() {
	go func() {
		for {
			if _, active := s.Counts(); s.GetStatus() == Running && active > 0 {
				result := s.tick()
				time.Sleep(s.nextDelay(result))
				continue
			}
			time.Sleep(idleInterval)
		}
	}()
}

// SendPush 推送到所有已配置的手机推送地址。失败只记日志，不影响其他提醒方式
func (s *listenService) SendPush(title string, content string, link string) []PushResult {
	results := Push(s.PushUrls(), PushMessage{Title: title, Content: content, URL: link})
	for _, r := range results {
		if r.Err != nil {
			log.Printf("推送失败 [%s] %s: %v", r.Channel, MaskTarget(r.Target), r.Err)
		}
	}
	return results
}
