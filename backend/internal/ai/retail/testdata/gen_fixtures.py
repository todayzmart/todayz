#!/usr/bin/env python3
"""Generates W3-11's recorded eval fixtures offline (no network, fixed seed).

    python3 backend/internal/ai/retail/testdata/gen_fixtures.py

Writes, under backend/internal/ai/:
  billscan/billeval/fixtures/      60 supplier bills (36 printed, 24 handwritten; en, hi, kn)
  shelfscan/shelfeval/fixtures/    40 shelf photos
  imagecheck/imageeval/fixtures/   banner images for the safety check

Each case holds the image path, the truth (expect) and a recorded reply. The replies are
SYNTHETIC: written here from the truth with seeded reading errors, not by a model. The
recorded eval therefore proves the parsing, conversion and scoring pipeline and its bars; a
live run (make ai-eval, the user's own key) measures a real model on the same images.
"""
import json
import random
import subprocess
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = Path(__file__).resolve().parents[2]  # backend/internal/ai
RNG = random.Random(31126)

FONTS = {
    "print_en": "/System/Library/Fonts/Supplemental/Arial.ttf",
    "print_hi": "/System/Library/Fonts/Supplemental/Devanagari Sangam MN.ttc",
    "print_kn": "/System/Library/Fonts/Supplemental/Kannada Sangam MN.ttc",
    "hand_en": "/System/Library/Fonts/Supplemental/Bradley Hand Bold.ttf",
    "hand_hi": "/System/Library/Fonts/Supplemental/ITFDevanagari.ttc",
    "hand_kn": "/System/Library/Fonts/Supplemental/Kannada MN.ttc",
    "bold": "/System/Library/Fonts/Supplemental/Arial Bold.ttf",
}


def font(key, size):
    try:
        return ImageFont.truetype(FONTS[key], size)
    except OSError:
        return ImageFont.load_default()


# PIL here has no raqm, so it cannot shape Indic scripts: text that is not ASCII is rendered by
# pango-view (HarfBuzz) and pasted in.
PANGO = {
    "print_hi": "Arial, Devanagari Sangam MN", "print_kn": "Arial, Kannada Sangam MN",
    "hand_hi": "Bradley Hand, ITF Devanagari", "hand_kn": "Bradley Hand, Kannada MN",
    "bold": "Arial Bold, Devanagari Sangam MN Bold, Kannada Sangam MN Bold",
    "print_en": "Arial", "hand_en": "Bradley Hand",
}
TMP = tempfile.mkdtemp(prefix="tz-fixtures-")


def put(img, xy, s, key, size, fill):
    if s.isascii():
        ImageDraw.Draw(img).text(xy, s, font=font(key, size), fill=fill)
        return
    out = Path(TMP) / "t.png"
    subprocess.run(["pango-view", "--text=" + s, "--font=%s %d" % (PANGO[key], size), "--background=transparent",
                    "--foreground=#%02x%02x%02x" % fill, "--margin=0", "--dpi=72", "-q", "-o", str(out)], check=True)
    t = Image.open(out).convert("RGBA")
    img.paste(Image.new("RGB", t.size, fill), (int(xy[0]), int(xy[1])), t.split()[3])


# name per language, MRP in paise
PRODUCTS = [
    ({"en": "Amul Butter 100g", "hi": "अमूल बटर 100g", "kn": "ಅಮುಲ್ ಬೆಣ್ಣೆ 100g"}, 6200),
    ({"en": "Maggi Noodles 70g", "hi": "मैगी नूडल्स 70g", "kn": "ಮ್ಯಾಗಿ ನೂಡಲ್ಸ್ 70g"}, 1400),
    ({"en": "Tata Salt 1kg", "hi": "टाटा नमक 1kg", "kn": "ಟಾಟಾ ಉಪ್ಪು 1kg"}, 2800),
    ({"en": "Aashirvaad Atta 5kg", "hi": "आशीर्वाद आटा 5kg", "kn": "ಆಶೀರ್ವಾದ್ ಹಿಟ್ಟು 5kg"}, 29500),
    ({"en": "Parle-G 250g", "hi": "पारले-जी 250g", "kn": "ಪಾರ್ಲೆ-ಜಿ 250g"}, 3000),
    ({"en": "Colgate Strong Teeth 200g", "hi": "कोलगेट 200g", "kn": "ಕೋಲ್ಗೇಟ್ 200g"}, 12000),
    ({"en": "Surf Excel 1kg", "hi": "सर्फ एक्सेल 1kg", "kn": "ಸರ್ಫ್ ಎಕ್ಸೆಲ್ 1kg"}, 14500),
    ({"en": "Fortune Sunflower Oil 1L", "hi": "फॉर्च्यून तेल 1L", "kn": "ಫಾರ್ಚೂನ್ ಎಣ್ಣೆ 1L"}, 18500),
    ({"en": "Brooke Bond Red Label 250g", "hi": "रेड लेबल चाय 250g", "kn": "ರೆಡ್ ಲೇಬಲ್ ಟೀ 250g"}, 15500),
    ({"en": "Amul Taaza 500ml", "hi": "अमूल ताज़ा 500ml", "kn": "ಅಮುಲ್ ತಾಜಾ 500ml"}, 2900),
    ({"en": "Britannia Good Day 100g", "hi": "गुड डे 100g", "kn": "ಗುಡ್ ಡೇ 100g"}, 3000),
    ({"en": "Dettol Soap 125g", "hi": "डेटॉल साबुन 125g", "kn": "ಡೆಟಾಲ್ ಸೋಪು 125g"}, 5500),
    ({"en": "Lux Soap 100g", "hi": "लक्स साबुन 100g", "kn": "ಲಕ್ಸ್ ಸೋಪು 100g"}, 3800),
    ({"en": "MDH Chana Masala 100g", "hi": "एमडीएच चना मसाला 100g", "kn": "ಎಂಡಿಎಚ್ ಚನಾ ಮಸಾಲ 100g"}, 8200),
    ({"en": "Haldiram Bhujia 200g", "hi": "हल्दीराम भुजिया 200g", "kn": "ಹಲ್ದಿರಾಮ್ ಭುಜಿಯಾ 200g"}, 5500),
    ({"en": "Nescafe Classic 50g", "hi": "नेस्कैफे 50g", "kn": "ನೆಸ್ಕೆಫೆ 50g"}, 19000),
    ({"en": "Bournvita 500g", "hi": "बॉर्नविटा 500g", "kn": "ಬೋರ್ನ್‌ವಿಟಾ 500g"}, 24900),
    ({"en": "Clinic Plus Shampoo 175ml", "hi": "क्लिनिक प्लस 175ml", "kn": "ಕ್ಲಿನಿಕ್ ಪ್ಲಸ್ 175ml"}, 13500),
    ({"en": "Vim Bar 200g", "hi": "विम बार 200g", "kn": "ವಿಮ್ ಬಾರ್ 200g"}, 2000),
    ({"en": "Good Knight Refill", "hi": "गुड नाइट रिफिल", "kn": "ಗುಡ್ ನೈಟ್ ರಿಫಿಲ್"}, 8500),
    ({"en": "Kurkure Masala 90g", "hi": "कुरकुरे 90g", "kn": "ಕುರ್ಕುರೆ 90g"}, 2000),
    ({"en": "Real Mango Juice 1L", "hi": "रियल आम जूस 1L", "kn": "ರಿಯಲ್ ಮಾವು ಜ್ಯೂಸ್ 1L"}, 12000),
    ({"en": "Saffola Gold Oil 1L", "hi": "सफोला गोल्ड 1L", "kn": "ಸಫೋಲಾ ಗೋಲ್ಡ್ 1L"}, 21000),
    ({"en": "Horlicks 500g", "hi": "हॉर्लिक्स 500g", "kn": "ಹಾರ್ಲಿಕ್ಸ್ 500g"}, 26000),
]

SUPPLIERS = {
    "en": ["Sri Lakshmi Agencies", "Ganesh Distributors", "Balaji Traders", "Om Sai Enterprises"],
    "hi": ["श्री गणेश एजेंसी", "बालाजी ट्रेडर्स", "राम वितरक", "शिव एंटरप्राइज़"],
    "kn": ["ಶ್ರೀ ಲಕ್ಷ್ಮಿ ಏಜೆನ್ಸೀಸ್", "ಗಣೇಶ್ ಡಿಸ್ಟ್ರಿಬ್ಯೂಟರ್ಸ್", "ಬಾಲಾಜಿ ಟ್ರೇಡರ್ಸ್", "ಓಂ ಸಾಯಿ ಎಂಟರ್‌ಪ್ರೈಸಸ್"],
}


def rupees(p):
    return p / 100


def reply(msg_id, tool, inp, model="claude-sonnet-5-5", tokens=(1500, 400)):
    return {
        "id": "msg_" + msg_id, "type": "message", "role": "assistant", "model": model,
        "content": [{"type": "tool_use", "id": "toolu_" + msg_id, "name": tool, "input": inp}],
        "stop_reason": "tool_use", "stop_sequence": None,
        "usage": {"input_tokens": tokens[0], "output_tokens": tokens[1]},
    }


def write_case(folder, case):
    folder.mkdir(parents=True, exist_ok=True)
    with open(folder / (case["name"] + ".json"), "w", encoding="utf-8") as f:
        json.dump(case, f, ensure_ascii=False, indent=2)
        f.write("\n")


def save_jpeg(img, path):
    path.parent.mkdir(parents=True, exist_ok=True)
    img.save(path, "JPEG", quality=55, optimize=True)


# ---------------------------------------------------------------- supplier bills

def bill_lines(n):
    picks = RNG.sample(PRODUCTS, n)
    lines = []
    for names, mrp in picks:
        qty = RNG.choice([2, 3, 4, 5, 6, 10, 12, 24])
        cost = int(round(mrp * RNG.uniform(0.78, 0.92)))
        cost -= cost % 5  # bills round rates to 5 paise
        exp = None
        if RNG.random() < 0.45:
            exp = "2027-%02d-%02d" % (RNG.randint(1, 12), RNG.choice([15, 28]))
        lines.append({"names": names, "quantity": qty, "costPaise": cost, "mrpPaise": mrp, "expiresOn": exp})
    return lines


def draw_bill(kind, lang, supplier, date, lines, path):
    w, lh = 640, 30
    h = 190 + lh * len(lines) + 70
    paper = (252, 251, 246) if kind == "printed" else (250, 247, 232)
    img = Image.new("RGB", (w, h), paper)
    d = ImageDraw.Draw(img)
    fkey = ("print_" if kind == "printed" else "hand_") + lang
    ink = (20, 20, 20) if kind == "printed" else (25, 45, 140)
    put(img, (24, 18), supplier, "bold", 22, ink)
    d.text((24, 50), "GSTIN 29ABCDE1234F1Z5   Ph 98450 12345", font=font("print_en", 13), fill=ink)
    d.text((24, 74), "Bill No %d   Date %s" % (RNG.randint(1000, 9999), date), font=font("print_en", 15), fill=ink)
    y = 110
    cols = [24, 330, 400, 480, 560]
    if kind == "printed":
        d.line((20, y - 6, w - 20, y - 6), fill=ink)
        for x, t in zip(cols, ["Item", "Qty", "Rate", "MRP", "Exp"]):
            d.text((x, y), t, font=font("bold", 15), fill=ink)
        d.line((20, y + 22, w - 20, y + 22), fill=ink)
    else:
        for yy in range(y + 26, h - 40, lh):
            d.line((14, yy, w - 14, yy), fill=(170, 190, 220))
    y += 34
    for l in lines:
        jx = 0 if kind == "printed" else RNG.randint(-4, 4)
        jy = 0 if kind == "printed" else RNG.randint(-3, 3)
        exp = ""
        if l["expiresOn"]:
            yyyy, mm, _ = l["expiresOn"].split("-")
            exp = "%s/%s" % (mm, yyyy[2:]) if kind == "handwritten" else l["expiresOn"][2:]
        vals = [l["names"][lang], str(l["quantity"]), "%.2f" % rupees(l["costPaise"]), "%.2f" % rupees(l["mrpPaise"]), exp]
        for i, (x, t) in enumerate(zip(cols, vals)):
            if i == 0:
                put(img, (x + jx, y + jy), t, fkey, 17 if kind == "printed" else 19, ink)
            else:
                d.text((x + jx, y + jy), t, font=font(("print_en" if kind == "printed" else "hand_en"), 16), fill=ink)
        y += lh
    total = sum(l["quantity"] * l["costPaise"] for l in lines)
    d.text((cols[2], y + 12), "Total  %.2f" % rupees(total), font=font("bold", 16), fill=ink)
    if kind == "handwritten":
        img = img.rotate(RNG.uniform(-2.5, 2.5), resample=Image.BICUBIC, expand=False, fillcolor=paper)
        img = img.filter(ImageFilter.GaussianBlur(0.6))
    save_jpeg(img, path)


def misread(l, lang, kind):
    """One seeded reading error on a reply line (the line no longer matches the truth)."""
    r = dict(l)
    what = RNG.choice(["qty", "cost", "mrp", "name"] if kind == "printed" else ["qty", "cost", "cost", "mrp", "name", "expiry"])
    if what == "qty":
        r["quantity"] = l["quantity"] + RNG.choice([1, -1]) if l["quantity"] > 1 else 7
    elif what == "cost":
        r["costPaise"] = l["costPaise"] + RNG.choice([100, 1000, -50])
    elif what == "mrp":
        r["mrpPaise"] = l["mrpPaise"] + 1000
    elif what == "expiry":
        r["expiresOn"] = "2028-01-31" if l["expiresOn"] is None else None
    else:
        r["badName"] = True
    return r


def gen_bills():
    out = ROOT / "billscan" / "billeval" / "fixtures"
    plan = [("printed", lang, 12) for lang in ("en", "hi", "kn")] + [("handwritten", lang, 8) for lang in ("en", "hi", "kn")]
    stats = {"printed": [0, 0], "handwritten": [0, 0]}
    for kind, lang, count in plan:
        rate = 0.04 if kind == "printed" else 0.15
        for i in range(1, count + 1):
            name = "%s_%s_%02d" % (kind, lang, i)
            lines = bill_lines(RNG.randint(4, 9))
            supplier = RNG.choice(SUPPLIERS[lang])
            date = "2026-10-%02d" % RNG.randint(1, 24)
            img_rel = "images/%s.jpg" % name
            draw_bill(kind, lang, supplier, date, lines, out / img_rel)
            read = []
            for l in lines:
                stats[kind][1] += 1
                if RNG.random() < rate:
                    if RNG.random() < 0.15:
                        continue  # a line the reader missed
                    read.append(misread(l, lang, kind))
                else:
                    stats[kind][0] += 1
                    read.append(dict(l))
            tool_lines = []
            for r in read:
                nm = r["names"][lang]
                if r.get("badName"):
                    nm = nm.split(" ")[0] + " " + RNG.choice(["Gold", "Lite", "Mini", "XL"])
                tool_lines.append({
                    "name": nm, "quantity": r["quantity"], "cost_rupees": rupees(r["costPaise"]),
                    "mrp_rupees": rupees(r["mrpPaise"]), "expires_on": r["expiresOn"],
                    "confidence": round(RNG.uniform(0.82, 0.99) if kind == "printed" else RNG.uniform(0.55, 0.9), 2),
                })
            case = {
                "name": name,
                "input": {"image": img_rel, "kind": kind, "lang": lang},
                "expect": {"lines": [{"name": l["names"][lang], "quantity": l["quantity"], "costPaise": l["costPaise"],
                                      "mrpPaise": l["mrpPaise"], "expiresOn": l["expiresOn"]} for l in lines]},
                "reply": reply("bill_" + name, "read_supplier_bill",
                               {"supplier": supplier, "bill_date": date, "lines": tool_lines}),
            }
            write_case(out, case)
    return stats


# ---------------------------------------------------------------- shelf photos

def gen_shelves():
    out = ROOT / "shelfscan" / "shelfeval" / "fixtures"
    tints = [(232, 76, 61), (241, 196, 15), (46, 134, 193), (39, 174, 96), (155, 89, 182), (230, 126, 34), (236, 240, 241)]
    found, total = 0, 0
    for i in range(1, 41):
        name = "shelf_%02d" % i
        picks = RNG.sample(PRODUCTS, RNG.randint(4, 8))
        img = Image.new("RGB", (640, 420), (120, 86, 60))
        d = ImageDraw.Draw(img)
        for row in range(2):
            d.rectangle((0, 190 + row * 200, 640, 206 + row * 200), fill=(80, 55, 35))
        x, row = 14, 0
        for names, _ in picks:
            bw = RNG.randint(110, 150)
            if x + bw > 630:
                x, row = 14, row + 1
            top = 30 + row * 200
            c = RNG.choice(tints)
            d.rectangle((x, top, x + bw, top + 158), fill=c, outline=(30, 30, 30))
            brand, rest = names["en"].split(" ", 1) if " " in names["en"] else (names["en"], "")
            d.text((x + 8, top + 12), brand, font=font("bold", 18), fill=(20, 20, 20))
            d.text((x + 8, top + 44), rest, font=font("print_en", 13), fill=(20, 20, 20))
            x += bw + 10
        img = img.filter(ImageFilter.GaussianBlur(0.5))
        img_rel = "images/%s.jpg" % name
        save_jpeg(img, out / img_rel)
        products = []
        for names, _ in picks:
            total += 1
            if RNG.random() < 0.1:
                continue  # missed in the reply
            found += 1
            en = names["en"]
            parts = en.split(" ")
            size = parts[-1] if any(ch.isdigit() for ch in parts[-1]) else None
            products.append({"name": " ".join(parts[1:-1] if size else parts[1:]) or parts[0], "brand": parts[0],
                             "size": size, "confidence": round(RNG.uniform(0.6, 0.97), 2)})
        case = {
            "name": name,
            "input": {"image": img_rel},
            "expect": {"products": [n["en"] for n, _ in picks]},
            "reply": reply("shelf_" + name, "record_shelf_products", {"products": products}),
        }
        write_case(out, case)
    return found, total


# ---------------------------------------------------------------- banner images

def gen_banners():
    out = ROOT / "imagecheck" / "imageeval" / "fixtures"
    cases = [
        ("ok_diwali_offer", "Diwali offer: 10% off sweets", OK := "ok", []),
        ("ok_new_stock", "Fresh vegetables every morning", "ok", []),
        ("ok_brand_products", "Amul and Maggi now in stock", "ok", []),
        ("reject_phone", "Call 98450 12345 for orders", "reject", ["personal_data"]),
        ("reject_misleading", "FREE gold coin with every purchase", "reject", ["misleading"]),
        ("reject_competitor", "Better than BigBasket scammers", "reject", ["competitor_brand"]),
        ("unsure_tiny_text", "terms and conditions apply to all offers on selected items only", "unsure", ["illegible_text"]),
        ("unsure_not_banner", "", "unsure", ["not_a_banner"]),
    ]
    for name, text, verdict, reasons in cases:
        img = Image.new("RGB", (640, 360), (226, 247, 238))
        d = ImageDraw.Draw(img)
        if name == "unsure_not_banner":
            for _ in range(40):
                x, y = RNG.randint(0, 600), RNG.randint(0, 330)
                d.ellipse((x, y, x + 40, y + 30), fill=(RNG.randint(0, 255), RNG.randint(0, 255), RNG.randint(0, 255)))
        elif name == "unsure_tiny_text":
            d.text((20, 330), text, font=font("print_en", 8), fill=(60, 60, 60))
            img = img.filter(ImageFilter.GaussianBlur(1.2))
        else:
            d.rectangle((24, 24, 616, 336), outline=(28, 120, 90), width=4)
            d.text((48, 150), text, font=font("bold", 30), fill=(20, 60, 50))
        img_rel = "images/%s.jpg" % name
        save_jpeg(img, out / img_rel)
        write_case(out, {
            "name": name,
            "input": {"image": img_rel},
            "expect": {"verdict": verdict, "reasons": reasons},
            "reply": reply("img_" + name, "check_banner_image",
                           {"verdict": verdict, "reasons": reasons, "confidence": 0.9}, model="claude-haiku-5-5", tokens=(900, 40)),
        })
    # a reply that breaks the rule "reject needs a reason": schema, so it holds for review
    write_case(out, {
        "name": "schema_reject_without_reason",
        "input": {"image": "images/reject_phone.jpg"},
        "expect": {"verdict": "unsure", "reasons": [], "reject": "schema"},
        "reply": reply("img_schema", "check_banner_image", {"verdict": "reject", "reasons": [], "confidence": 0.7},
                       model="claude-haiku-5-5", tokens=(900, 30)),
    })


if __name__ == "__main__":
    s = gen_bills()
    f, t = gen_shelves()
    gen_banners()
    print(json.dumps({
        "bills_printed_right": s["printed"], "bills_handwritten_right": s["handwritten"],
        "shelf_found": [f, t],
    }))
