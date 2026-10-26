package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"apple-store-helper/checkout"
	"apple-store-helper/power"
)

const AutoOrderOff = "off"

// Only a non-secret path is persisted. Arming/mode never survives app restart.
type AutoOrderOptions struct {
	ConfigPath string `json:"config_path,omitempty"`
	Mode       string `json:"-"`
}

func (o AutoOrderOptions) Normalized() AutoOrderOptions {
	if o.Mode == "" {
		o.Mode = AutoOrderOff
	}
	return o
}

func (o AutoOrderOptions) Validate() error {
	o = o.Normalized()
	if o.Mode == AutoOrderOff {
		return nil
	}
	if o.Mode != checkout.ModeReview && o.Mode != checkout.ModeSubmit {
		return errors.New("API 下单模式必须是 off、review 或 submit")
	}
	if o.ConfigPath == "" {
		return errors.New("请先选择 API 下单的私有配置文件")
	}
	_, err := checkout.LoadConfig(o.ConfigPath)
	if err != nil {
		return errors.New("API 下单配置无效；请检查配置及私有会话/联系人文件")
	}
	return nil
}

type AutoOrderEvent struct {
	Mode, State, Stage, SKU, StoreID string
	TotalCents                       int64
	Message                          string
}

type autoOrderCoordinator struct {
	mu           sync.Mutex
	options      AutoOrderOptions
	config       checkout.Config
	contact      checkout.Contact
	executor     checkout.Executor
	consumed     bool
	running      bool
	done         chan struct{}
	cancel       context.CancelFunc
	result       AutoOrderEvent
	notify       func(AutoOrderEvent)
	acquirePower func() (func(), error)
}

func newAutoOrderCoordinator(options AutoOrderOptions, config checkout.Config, contact checkout.Contact,
	executor checkout.Executor, notify func(AutoOrderEvent)) *autoOrderCoordinator {
	config.StoreIDs = append([]string(nil), config.StoreIDs...)
	return &autoOrderCoordinator{options: options, config: config, contact: contact,
		executor: executor, notify: notify, acquirePower: power.Acquire}
}

func (c *autoOrderCoordinator) matches(item ListenItem) bool {
	if item.Disabled || item.Area.ShortCode != "cn" || item.Product.Type != "iphoneduo" || item.Product.Code != c.config.SKU {
		return false
	}
	for _, id := range c.config.StoreIDs {
		if id == item.Store.StoreNumber {
			return true
		}
	}
	return false
}

func (c *autoOrderCoordinator) handles(hits []ListenItem) bool {
	if c == nil {
		return false
	}
	for _, hit := range hits {
		if c.matches(hit) {
			return true
		}
	}
	return false
}

func (c *autoOrderCoordinator) tryStart(hits []ListenItem) bool {
	if c == nil {
		return false
	}
	// The configured store order is the user's preference, not map/scheduler order.
	var chosen *ListenItem
	for _, storeID := range c.config.StoreIDs {
		for _, hit := range hits {
			if hit.Status == StatusInStock && c.matches(hit) && hit.Store.StoreNumber == storeID {
				copy := hit
				chosen = &copy
				break
			}
		}
		if chosen != nil {
			break
		}
	}
	if chosen == nil {
		return false
	}
	c.mu.Lock()
	if c.consumed {
		c.mu.Unlock()
		return false
	}
	c.consumed, c.running = true, true
	c.done = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	c.cancel = cancel
	c.mu.Unlock()
	target := checkout.Target{SKU: chosen.Product.Code, StoreID: chosen.Store.StoreNumber,
		Province: c.config.Province, City: c.config.City, District: c.config.District,
		MaxTotalCents: c.config.MaxTotalCents, Payment: c.config.Payment, Contact: c.contact}
	go func() {
		defer cancel()
		event := AutoOrderEvent{Mode: c.options.Mode, State: "failed", SKU: target.SKU, StoreID: target.StoreID,
			Message: "API 下单尝试已停止，未自动重试；请核对会话和订单记录"}
		// Even a panic consumes this arm. Never automatically repeat mutations.
		defer func() {
			if recover() != nil {
				event.State = "failed"
				event.Message = "API 下单尝试异常停止；请先核对订单记录，不要盲目重试"
			}
			c.mu.Lock()
			c.result, c.running = event, false
			c.mu.Unlock()
			if c.notify != nil {
				func() {
					defer func() {
						if recover() != nil {
							log.Println("API 下单状态回调异常；尝试结果仍保留，请核对私有订单记录")
						}
					}()
					c.notify(event)
				}()
			}
			close(c.done)
		}()
		if release, err := c.acquirePower(); err == nil && release != nil {
			defer release()
		}
		result, err := c.executor.Run(ctx, target)
		event.Stage = result.Stage
		if err != nil {
			if result.State == "submission_unknown" {
				event.State = result.State
				event.Message = "提交结果未知：必须先在 Apple 订单页核对，程序不会再次提交"
			}
			return
		}
		event.State, event.TotalCents = result.State, result.TotalCents
		switch result.State {
		case "review", "review_ready":
			event.Message = "已到订单核对页；演练未提交订单，也未付款"
		case "created":
			event.Message = "订单已创建；请本人在 Apple 订单页核对并付款"
		default:
			event.State = "failed"
		}
	}()
	return true
}

func (c *autoOrderCoordinator) wait(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ConfigureAutoOrder explicitly creates a fresh one-shot arm; callers invoke it
// only from a manual Start or a command-line invocation, never during settings load.
func (s *listenService) ConfigureAutoOrder(options AutoOrderOptions) error {
	options = options.Normalized()
	if err := options.Validate(); err != nil {
		return err
	}
	var next *autoOrderCoordinator
	if options.Mode != AutoOrderOff {
		config, err := checkout.LoadConfig(options.ConfigPath)
		if err != nil {
			return errors.New("无法读取 API 下单配置")
		}
		probe := &autoOrderCoordinator{config: config}
		matched := false
		for _, item := range s.GetListenItems() {
			if probe.matches(item) {
				matched = true
				break
			}
		}
		if !matched {
			return errors.New("监听列表中没有 API 配置所指定的中国大陆 Duo 型号和门店")
		}
		contact, err := checkout.LoadContact(config.ContactFile)
		if err != nil {
			return errors.New("无法读取私有提货人资料")
		}
		if _, err := checkout.LoadSession(config.SessionFile); err != nil {
			return errors.New("私有 Apple 会话文件不可用，请先更新会话并完成只读自检")
		}
		runner, err := checkout.NewRunner(config, options.Mode)
		if err != nil {
			return errors.New("API 下单会话或配置不可用")
		}
		next = newAutoOrderCoordinator(options, config, contact, runner, s.fireAutoOrder)
	}
	return s.installAutoOrder(next)
}

func (s *listenService) installAutoOrder(next *autoOrderCoordinator) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.autoOrder; old != nil {
		old.mu.Lock()
		defer old.mu.Unlock()
		if old.running {
			return errors.New("API 下单尝试正在运行；不能修改或重复武装")
		}
		// tick may already hold old outside s.mu. Retire that snapshot atomically
		// before replacement so it cannot launch an orphaned submit after disarm.
		old.consumed = true
	}
	s.autoOrder = next
	return nil
}

func (s *listenService) OnAutoOrder(fn func(AutoOrderEvent)) {
	s.mu.Lock()
	s.onAutoOrder = fn
	s.mu.Unlock()
}

func (s *listenService) AutoOrderLastResult() (AutoOrderEvent, bool) {
	s.mu.Lock()
	c := s.autoOrder
	s.mu.Unlock()
	if c == nil {
		return AutoOrderEvent{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result, c.result.State != ""
}

func (s *listenService) fireAutoOrder(event AutoOrderEvent) {
	s.mu.Lock()
	fn := s.onAutoOrder
	s.mu.Unlock()
	go s.SendPush("API 下单状态", event.Message+"\n型号："+event.SKU+"\n门店："+event.StoreID,
		"https://www.apple.com.cn/shop/order/list")
	if fn != nil {
		fn(event)
	}
}

func (s *listenService) AutoOrderHandles(hits []ListenItem) bool {
	s.mu.Lock()
	c := s.autoOrder
	s.mu.Unlock()
	return c.handles(hits)
}

func (s *listenService) WaitAutoOrder(ctx context.Context) error {
	s.mu.Lock()
	c := s.autoOrder
	s.mu.Unlock()
	return c.wait(ctx)
}

func (s *listenService) CancelAutoOrder() {
	s.mu.Lock()
	c := s.autoOrder
	if c != nil {
		c.mu.Lock()
		c.consumed = true
		cancel := c.cancel
		if !c.running {
			s.autoOrder = nil
		}
		c.mu.Unlock()
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	s.mu.Unlock()
}
