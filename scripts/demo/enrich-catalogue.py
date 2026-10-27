#!/usr/bin/env python3
"""Shape the local demo catalogue for the customer app (D-273).

Local and test only. It cleans junk Open Facts names, fills a pack size and
unit where the import left "1 pack", and gives every store item a realistic
MRP and selling price so the cards show a real saving. It never runs against
staging or prod, and it changes no stock: quantities and movements stay as the
catalogue writer left them.

Usage:
  set -a; . infra/local/local.env; . .env.local; set +a
  APP_ENV=local python3 scripts/demo/enrich-catalogue.py [--dry-run]

The database URL is read from DATABASE_URL and never printed.
"""

from __future__ import annotations

import hashlib
import os
import re
import subprocess
import sys

DATE_RANGE = re.compile(
    r"^\s*\d{1,2}\s+[A-Za-z]{3,9}\s+\d{4}\s+(?:to|[-–])\s+\d{1,2}\s+[A-Za-z]{3,9}\s+\d{4}\s*$",
    re.I,
)
PACK_ONLY = re.compile(r"^\s*[\d.,\s]*(?:kg|g|gm|gms|ml|l|ltr|litre|litres|pack|pc|pcs|pieces?)\s*$", re.I)
SIZE = re.compile(
    r"(?<![\w.])(\d{1,4}(?:[.,]\d{1,3})?)\s*(kg|g|gm|gms|ml|l|ltr|litre|litres|pack|pc|pcs|pieces?|tablets|capsules|sachets)\b",
    re.I,
)
BAD_NAME = re.compile(r"(?:https?://|www\.|@|₹\s*\d)", re.I)

# slug -> (default pack size, unit)
DEFAULT_PACK = {
    "staples": (1, "kg"),
    "spices-oils": (1, "l"),
    "dairy-eggs": (500, "g"),
    "snacks": (100, "g"),
    "beverages": (750, "ml"),
    "personal-care": (200, "ml"),
    "household": (500, "ml"),
    "bakery": (200, "g"),
    "fruits-vegetables": (1, "kg"),
    "electrical": (1, "pc"),
    "tools": (1, "pc"),
    "plumbing": (1, "pc"),
    "paints": (1, "l"),
    "baby-care": (1, "pack"),
    "medicines": (1, "pack"),
    "hair-care": (200, "ml"),
    "skin-care": (100, "ml"),
    "makeup": (1, "pc"),
    "stationery": (1, "pack"),
    "hardware": (1, "pc"),
}

# slug -> rupees per gram (or per millilitre), or per piece for the last group.
PER_UNIT = {
    "staples": 0.075,
    "spices-oils": 0.20,
    "dairy-eggs": 0.10,
    "snacks": 0.32,
    "beverages": 0.09,
    "personal-care": 0.65,
    "household": 0.22,
    "bakery": 0.28,
    "fruits-vegetables": 0.07,
    "hair-care": 0.55,
    "skin-care": 0.90,
    "paints": 0.25,
}
PER_PIECE = {
    "personal-care": 150,
    "household": 130,
    "hair-care": 220,
    "skin-care": 300,
    "electrical": 260,
    "tools": 320,
    "plumbing": 160,
    "makeup": 320,
    "stationery": 160,
    "hardware": 220,
    "baby-care": 360,
    "medicines": 130,
}

PREMIUM = (
    "amul", "nestle", "maggi", "fortune", "tata", "aashirvaad", "haldiram", "britannia",
    "parle", "itc", "dabur", "himalaya", "colgate", "pepsodent", "dove", "lifebuoy",
    "surf excel", "ariel", "vicks", "horlicks", "boost", "bournvita", "cadbury", "kitkat",
    "kellogg", "quaker", "saffola", "mdh", "everest", "catch", "bikaji", "balaji", "unibic",
)


def rupees(n: int, step: int) -> int:
    return step * round(n / step)


def parse_size(name: str) -> tuple[float, str] | None:
    m = SIZE.search(name)
    if not m:
        return None
    qty = float(m.group(1).replace(",", "."))
    unit = m.group(2).lower()
    if unit in ("gm", "gms", "g"):
        unit = "g"
    elif unit in ("ltr", "litre", "litres", "l"):
        unit = "l"
    elif unit in ("pc", "pcs", "pieces", "piece"):
        unit = "pc"
    elif unit in ("tablets", "capsules", "sachets"):
        unit = "pack"
    if qty <= 0:
        return None
    return qty, unit


def pack_label(size: float, unit: str) -> str:
    qty = int(size) if size == int(size) else size
    return f"{qty} {unit}"


def clean_name(name: str, brand: str, category: str, size: float, unit: str) -> str:
    n = " ".join(name.split())
    digits = sum(ch.isdigit() for ch in n)
    looks_like_a_code = bool(n) and digits >= 6 and digits >= len(n) * 0.6
    if (
        not n
        or DATE_RANGE.match(n)
        or PACK_ONLY.match(n)
        or BAD_NAME.search(n)
        or looks_like_a_code
        or re.match(r"^(?:mrp|best before|net\s*(?:wt|weight)|ingredients)\b", n, re.I)
    ):
        n = f"{brand} {category} {pack_label(size, unit)}".strip()
    if n.islower() and len(n) > 3:
        n = n[0].upper() + n[1:]
    return n[:120]


def price_for(category: str, size: float, unit: str, brand: str, key: str) -> tuple[int, int]:
    # grams or millilitres; pieces and packs count one each
    if unit == "kg":
        qty = size * 1000
    elif unit == "l":
        qty = size * 1000
    elif unit in ("g", "ml"):
        qty = size
    else:
        qty = size
    if category in PER_PIECE:
        base = PER_PIECE[category] * max(qty, 1)
    else:
        base = PER_UNIT.get(category, 0.10) * max(qty, 1)
    # A stable per-product variance, then a premium-brand lift.
    h = int(hashlib.sha256(key.encode()).hexdigest()[:8], 16)
    base *= 0.88 + (h % 25) / 100.0  # 0.88 to 1.12
    if brand and any(p in brand.lower() for p in PREMIUM):
        base *= 1.18
    sale = max(1000, int(base * 100))
    step = 100 if sale < 10000 else 500
    sale = rupees(sale, step)
    markup = 1.18 + ((h >> 8) % 22) / 100.0  # 1.18 to 1.39
    mrp = rupees(int(sale * markup), step)
    if mrp <= sale:
        mrp = sale + step
    return mrp, sale


def psql(url: str, args: list[str], stdin: str | None = None) -> str:
    return subprocess.run(
        ["psql", url, "-v", "ON_ERROR_STOP=1", "-At", "-F", "\t", *args],
        input=stdin,
        text=True,
        capture_output=True,
        check=True,
    ).stdout


def main() -> int:
    if os.environ.get("APP_ENV") not in ("local", "test"):
        print("enrich-catalogue: APP_ENV must be local or test", file=sys.stderr)
        return 2
    url = os.environ.get("DATABASE_URL")
    if not url:
        print("enrich-catalogue: DATABASE_URL is not set", file=sys.stderr)
        return 2
    dry = "--dry-run" in sys.argv[1:]

    rows = psql(
        url,
        [
            "-c",
            """
            SELECT p.id::text, p.name, coalesce(b.name, ''), c.slug, coalesce(c.names->>'en', c.slug),
                   p.pack_size::text, p.unit
            FROM products p
            JOIN categories c ON c.id = p.category_id
            LEFT JOIN brands b ON b.id = p.brand_id
            ORDER BY p.id
            """,
        ],
    )
    out: list[tuple[str, str, str, str, int, int]] = []
    for line in rows.splitlines():
        if not line.strip():
            continue
        pid, name, brand, slug, label, raw_pack, unit = line.split("\t")
        size = float(raw_pack or "1")
        if size == 1 and unit == "pack":
            parsed = parse_size(name)
            if parsed:
                size, unit = parsed
            else:
                size, unit = DEFAULT_PACK.get(slug, (1, "pack"))
        else:
            parsed = parse_size(name)
            if parsed and abs(parsed[0] - size) < 0.001 and parsed[1] == unit:
                pass
        name = clean_name(name, brand, label, size, unit)
        mrp, sale = price_for(slug, size, unit, brand, pid)
        out.append((pid, name, f"{size:.3f}", unit, mrp, sale))

    if dry:
        print(f"would update {len(out)} products")
        for row in out[:10]:
            print(row)
        return 0

    statements: list[str] = []
    for pid, name, pack, unit, mrp, sale in out:
        safe = name.replace("'", "''")
        statements.append(
            f"UPDATE products SET name='{safe}', pack_size={pack}, unit='{unit}', "
            f"mrp_paise={mrp}, updated_at=now() WHERE id='{pid}';"
        )
        statements.append(
            "UPDATE store_items SET "
            f"mrp_paise={mrp}, "
            f"price_paise=LEAST({mrp}, GREATEST(100, {sale} + ((abs(hashtext(id::text)) % 5) - 2) * 100)), "
            f"updated_at=now() WHERE product_id='{pid}';"
        )
    combined = "\n".join(statements) + "\n"
    res = subprocess.run(
        ["psql", url, "-v", "ON_ERROR_STOP=1", "-q"],
        input=combined,
        text=True,
        capture_output=True,
    )
    if res.returncode != 0:
        print(res.stderr[-2000:], file=sys.stderr)
        return 1
    print(f"enriched {len(out)} products and their store items")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
