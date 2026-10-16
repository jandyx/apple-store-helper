# Apple Store Helper

监听 Apple Store 门店自提库存。有货时立即提醒，并打开购物车页面，帮你抢到想要的 iPhone。

支持 macOS、Windows、Linux。下载后直接运行，不需要安装任何依赖。

![UI](screenshot.png)

## 下载

前往 [Releases](https://github.com/jandyx/apple-store-helper/releases) 下载对应系统的压缩包：

| 系统 | 文件 |
| --- | --- |
| macOS（Apple 芯片） | `AppleStoreHelper-macos-arm64.zip` |
| Windows 10 / 11（64 位） | `AppleStoreHelper-windows-amd64.zip` |
| Linux（x86_64） | `AppleStoreHelper-linux-amd64.zip` |

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
- **查询频率可调**：最快 0.3 秒一次。可以从下拉框选，也可以直接输入，如 `1.5`、`800ms`
- **有货时多重提醒**：系统通知、提示音，并自动打开购物车页面
- **开售前自检**：用「测试通知」「试听」「测试打开购物车」三个按钮，提前确认每个环节都正常
- **自动保存配置**：地区、门店、型号、监听列表和查询频率都会保存，下次打开直接使用

## 使用方法

### 开售前准备

1. 在浏览器里登录 Apple 官网账号，把要买的型号**加入购物车**。程序只负责监控和提醒，下单要你自己完成。
2. 打开程序，依次点「测试通知」「试听(有货提示音)」「测试打开购物车」。
   - 在 macOS 上第一次点「测试通知」时，系统会请求通知权限，请选择允许。
   - 建议在「系统设置 → 通知 → Apple Store Helper」里，把通知样式改成「提醒」。这样通知会一直停在屏幕上，不会自动消失。

### 开始监听

1. 选择**地区**，再选**门店**（可多选）和**型号**（可多选）。
2. 点「添加」，所选组合会出现在监听列表里。
3. 选好查询频率，点「开始」。
4. 有货时，程序会发出通知、播放提示音、自动打开购物车页面，并暂停监听。在购物车页面选择门店和取货时间，完成下单。
5. 要继续监听，再点一次「开始」。

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

点「清空」会清掉监听列表，并删除这个文件。

## 开发

- Go 1.24+
- GUI 框架：[Fyne](https://github.com/fyne-io/fyne)
- 库存接口：`/shop/retail/pickup-message`。每次请求最多查 20 个型号，超出部分会自动分批请求

### 运行

```shell
go run .
```

在 Linux 上，需要先安装依赖：

```shell
sudo apt-get install -y libgl1-mesa-dev xorg-dev libasound2-dev
```

### 打包

**GitHub Actions（推荐）**：推送到 `main` 时，会自动构建三个平台的安装包，可以在 Actions 页面下载。推送标签（如 `1.8.0`）时，还会自动发布 Release，程序里显示的版本号就是这个标签名。

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

### 更新机型数据

机型数据在 `config/files/products_<地区>.json`，取自各地区购买页里的 `productSelectionData`。更新方法：打开购买页，在开发者工具的 Elements 面板里搜索 `productSelectionData`，把它的值复制出来。

门店数据在 `config/files/stores.json`。

## 更新记录

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
