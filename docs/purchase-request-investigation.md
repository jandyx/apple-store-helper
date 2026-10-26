# 自提请求链：观察与验证

## 目标与边界

目标是有货后用 HTTP 请求推进已验证的自提步骤，不是重放页面点击。
浏览器仅用于一次正常流程观察、本人登录和必要的人工接管。
**调查工具本身只做观察与只读回放，不接到有货回调。** 后续 Go 请求链已接入监听器的独立一次性协调器，但完整真实链路仍未验收；见 [API 下单说明](api-auto-order.md)。

阶段必须分别验收：

```text
库存命中 → 当前会话与动态商品参数 → 加购物袋 → 选择门店/时段
        → 提货信息 → 查看订单 → 人工提交/付款
```

`updateSummary` 返回 200 只证明商品摘要可读，不证明购物袋有商品，更不证明占到库存。
541、HTML、跳转登录和应用层错误都不能当成无货或成功。不要绕过站点限制。

## 2026-10-19 已观察到的情况

- 独立 Chrome 的 `/shop/bag` 返回 200，但未登录、购物袋为空。
- 正常会话已有 `dssid2`、`as_dc`、`as_pcts` 等 Cookie；存在 Cookie 不等于登录成功。
- iPhone Duo 夜空色 512GB 的 `updateSummary` 返回 JSON；请求包含动态参数 `igt`。
- 摘要中存在 `body.response.summarySection.summary.buyNowButton` 等字段，尚未验证其请求能加购。
- 正常商品页发出的 `/shop/fulfillment-messages` 仍返回 541 + `Page Not Found` HTML，且请求带 Cookie。
- 此前点击加购物袋也返回 541；没有加购成功基线。正常访问购物袋后，配送查询仍然失败。
- 结账登录入口实测位于 `secure11.www.apple.com.cn`：不能把结账域名写死为 `secure.www.apple.com.cn`。
- `bag/status` 实测顶层是 `items`（number）、`ttl`（number）、`api`（object），不是通用 `head/body` 包装；单独校验。
- 用工具的同一会话 HTTP 客户端回放 `bag/status`，实测 200 JSON、`items=0`、耗时 295 ms（单次测量，不是性能基准）；没有页面点击或加购。
- 本人完成账号登录后，购物袋的 Apple Store 登录入口仍显示再次输入密码的页面；不能由账号 Cookie 推断商店授权已完成。

以上是当时的观察，不是对所有地区、会话或时段的保证。

## 可复用工具

`scripts/purchase_probe.cjs` 使用本机 Chrome 和已安装的 Playwright，不给 Go 应用增加运行依赖。
本机排查环境已把 Playwright 安装在 `~/.local/share/apple-store-helper-browser/node_modules/playwright`。
其他机器需自行提供 Playwright 模块路径，不会隐式安装软件。

```sh
node --test scripts/purchase_probe.test.cjs
node scripts/purchase_probe.cjs --help
node scripts/purchase_probe.cjs
# 非默认安装位置：ASH_PLAYWRIGHT_MODULE=/absolute/path/to/playwright
```

打开的是应用专用资料目录，不读取或复制个人 Chrome 登录数据。
同一资料目录只能由一个浏览器进程使用；先正常关闭已有排查窗口，不删除锁文件。
在这个窗口里本人登录、操作正常流程，终端支持：

- `requests`：列出已观察到的请求索引及脱敏摘要。
- `status`：列出会话 Cookie 名称，不显示值；不把它当作登录证明。
- `replay <index>`：用当前会话的 HTTP 客户端回放一次新鲜、成功的只读 JSON 请求。
- `exit`：关闭本工具拥有的浏览器。

回放只允许 `updateSummary`、`bag/status`、`retail/pickup-message`；
**即使是 GET，只要涉及加购、结账或下单，也不会回放。**
捕获记录 5 分钟后不允许回放，同一记录最多尝试一次；不自动重试、不跟随重定向。
这些限制用于调查工具，不代表完整购买流程的实现。

只有 URL 的 host/path、参数名称、HTTP 状态、类型和时间写入默认的 `.omx/purchase-capture/`。
不写请求正文、完整 URL、Cookie 值、认证请求或支付/结账内容。原始请求仅保留在当前进程内存。
不要公开专用浏览器资料目录；其中可能保存本人登录状态。

## 后续验收

1. 本人登录后，用正常官网流程取得加购和自提选择成功的基线；遇到拒绝就停止该链路。
2. 分析该次响应中的动态链接、token 来源、会话及步骤之间依赖，禁止凭空猜 API。
3. 每次只替换一个已确认步骤为请求，核对购物袋型号/数量、门店和时段；不能只看 HTTP 200。
4. 没有真实成功证据前，不连接有货触发、不声称已锁库存，不实现自动提交/付款。

## 社区线索（不是本机根因结论）

- [hteen #126](https://github.com/hteen/apple-store-helper/issues/126)：541 与会话有效期的历史报告。
- [dashug PR #8](https://github.com/dashug/apple-store-helper/pull/8)：库存迁移到 `retail/pickup-message`，不涵盖下单。
- [ENCHIGO PR #6](https://github.com/ENCHIGO/apple-pickup-watcher/pull/6)：同环境 Cookie 对照及会话处理；不保证适用于当前加购。
- [fruit-radar bag.rs](https://github.com/am-tmac/fruit-radar/blob/main/src-tauri/src/bag.rs)：页面流程参考，不是纯 HTTP 购买协议。

## 2026-10-23：原 Chrome 自提基线与 HTTP 搜店验证

- 用户在原 Chrome 自行完成登录并进入自提页。通过被动 DevTools Network 确认：购物袋结账为 `POST /shop/bagx/checkout_now`，选择取货为 `POST /shop/checkoutx/fulfillment`，正文绑定 `checkout.fulfillment.fulfillmentOptions.selectFulfillmentLocation`。
- 使用当前本人会话 `GET https://secure6.www.apple.com.cn/shop/checkout`，成功读到活动 `fulfillment` 模块。没有使用页面点击自动化。
- 从该模型读取 `checkout.fulfillment.pickupTab.pickup.storeLocator.a.search.url`，提交模型生成的表单，并覆盖 `searchInput` 及地区绑定为 `广东 广州 天河区`。实际 HTTP 200、模型状态 200，共 8 个表单字段。
- 该次直接搜店请求成功，不需要先逐次请求 `Selectstate` / `Selectcity`。这只证明本次会话下可直接提交地区，不代表地区联动接口永远可省略。
- 返回 12 家门店，其中 `R639`（珠江新城）、`R577`（天环广场）均为 `storeDisabled=true`、`availableNowForAllLines=false`。没有选择门店、继续联系人步骤、提交订单或付款。
- 当前 Go 模型解释器对该真实搜索响应应用成功，读到相同的 12 家门店；基线搜店表单序列化无错误。后续联系人、时段、支付方式和最终审核仍缺真实可用库存下的验收，不能声称已打通完整下单或锁库存。
- 原始模型及响应仅保存在忽略的 `.omx/purchase-runtime/`，权限 0600；不得提交 Cookie、动态 token、完整会话或个人信息。

### Go 原生请求复验与阶段判断修复

真实 Go HTTP 请求暴露了原阶段判断的兼容缺陷：官网 `checkout.c` 实际为 `[companionBar, fulfillment, locationConsent, session]`，不是单一元素。原 `phase` 因此错误返回 `unknown`。

修复后仅忽略这三个已观察到的辅助模块，并要求恰好一个支持的业务阶段；未知模块、非字符串及多个业务阶段仍停止。新增阶段判断回归测试，并让现有模拟全链路使用官网多模块布局。

使用当前私有会话运行一次临时 Go 诊断：仅 GET 当前结账页，再调用模型返回的搜店 action。该诊断通过，解析到 12 家门店；R639/R577 仍不可用。临时联网测试已删除，常规测试不访问官网。`go test ./...`、`go test -race ./checkout ./services ./cmd/...`、`go vet ./...`、`git diff --check` 全部通过。

当前页面的抽象模板只有 FulfillmentCartItem/ItemDelivery，未返回联系人、取货时段或审核模型。不得为了取得后续模型强行选择不可用门店，后续真实验收仍待库存条件满足。

### iPhone 18 Pro 独立会话诊断（Go）

用户允许任选一款 iPhone 验证，并要求后续使用 Go。选取 `MJT74CH/A`（iPhone 18 Pro 256GB 黑色）：官方商品页和 summary 型号一致，summary 金额为 999900 分、可购买；库存接口当次返回珠江新城可取货。

Go 诊断使用全新 CookieJar，只复用 User-Agent，不导入原账号 Cookie；独立购物袋初始为空（空袋的 `items.c` 可能缺失）。正式 Duo SKU 白名单未放宽。

Pro 的 summary 返回原生 GET 表单路径为 `/shop/bag`，与 Duo 的旧路径假设不同；没有 `atbtoken` 字段，全新会话也未取得该 Cookie。仅一次提交返回的表单时，当前 HTTP 客户端未取得成功响应，立即停止，没有重复该加购请求，也没有提交订单或付款。此诊断未捕获具体 HTTP 状态，不能判断为库存不足、认证失效或反爬。

随后仅只读查看官方产品解析页（301 后到 `/shop/buy-iphone/iphone-18-pro/mjt74ch/a`，最终 200）。该页面无旧式 `add-to-cart` HTML 表单，不能把旧 summary 表单视为已验证的 Pro 加购协议。下一步需从当前页面数据或原 Chrome 的正常 Network 取得真实加购基线，再替换对应请求。

最后使用 Go CLI 对原私有账号会话只读复核，仍为原来一台 `MK2Q4CH/A`，数量 1。临时联网测试已从 checkout 包移除，诊断源码只保存在忽略的 `.omx/` 中；没有新增 Go 依赖。

### 继续排查：更正 Pro 表单地址结论

静态检查当前官方页面和其公开 `step1evolution.js` 后，确认产品 summary 仍使用页面声明的 `/shop/updateSummary`；JS 会带真实 SKU 和 `purchaseOption=fullPrice`，且在提交前从当前 Cookie 补 `atbtoken`（若存在），没有发现把 `#` 改写成实际加购路径的逻辑。

**更正上一节**：“Pro summary 返回原生 GET 表单路径为 `/shop/bag`”不是原始响应事实。原始 `buyNowButton` 的 `action` 实为 `#`，旧诊断错误地相对于当前购物袋地址解析，才得到 `/shop/bag`。不能把这次请求认作正确加购 API，也不能由此推断网站阻止加购的原因。

Go 独立会话补齐全款参数和官方 fetch trace 头后，summary 仍为 `isBuyable=true`、`disabled=false`，按钮 action 仍为 `#`；本轮在网络加购前停止。再用原账号会话作一次只读 summary 对照，结果同样为 `#`，因此不足以归因为“独立会话未登录”。原账号购物袋前后都是唯一一台 MK2Q4CH/A、数量 1。

正式 Go 加购解析现在在任何网络请求前拒绝空 action、片段占位、纯 query action 及带片段的地址；未放宽 Duo 路径/SKU 白名单。HTTP 错误仅增加数字状态码，仍不打印响应正文、URL 或 token，且不重试。新增回归测试验证占位表单零请求、302/403/541 有状态码且无敏感内容。

要继续真实审核链路，需要正常 Chrome 中一次成功的 Pro 加购 Network 基线；只被动读取，不使用页面点击自动化，不猜测或绕过未知校验。已请求用户手动加购一台测试机，不结账/付款，且明确可能与原 Duo 同袋。未取得这次真实请求前，不声称加购已修复。

### 用户手动加购后：Go 购物袋核对与收藏请求

用户实际加购的是 `MJTF4CH/A`（iPhone 18 Pro 512GB 冰川蓝色），而不是之前诊断的黑色 256GB。当前本人会话 Go 只读购物袋确认同时包含 `MJTF4CH/A × 1` 与原 `MK2Q4CH/A × 1`。本次新页面的被动 Network 列表为空，不能声称已经捕获到用户那次成功加购请求。

用户明确授权临时将 Duo 移入收藏并恢复后，诊断程序先确认模型中存在 `saveForLater` 与对应 `bagSavedItems.addToBag` 恢复动作，再对唯一 Duo 的模型动作发一次 POST `/shop/bagx`（`_a=saveForLater`，模型提供作用域与当前请求头）。实际收到 HTTP 541，停止，未重试、未进入结账。

随后仅 GET 购物袋核对，仍是 Pro 与 Duo 各 1 台，未发生可见收藏移动，因此不需要执行恢复。不能把 541 进一步断言为 Cookie 过期、地区错误或具体安全机制根因。后续需用户在普通官网正常执行移入收藏，或取得这一步真实 Network 基线；不得通过重试或猜测动作绕过响应。

本轮全为 Go HTTP；临时诊断二进制和私有模型只保存在忽略的 `.omx/purchase-runtime/`，常规 checkout 包不保留联网测试。没有提交订单或付款。

### 用户要求代操作后：普通 Chrome 收藏成功，Go 恢复成功

用户在本线程明确要求不再让其操作，已改为仅由代理在普通 Chrome 完成这次已授权的“移入收藏”基线动作；最终执行链路仍为 Go HTTP，未启用自动提交。

- 确认普通 Chrome 购物袋内 Pro 与 Duo 各 1 台后，点击精确的“移入收藏 iPhone Duo 512GB 夜空色”。页面确认已收藏，购物袋只剩 Pro，金额 1199900 分。
- 被动 Network 捕获实际 `POST /shop/bagx`，请求编号 56，HTTP 200。私有捕获文件为 `native-request-56.private.json`。内联响应被工具截断，不能把它作为可完整解析的协议响应样本。
- 捕获的当前根域请求 Cookie 与早先会话快照存在变动。仅在本机更新本人会话：已知 Cookie 保留原有 scope，新 Cookie 保守限制为 www 主机；不打印值，不读取密码。不能由此断言这就是先前 541 的唯一根因。
- Go 对单件 Pro 的结账动作仍返回需要 Apple Store 认证；普通 Chrome 直接读取 secure7 checkout 显示 session_expired。未实现或尝试登录绕过，没有填写凭据。
- 结束前优先恢复购物袋：读取官网 `bag/savedbyyou`，从本人收藏清单定位精确 MK2Q4CH/A，调用当前模型提供的全局 `bagSavedItems.addToBag` 绑定，传该清单/条目及精确 SKU，一次 POST，模型状态 200。
- 随后 Go GET 购物袋证实 MK2Q4CH/A × 1、MJTF4CH/A × 1，恢复完成。记录在 `duo-restore-result.json`，原始响应仅存在私有忽略目录。未改 Pro 数量、未提交订单、未付款。

依然未取得 Pro 成功加购的 API 基线，也未完成真实时段/联系人/审核验收。当前可证明的是库存/购物袋/先前 Duo 搜店、普通 Chrome 收藏与 Go 恢复，不得表述为整条下单链路可用。

### 已授权的访客 Go 实测：直传门店、时段、联系人页

用户明确授权同意访客流程中的 Apple 隐私政策及有限个人信息境外处理声明后，Go 读取官网当前 signed signIn URL 的模型，仅执行其声明的 `signIn.guestLogin.a.guestLogin`，没有读取/提交账号密码。

实测顺序：

1. `POST /shop/signInx`：HTTP 200、应用状态 302；字段是 deviceID 与三项同意状态，作用域按模型为 signIn。
2. 应用重定向明确包含 `method=post`、`args={pltn:...}`、目标同源 `/shop/checkout/start`。不能误当成普通 GET。
3. Go POST bootstrap 返回 HTTP 302；仅跟随同源精确 `/shop/checkout` GET，HTTP 200，活动阶段 fulfillment。
4. 选择 RETAIL 后，**没有发送 locator.search**。另一次 Go 原监控器单次查询确认 MJTF4CH/A 在 R639 当前可取货；随后直接将 `selectStore=R639` 传给模型的 locator.select。
5. 返回 locator.selectStore=R639、timeSlot.storeId=R639、enableContinue=true、单件总额 RMB 11,999，说明本次会话可省一次搜店 HTTP 请求。正式 Runner 的默认路径尚未因此自动切换。
6. 官网真实取货窗口的 `isRestricted` 为 null，且部分展示字段缺失。按公开官方 checkout.js 的 pickup CT 组件语义修复选择器：false/null 可选，可选展示字段空值，签名/时段ID/起止/时区仍必填；失败原子性受回归测试保护。
7. 官网继续按钮附带 metric 事件与仅遥测的条件事件；原 BindingAction 错把这些声明式元数据当未知动作拒绝。现仅忽略经严格形态检查的无副作用遥测，不忽略真正动作或禁用状态。
8. 修复后 Go 已实际提交 fulfillment continue，服务器活动阶段成功进入 **pickupContact**。

真实联系人模型：`checkout.pickupContact.selfPickupContact.selfContact.address`，字段为 firstName、lastName、emailAddress、**fullDaytimePhone**（不是之前合成模型的 mobilePhone），目前均空。

另有 `nationalIdSelf` enabled=true，模型 maxLen=4，校验提示明确要求政府颁发身份证件号码 **后四位**，不是完整证件号码。验证模块 authRequired=false；尚不能由此保证填写后一定不需进一步验证。没有编造任何联系资料或证件片段，也未发送联系人继续请求。

已向用户请求现有私有联系人文件路径或真实的姓、名、邮箱、手机号和证件后4位；不要收集完整证件号、密码或验证码。原 Duo 为本次隔离测试仍暂存收藏，结束前必须恢复，并做只读购物袋核对。仍未创建订单、未付款。

本轮结束前的恢复证据：Go 对精确收藏 Duo 执行一次 addToBag，模型状态 200；GET 购物袋证实 MK2Q4CH/A × 1 与 MJTF4CH/A × 1。临时 Go 诊断已关闭，无待恢复商品。继续测试前必须重新校验单件 Pro 购物袋，不得沿用恢复前的旧结账模型提交。

联系人适配已补充 `fullDaytimePhone` 与仅四位的可选 `id_last4`，采用全部校验通过才写入的原子更新；真实根模块重复 eFapiaoSelector 的表单遍历也只做了精确路径兼容。由于政府证件片段在真实审核页的回显还未验收，含 id_last4 的自动提交模式在任何网络前拒绝，不能误把本轮的字段适配当成最终提交验收。全项目测试、checkout/services/CLI race、vet、diff-check 通过，限定静态审查 CLEAR；这些检查不替代真实联系人/账单/审核 API 验收。
