# Apple Store Helper

监听 Apple Store 门店自提库存。有货时立即提醒，并打开购物车页面，帮你抢到想要的 iPhone。

支持 macOS、Windows、Linux。下载后直接运行，不需要安装任何依赖。还有不需要桌面环境的[命令行版](#命令行版)，适合放在服务器上长期运行。

![UI](screenshot.png)

## 下载

前往 [Releases](https://github.com/jandyx/apple-store-helper/releases) 下载对应系统的压缩包：

| 系统 | 文件 |
| --- | --- |
| macOS（Apple 芯片） | `AppleStoreHelper-macos-arm64.zip` |
| Windows 10 / 11（64 位） | `AppleStoreHelper-windows-amd64.zip` |
| Linux（x86_64） | `AppleStoreHelper-linux-amd64.zip` |
| 命令行版 | `AppleStoreHelper-cli-<系统>-<架构>.zip`，如 `cli-linux-amd64`、`cli-linux-arm64`、`cli-darwin-arm64`、`cli-windows-amd64` |

### 首次打开

程序没有付费签名，第一次打开时系统会拦截：

- **macOS**：提示「已损坏」或「无法验证开发者」时，在终端执行下面的命令，然后再双击打开。路径换成你实际存放 App 的位置。
  ```shell
  xattr -cr "/Applications/Apple Store Helper.app"
  ```
- **Windows**：出现「Windows 已保护你的电脑」时，点「更多信息」，再点「仍要运行」。

## 功能

- **支持机型**：iPhone Duo、iPhone 18 Pro / Pro Max、iPhone 17、iPhone Air
- **支持地区**：中国大陆、中国香港、中国台湾、新加坡、日本、澳大利亚、马来西亚
- **门店和型号都能多选**：一次添加所选门店与型号的全部组合，并发查询
- **多个地区同时监听**：切换地区后继续添加，列表不会清空，各地区一起查询
- **单条停用和删除**：监听列表每一行都能临时停用或删除，不用整个清空重来
- **有货记录**：每次有货都会记下时间、门店和型号，并统计哪家店最常放货
- **查询频率可调**：最快 0.3 秒一次。可以从下拉框选，也可以直接输入，如 `1.5`、`800ms`
- **有货时多重提醒**：系统通知、提示音，并自动打开购物车页面
- **手机推送**：支持 Bark、Server酱（推到微信）、企业微信群机器人、Telegram 和通用 Webhook，可以同时填多个
- **后台运行**：关闭窗口会收进系统托盘，继续监听。点托盘图标、Dock 图标或再次打开 App 就能找回窗口
- **查询失败看得见**：被拦截、超时、门店编号失效时，列表会显示具体原因，不会误显示成「无货」
- **开售前自检**：用「测试通知」「试听」「测试打开购物车」「测试手机推送」按钮，提前确认每个环节都正常
- **自动保存配置**：地区、门店、型号、监听列表和查询频率都会保存，下次打开直接使用

## 使用方法

### 开售前准备

1. 在浏览器里登录 Apple 官网账号，把要买的型号**加入购物车**。程序只负责监控和提醒，下单要你自己完成。
2. 打开程序，依次点「测试通知」「试听(有货提示音)」「测试打开购物车」。
   - 在 macOS 上第一次点「测试通知」时，系统会请求通知权限，请选择允许。
   - 建议在「系统设置 → 通知 → Apple Store Helper」里，把通知样式改成「提醒」。这样通知会一直停在屏幕上，不会自动消失。

### 开始监听

1. 选择**地区**，再选**门店**（可多选）和**型号**（可多选）。
2. 点「添加」，所选组合会出现在监听列表里。想同时盯其他地区，切换地区后再添加即可。
3. 选好查询频率，点「开始」。
4. 有货时，程序会发出通知、播放提示音、自动打开购物车页面，并暂停监听。在购物车页面选择门店和取货时间，完成下单。
5. 要继续监听，再点一次「开始」。

### 手机推送地址

在「手机推送」框里每行填一个地址，程序会按域名自动识别渠道。填好后点「测试手机推送」，会逐条显示每个地址是否发送成功。

| 渠道 | 地址格式 | 获取方式 |
| --- | --- | --- |
| Server酱（推到微信） | `https://sctapi.ftqq.com/<SendKey>.send` | 在 [sct.ftqq.com](https://sct.ftqq.com) 用微信扫码登录，复制 SendKey |
| 企业微信群机器人 | `https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=<key>` | 在企业微信群里添加「群机器人」，复制 Webhook 地址 |
| Bark（iOS） | `https://api.day.app/<key>` | 在 Bark App 里复制地址 |
| Telegram | `https://api.telegram.org/bot<token>/sendMessage?chat_id=<id>` | 通过 @BotFather 创建机器人 |
| 通用 Webhook | 任意 `https://` 地址 | 程序会 POST 一段 JSON：`{"title","content","url"}` |

推送地址里包含密钥，不要截图或公开分享。

### 注意事项

- 预售阶段，门店自提可能一直显示无货。程序没检测到库存，不代表一定买不到，可以同时留意线上发货。
- 查询太频繁可能被 Apple 临时限流，一般设成 1 到 3 秒比较稳妥。
- 这不是外挂：程序不会自动下单，也不会接触你的 Apple ID 账号和密码。

## 配置文件位置

| 系统 | 路径 |
| --- | --- |
| macOS | `~/Library/Application Support/apple-store-helper/user_settings.json` |
| Windows | `%AppData%\apple-store-helper\user_settings.json` |
| Linux | `~/.config/apple-store-helper/user_settings.json` |

同一目录下还有有货记录 `history.json` 和运行日志 `app.log`。点程序里的「打开日志」可以直接打开这个目录，遇到问题时把 `app.log` 发出来即可排查。

## 命令行版

不需要桌面环境，适合放在服务器、NAS 或一直开着的电脑上，配合手机推送使用。从 Releases 下载 `AppleStoreHelper-cli-*.zip`，解压后运行：

```shell
# 查看门店和型号
./apple-store-helper-cli -list-stores -area cn -store 广东
./apple-store-helper-cli -list-products -area cn -product "duo 夜空色"

# 监听广州两家店的全部 iPhone Duo，3 秒一轮，有货推送到 Server酱
./apple-store-helper-cli -area cn -store 天环,珠江新城 -product duo -interval 3s \
  -push https://sctapi.ftqq.com/<SendKey>.send

# 直接使用图形版保存的监听列表、查询频率和推送地址
./apple-store-helper-cli -use-gui-config
```

- 门店可以写编号（如 `R639`）或名称关键字（如 `天环`）。型号可以写型号编号，也可以写关键字；一个参数里用空格隔开的多个关键字需要同时满足，比如 `"duo 512gb"`。
- 默认一直监听，只在某一项**变成有货**时推送一次；加 `-stop-on-hit` 则有货后退出。
- `-once` 只查询一轮就退出，退出码 0 表示有货，1 表示无货，2 表示查询失败，方便放进其他脚本。
- 终端只打印状态变化，外加每分钟一行运行状态；诊断日志写在 `app.log`，加 `-v` 可同时输出到终端。
- 所有参数见 `./apple-store-helper-cli -h`。

## 开发

- Go 1.24+
- GUI 框架：[Fyne](https://github.com/fyne-io/fyne)
- 库存接口：`/shop/retail/pickup-message`。每次请求最多查 20 个型号，超出部分会自动分批请求

### 运行

```shell
go run .                                 # 图形版
go run ./cmd/apple-store-helper-cli -h   # 命令行版
CGO_ENABLED=0 go test ./services/ ./cmd/...
```

`services` 包不依赖图形和音频库，图形版和命令行版共用；图形相关代码都在根目录的 `main.go`、`ui_*.go`、`tray.go`。

在 Linux 上，需要先安装依赖：

```shell
sudo apt-get install -y libgl1-mesa-dev xorg-dev libasound2-dev
```

### 打包

**GitHub Actions（推荐）**：推送到 `main` 时，会自动构建三个平台的图形版和多个平台的命令行版，可以在 Actions 页面下载。推送标签（如 `1.8.0`）时，还会自动发布 Release，程序里显示的版本号就是这个标签名。

```shell
git tag -a 1.8.0 -m "1.8.0" && git push origin 1.8.0
```

**在本地打包 macOS 版**：

```shell
./build.sh
```

**在 Mac 上交叉编译 Windows 版**：

```shell
brew install mingw-w64
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -ldflags "-s -w -H windowsgui" -o "Apple Store Helper.exe" .
```

### 机型与门店数据

机型数据在 `config/files/products_<地区>.json`，取自各地区购买页里的 `productSelectionData`；门店数据在 `config/files/stores.json`，取自 Apple 零售店列表页。

两份数据都由脚本抓取：

```shell
python3 scripts/fetch_products.py              # 刷新全部地区的机型
python3 scripts/fetch_products.py --check-new  # 检查是否出现未收录的新机型
python3 scripts/fetch_stores.py --dry-run      # 只报告门店增减，不写文件
```

新机型发布后，把购买页的路径（如 `iphone-19`）加进 `scripts/fetch_products.py` 的 `SLUGS`。开售前 Apple 商店维护期间，脚本会跳过并保留原数据。

### 定时任务

- **Update data**：每天北京时间 9:00 运行上面两个脚本，数据有变化时推到 `auto/data-update` 分支并开 PR。需要在仓库 Settings → Actions → General 勾选「Allow GitHub Actions to create and approve pull requests」，否则只推分支，并在运行摘要里给出创建 PR 的链接。
- **API health**：每天北京时间 9:30 用真实接口逐个地区查一次库存。接口被拦截或改版时任务会失败，GitHub 会发邮件提醒。商店维护期间不算失败。

## 更新记录

- 从社区 fork [dashug/apple-store-helper](https://github.com/dashug/apple-store-helper) 引入多渠道手机推送、查询间隔随机抖动与并发上限、失败原因展示、日志文件、系统托盘、多地区同时监听、单条停用和删除、有货记录、命令行版，以及每日数据更新和接口健康检查。

- iPhone 17 Pro / Pro Max 已从 Apple 官网下架，已从型号列表中移除。
- 日本的同一个型号会按运营商（SoftBank、docomo、SIM 免费版）重复出现，现在会按型号编号去重。

## License

本项目采用 [GPL-3.0](LICENSE) 许可。转发或修改后再分发时，请附上源码地址。

本项目 fork 自 [hteen/apple-store-helper](https://github.com/hteen/apple-store-helper)。

## Contributors

- [@Hteen](https://github.com/hteen)
- [@Timssse](https://github.com/Timssse)
- [@Black-Hole](https://github.com/BlackHole1)
- [@RayJason](https://github.com/RayJason)
- [@Warkeeper](https://github.com/Warkeeper)
