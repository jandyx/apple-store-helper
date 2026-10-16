#!/usr/bin/env python3
"""
重新生成 config/files/stores.json

移植自 https://github.com/dashug/apple-store-helper 的 scripts/fetch_stores.py。

门店数据来自 Apple 零售店列表页 Next.js 数据中的
props.pageProps.storeList，一次请求即可拿到全部地区。

用法:
    python3 scripts/fetch_stores.py            # 写入文件
    python3 scripts/fetch_stores.py --dry-run  # 只报告差异，不写文件
"""

import argparse
import json
import pathlib
import re
import sys
import urllib.request

# 必须与 model/area.go 中的 Areas 保持一致，顺序也一致
LOCALES = ["zh_CN", "zh_HK", "zh_TW", "en_SG", "ja_JP", "en_AU", "en_MY"]

# 入口域名决定港澳地区返回的语言变体：
# apple.com 给的是 en_HK / en_MO，apple.com.cn 给的才是 zh_HK / zh_MO。
# 本项目需要 zh_HK，因此固定使用 .cn 入口。
STORELIST_URL = "https://www.apple.com.cn/retail/storelist/"

UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)
NEXT_DATA = re.compile(
    r'<script id="__NEXT_DATA__" type="application/json">(.*?)</script>', re.S
)
OUT_FILE = pathlib.Path(__file__).resolve().parent.parent / "config" / "files" / "stores.json"


def fetch_store_list():
    req = urllib.request.Request(STORELIST_URL, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=30) as resp:
        html = resp.read().decode("utf-8", errors="replace")

    match = NEXT_DATA.search(html)
    if not match:
        raise RuntimeError("页面中没有 __NEXT_DATA__，零售店列表页结构可能已变更")

    data = json.loads(match.group(1))
    store_list = data.get("props", {}).get("pageProps", {}).get("storeList")
    if not store_list:
        raise RuntimeError("__NEXT_DATA__ 中没有 props.pageProps.storeList")

    return {block["locale"]: block for block in store_list if "locale" in block}


def store_names(block):
    """返回 {门店号: 展示名}，与 services/store.go 的取名方式一致"""
    names = {}

    if block.get("hasStates"):
        for state in block.get("state") or []:
            for store in state.get("store") or []:
                names[store["id"]] = f'{state.get("name")}-{store.get("name")}'
    else:
        for store in block.get("store") or []:
            names[store["id"]] = f'{store.get("address", {}).get("city")}-{store.get("name")}'

    return names


def load_current():
    if not OUT_FILE.exists():
        return {}
    return {b["locale"]: b for b in json.loads(OUT_FILE.read_text(encoding="utf-8"))}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true", help="只报告差异，不写文件")
    args = ap.parse_args()

    live = fetch_store_list()

    missing = [loc for loc in LOCALES if loc not in live]
    if missing:
        # 宁可失败也不要写入不完整的数据：少了哪个地区，用户就完全选不到那里的门店
        print(f"零售店列表中缺少地区: {', '.join(missing)}", file=sys.stderr)
        print(f"（页面实际包含: {', '.join(sorted(live))}）", file=sys.stderr)
        return 1

    current = load_current()
    blocks = []
    added_total = removed_total = 0

    for locale in LOCALES:
        block = live[locale]
        blocks.append(block)

        new_names = store_names(block)
        old_names = store_names(current.get(locale, {}))

        added = sorted(set(new_names) - set(old_names))
        removed = sorted(set(old_names) - set(new_names))
        added_total += len(added)
        removed_total += len(removed)

        note = ""
        if added:
            note += "  新增: " + ", ".join(new_names[i] for i in added)
        if removed:
            note += "  已关闭: " + ", ".join(old_names[i] for i in removed)

        print(f"  {locale:8} {len(new_names):>3} 家{note}")

    print(f"\n合计 {sum(len(store_names(b)) for b in blocks)} 家门店"
          f"（新增 {added_total}，关闭 {removed_total}）")

    if args.dry_run:
        print("dry-run，未写入文件")
        return 0

    if added_total == 0 and removed_total == 0 and current:
        print("门店数据无变化")
        return 0

    OUT_FILE.write_text(
        json.dumps(blocks, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(f"已写入 {OUT_FILE.relative_to(OUT_FILE.parent.parent.parent)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
