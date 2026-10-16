#!/usr/bin/env python3
"""
重新生成 config/files/products_<locale>.json

移植自 https://github.com/dashug/apple-store-helper 的 scripts/fetch_products.py，
改动：中国大陆走 apple.com.cn；开售前商店维护（预购封面页）时跳过并保留原文件，不算失败。

机型数据来自 Apple 各地区购买页中内嵌的
window.PRODUCT_SELECTION_BOOTSTRAP.productSelectionData，
此前需要手动开发者工具复制，这里自动完成。

用法:
    python3 scripts/fetch_products.py                  # 全部地区
    python3 scripts/fetch_products.py --locale zh_CN   # 指定地区
    python3 scripts/fetch_products.py --dry-run        # 只报告，不写文件

新一代机型发布后，通常只需更新下面的 SLUGS。
可用的购买页可以从 https://www.apple.com/<shortcode>/shop/buy-iphone 页面里找到。
"""

import argparse
import json
import pathlib
import re
import sys
import time
import urllib.error
import urllib.request

# (locale, shortCode) —— 必须与 model/area.go 中的 Areas 保持一致
AREAS = [
    ("zh_CN", "cn"),
    ("zh_HK", "hk-zh"),
    ("zh_TW", "tw"),
    ("en_SG", "sg"),
    ("ja_JP", "jp"),
    ("en_AU", "au"),
    ("en_MY", "my"),
]

# 购买页，顺序决定型号在下拉框中的先后
SLUGS = [
    "iphone-17",
    "iphone-air",
    "iphone-18-pro",
    "iphone-duo",
]

UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)
KEY = "productSelectionData:"
BUY_INDEX = "shop/buy-iphone"

# 索引页上并非机型的链接
NOT_A_MODEL = {"carrier-offers"}

# 明确不纳入监控的购买页。上一代机型供货充足，不是这个工具的目标，
# 列在这里是为了让每日检查不必反复报告它们。
# iphone-17-pro 已被 Apple 下架，部分地区索引页仍残留链接
IGNORED_SLUGS = {"iphone-16", "iphone-17e", "iphone-17-pro"}

# 开售前 Apple 会把整个商店切到预购封面页，所有购买页返回 503 且带这个标记
COVER_MARK = re.compile(r'"cv"\s*:\s*"preorder"')


class StoreClosed(Exception):
    """商店维护中（预购封面页）"""


def shop_base(short):
    """站点根地址。中国大陆必须用 apple.com.cn，apple.com/cn 会被拦截或跳转"""
    if short == "cn":
        return "https://www.apple.com.cn"
    return f"https://www.apple.com/{short}"
OUT_DIR = pathlib.Path(__file__).resolve().parent.parent / "config" / "files"


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept-Language": "en,zh;q=0.9"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as e:
        if e.code == 503:
            body = e.read().decode("utf-8", errors="replace")
            if COVER_MARK.search(body):
                raise StoreClosed(url) from e
        raise


def extract(html):
    """括号配平地取出 productSelectionData 对象，需正确跳过字符串内的转义引号"""
    i = html.find(KEY)
    if i < 0:
        return None
    j = html.index("{", i + len(KEY))

    depth, in_str, esc = 0, False, False
    for k in range(j, len(html)):
        c = html[k]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
            continue
        if c == '"':
            in_str = True
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return json.loads(html[j : k + 1])
    return None


def families(data):
    return sorted({p.get("familyType") for p in data.get("products", [])})


def discover_slugs(short):
    """从 /shop/buy-iphone 索引页列出当前在售的购买页"""
    html = fetch(f"{shop_base(short)}/{BUY_INDEX}")
    found = set(re.findall(r"/shop/buy-iphone/([a-z0-9][a-z0-9-]*)", html))
    return sorted(found - NOT_A_MODEL)


def is_real_buy_page(short, slug):
    """确认这个 slug 真的是一个可用的购买页

    索引页会残留已下架机型的链接（实测 jp 索引页一度列出 iphone-17-pro，
    而该页面实际 301）。不做这一步校验，每日检查就会周期性误报。
    """
    try:
        html = fetch(f"{shop_base(short)}/{BUY_INDEX}/{slug}")
    except Exception:
        return False
    return KEY in html


def check_new(areas):
    """报告 Apple 站点上有、但 SLUGS 未收录的购买页

    新机型的购买页会先出现在索引页上，这一步让它当天就被发现，
    而不是等用户反馈「监控列表里没有新机型」。
    """
    known = set(SLUGS) | IGNORED_SLUGS
    missing = {}

    for locale, short in areas:
        try:
            slugs = discover_slugs(short)
        except StoreClosed:
            print(f"  {locale}: 商店维护中（预购封面页），跳过")
            continue
        except Exception as e:
            print(f"  {locale}: 索引页读取失败 {e}")
            continue

        candidates = [s for s in slugs if s not in known]

        # 逐个校验，滤掉索引页上的陈旧链接
        confirmed = []
        for slug in candidates:
            if is_real_buy_page(short, slug):
                confirmed.append(slug)
            else:
                print(f"  {locale}: {slug} 在索引页出现但不是有效购买页，忽略")
            time.sleep(0.5)

        if confirmed:
            missing[locale] = confirmed

        note = f"，未收录 {confirmed}" if confirmed else ""
        print(f"  {locale}: {len(slugs)} 个购买页{note}")

    if missing:
        merged = sorted({s for v in missing.values() for s in v})
        print()
        print(f"发现未收录的购买页: {' '.join(merged)}")
        print("确认需要监控的话，把它们加进 scripts/fetch_products.py 的 SLUGS；")
        print("确认不需要（例如上一代机型），加进 IGNORED_SLUGS。")
        return 2

    print("\n没有未收录的购买页。")
    return 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--locale", action="append", help="只处理指定地区，可重复")
    ap.add_argument("--dry-run", action="store_true", help="只报告，不写文件")
    ap.add_argument("--delay", type=float, default=1.0, help="请求间隔秒数")
    ap.add_argument(
        "--check-new",
        action="store_true",
        help="只检查是否出现了 SLUGS 未收录的购买页，退出码 2 表示发现新机型",
    )
    args = ap.parse_args()

    areas = [a for a in AREAS if not args.locale or a[0] in args.locale]

    if args.check_new:
        print("检查未收录的购买页：")
        return check_new(areas)
    failed = False

    for locale, short in areas:
        blocks, report = [], []
        closed = False

        for slug in SLUGS:
            url = f"{shop_base(short)}/shop/buy-iphone/{slug}"
            try:
                html = fetch(url)
            except StoreClosed:
                closed = True
                report.append(f"{slug}: 商店维护中（预购封面页）")
                break
            except urllib.error.HTTPError as e:
                # 机型并非每个地区都有售，404 属正常情况
                report.append(f"{slug}: HTTP {e.code}，跳过")
                continue
            except Exception as e:
                report.append(f"{slug}: 请求失败 {e}")
                failed = True
                continue

            data = extract(html)
            if data is None:
                report.append(f"{slug}: 未找到 productSelectionData")
                failed = True
                continue

            n = len(data.get("products", []))
            if n == 0:
                report.append(f"{slug}: products 为空，跳过")
                continue

            blocks.append(data)
            report.append(f"{slug}: {n} 个 SKU  {'+'.join(families(data))}")
            time.sleep(args.delay)

        print(f"\n=== {locale} ({short}) ===")
        for line in report:
            print(f"  {line}")

        if closed:
            # 维护期间数据不完整，写入会丢掉机型；保留原文件，下次再刷新
            print("  商店维护中，保留原文件")
            continue

        if not blocks:
            print("  !! 没有任何数据，保留原文件")
            failed = True
            continue

        total = sum(len(b.get("products", [])) for b in blocks)
        print(f"  合计 {len(blocks)} 组 / {total} 个 SKU")

        if not args.dry_run:
            out = OUT_DIR / f"products_{locale}.json"
            out.write_text(json.dumps(blocks, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            print(f"  已写入 {out.relative_to(OUT_DIR.parent.parent)}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
