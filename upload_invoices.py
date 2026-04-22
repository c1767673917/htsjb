#!/usr/bin/env python3
"""
电子发票批量上传脚本

从"上传表.csv"读取每个单号对应的发票号，
在 pdf汇总 目录中找到对应 PDF，转为 JPEG 后上传到系统。

用法:
    python3 upload_invoices.py [--base-url http://localhost:8080] [--dry-run] [--workers 8]
"""

import argparse
import csv
import io
import json
import os
import re
import sys
import threading
import time
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed

from typing import Dict, List, Tuple

import fitz  # PyMuPDF
import requests

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
CSV_PATH = os.path.join(SCRIPT_DIR, "上传表.csv")
PDF_DIR = "/Users/lichuansong/Desktop/projects/chl发票检查/pdf汇总"
PROGRESS_FILE = os.path.join(SCRIPT_DIR, "upload_progress.json")

DEFAULT_BASE_URL = "http://localhost:8080"
DEFAULT_YEAR = 2023

progress_lock = threading.Lock()


def parse_csv(path: str) -> Dict[str, List[str]]:
    order_invoices = defaultdict(set)  # type: Dict[str, set]
    with open(path, "r", encoding="utf-8-sig") as f:
        reader = csv.DictReader(f)
        for row in reader:
            order_no = row["单号"].strip()
            raw_invoices = row["发票号"].strip()
            if not order_no or not raw_invoices:
                continue
            for inv in raw_invoices.split(","):
                inv = inv.strip()
                if inv:
                    order_invoices[order_no].add(inv)
    return {k: sorted(v) for k, v in order_invoices.items()}


def build_pdf_index(pdf_dir: str) -> Dict[str, str]:
    index = {}  # type: Dict[str, str]
    pattern = re.compile(r"^dzfp_(\d+)_.*\.pdf$")
    for filename in os.listdir(pdf_dir):
        m = pattern.match(filename)
        if m:
            inv_no = m.group(1)
            if inv_no not in index:
                index[inv_no] = os.path.join(pdf_dir, filename)
    return index


def pdf_to_jpeg(pdf_path: str, dpi: int = 200) -> List[bytes]:
    doc = fitz.open(pdf_path)
    images = []
    for page in doc:
        mat = fitz.Matrix(dpi / 72, dpi / 72)
        pix = page.get_pixmap(matrix=mat)
        images.append(pix.tobytes("jpeg", jpg_quality=90))
    doc.close()
    return images


def extract_year(order_no: str) -> int:
    try:
        parts = order_no.split("_")
        if len(parts) >= 2:
            year_code = parts[1][:2]
            return 2000 + int(year_code)
    except (ValueError, IndexError):
        pass
    return DEFAULT_YEAR


def upload_one(
    base_url: str,
    year: int,
    order_no: str,
    jpeg_data_list: List[Tuple[str, bytes]],
    operator: str = "",
) -> dict:
    url = "%s/api/y/%d/orders/%s/uploads" % (base_url, year, order_no)
    files = []
    for filename, data in jpeg_data_list:
        files.append(("invoice[]", (filename, io.BytesIO(data), "image/jpeg")))
    form_data = {}
    if operator:
        form_data["operator"] = operator
    resp = requests.post(url, data=form_data, files=files, timeout=120)
    return {"status": resp.status_code, "body": resp.json() if resp.ok else resp.text}


def load_progress() -> set:
    if os.path.exists(PROGRESS_FILE):
        try:
            with open(PROGRESS_FILE, "r") as f:
                data = json.load(f)
                return set(data.get("completed", []))
        except (json.JSONDecodeError, ValueError):
            return set()
    return set()


def save_progress(completed: set) -> None:
    with open(PROGRESS_FILE, "w") as f:
        json.dump({"completed": sorted(completed)}, f, ensure_ascii=False, indent=2)


def process_order(
    order_no: str,
    pdfs: List[Tuple[str, str]],
    base_url: str,
    operator: str,
    dpi: int,
    completed: set,
    counter: dict,
) -> Tuple[str, bool, str]:
    """处理单个订单：转换 + 上传。返回 (单号, 是否成功, 消息)"""
    year = extract_year(order_no)

    jpeg_list = []
    for inv_no, pdf_path in pdfs:
        try:
            pages = pdf_to_jpeg(pdf_path, dpi=dpi)
            for page_idx, jpeg_data in enumerate(pages):
                if len(pages) == 1:
                    filename = "%s.jpg" % inv_no
                else:
                    filename = "%s_p%d.jpg" % (inv_no, page_idx + 1)
                jpeg_list.append((filename, jpeg_data))
        except Exception as e:
            return (order_no, False, "PDF 转换失败: %s - %s" % (inv_no, e))

    if not jpeg_list:
        return (order_no, False, "无图片")

    try:
        result = upload_one(base_url, year, order_no, jpeg_list, operator=operator)
        if result["status"] == 200:
            counts = result["body"].get("counts", {})
            with progress_lock:
                completed.add(order_no)
                counter["success"] += 1
                idx = counter["success"] + counter["fail"]
                save_progress(completed)
            return (order_no, True, "发票数: %s" % counts.get("发票", "?"))
        else:
            with progress_lock:
                counter["fail"] += 1
            return (order_no, False, "HTTP %d - %s" % (result["status"], result["body"]))
    except requests.exceptions.ConnectionError:
        with progress_lock:
            counter["fail"] += 1
        return (order_no, False, "连接失败")
    except Exception as e:
        with progress_lock:
            counter["fail"] += 1
        return (order_no, False, "异常: %s" % e)


def main():
    parser = argparse.ArgumentParser(description="电子发票批量上传")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help="服务器地址")
    parser.add_argument("--dry-run", action="store_true", help="仅检查匹配，不实际上传")
    parser.add_argument("--operator", default="发票脚本", help="录入人名称")
    parser.add_argument("--dpi", type=int, default=200, help="PDF 转图片 DPI")
    parser.add_argument("--workers", type=int, default=8, help="并发线程数")
    parser.add_argument("--limit", type=int, default=0, help="限制上传单号数量，0=不限")
    parser.add_argument("--reset", action="store_true", help="清除上传进度，从头开始")
    args = parser.parse_args()

    if args.reset and os.path.exists(PROGRESS_FILE):
        os.remove(PROGRESS_FILE)
        print("已清除上传进度记录。")

    print("读取 CSV: %s" % CSV_PATH)
    order_map = parse_csv(CSV_PATH)
    print("共 %d 个单号" % len(order_map))

    print("\n建立 PDF 索引: %s" % PDF_DIR)
    pdf_index = build_pdf_index(PDF_DIR)
    print("索引完成: %d 个 PDF 文件" % len(pdf_index))

    total_invoices = sum(len(v) for v in order_map.values())
    found = 0
    not_found_invoices = []
    order_pdfs = {}  # type: Dict[str, List[Tuple[str, str]]]

    for order_no, invoice_nos in order_map.items():
        pdfs = []
        for inv_no in invoice_nos:
            pdf_path = pdf_index.get(inv_no)
            if pdf_path:
                pdfs.append((inv_no, pdf_path))
                found += 1
            else:
                not_found_invoices.append((order_no, inv_no))
        if pdfs:
            order_pdfs[order_no] = pdfs

    print("\n发票总数: %d, 找到 PDF: %d, 未找到: %d" % (total_invoices, found, len(not_found_invoices)))

    if not_found_invoices:
        print("\n未找到 PDF 的发票 (前 20 条):")
        for order_no, inv_no in not_found_invoices[:20]:
            print("  %s -> %s" % (order_no, inv_no))
        if len(not_found_invoices) > 20:
            print("  ... 还有 %d 条" % (len(not_found_invoices) - 20))

    if args.dry_run:
        print("\n[干运行模式] 共 %d 个单号可上传，跳过实际上传。" % len(order_pdfs))
        return

    if not order_pdfs:
        print("没有可上传的发票，退出。")
        return

    completed = load_progress()
    to_upload = {k: v for k, v in order_pdfs.items() if k not in completed}

    if len(completed) > 0:
        print("\n已完成 %d 个单号，剩余 %d 个待上传。" % (len(completed), len(to_upload)))

    if not to_upload:
        print("所有单号均已上传完成！如需重新上传，请加 --reset 参数。")
        return

    if args.limit > 0:
        items = list(to_upload.items())[:args.limit]
        to_upload = dict(items)

    total = len(to_upload)
    print("\n开始并发上传 (workers=%d)，共 %d 个单号..." % (args.workers, total))
    counter = {"success": 0, "fail": 0}
    start_time = time.time()

    try:
        with ThreadPoolExecutor(max_workers=args.workers) as executor:
            futures = {}
            for order_no, pdfs in to_upload.items():
                f = executor.submit(
                    process_order,
                    order_no, pdfs, args.base_url, args.operator, args.dpi,
                    completed, counter,
                )
                futures[f] = order_no

            for f in as_completed(futures):
                order_no, ok, msg = f.result()
                done = counter["success"] + counter["fail"]
                elapsed = time.time() - start_time
                rate = done / elapsed if elapsed > 0 else 0
                eta = (total - done) / rate if rate > 0 else 0
                status = "OK" if ok else "FAIL"
                print("[%d/%d %.1f/s ETA %.0fs] %s %s - %s" % (
                    done, total, rate, eta, status, order_no, msg
                ))
    except KeyboardInterrupt:
        print("\n\n用户中断，已保存进度。下次运行将从断点继续。")
        save_progress(completed)
        sys.exit(1)

    elapsed = time.time() - start_time
    print("\n上传完成! 成功: %d, 失败: %d, 总计: %d, 耗时: %.1f 秒" % (
        counter["success"], counter["fail"], total, elapsed
    ))


if __name__ == "__main__":
    main()
