#!/usr/bin/env python3
"""Writes W3-11's recorded text eval fixtures (insight_text, review_reply, banner_copy).

    python3 backend/internal/ai/retail/testdata/gen_text_fixtures.py

The replies are written by hand here, not by a model: each grounded case is a sentence a good
reply would give, and each reject case carries the fault the feature must catch (a number not in
the facts, arithmetic, a phone number, a schema break, no tool call, a refusal). A live run
(make ai-eval) re-records them against the model.
"""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def tool_reply(name, tool, inp):
    return {"id": "msg_" + name, "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
            "content": [{"type": "tool_use", "id": "toolu_" + name, "name": tool, "input": inp}],
            "stop_reason": "tool_use", "stop_sequence": None, "usage": {"input_tokens": 700, "output_tokens": 120}}


def text_reply(name, text):
    return {"id": "msg_" + name, "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
            "content": [{"type": "text", "text": text}],
            "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 700, "output_tokens": 30}}


def write(folder, name, inp, expect, reply):
    folder.mkdir(parents=True, exist_ok=True)
    with open(folder / (name + ".json"), "w", encoding="utf-8") as f:
        json.dump({"name": name, "input": inp, "expect": expect, "reply": reply}, f, ensure_ascii=False, indent=2)
        f.write("\n")


# ------------------------------------------------------------------ insight_text (AI-R4)

def row(ref, kind, facts):
    return {"ref": ref, "kind": kind, "facts": facts}


INSIGHT = [
    # name, lang, rows, reply texts {ref: text}, expected kept refs, dropped
    ("en_selling_fast", "en",
     [row("r1", "selling_fast", {"itemName": "Amul Butter 100g", "daysOfCover": 2, "soldLast7Days": 18, "action": {"quantity": 24}})],
     {"r1": "Amul Butter 100g sold 18 in the last week and lasts about 2 more days; restock 24."}, ["r1"], 0),
    ("hi_going_to_waste", "hi",
     [row("r1", "going_to_waste", {"itemName": "अमूल ताज़ा 500ml", "units": 12, "valueAtCost": "182.40", "daysToExpiry": 3})],
     {"r1": "अमूल ताज़ा 500ml के 12 पैकेट 3 दिन में एक्सपायर होंगे, लागत ₹182.40; फ़्लैश ऑफ़र से इन्हें निकालें।"}, ["r1"], 0),
    ("hi_native_digits", "hi",
     [row("r1", "stock_check_due", {"items": 14, "daysSinceCount": 30})],
     {"r1": "१४ सामानों की गिनती ३० दिन से नहीं हुई है; आज इन्हें गिन लें।"}, ["r1"], 0),
    ("kn_sitting_on_shelf", "kn",
     [row("r1", "sitting_on_shelf", {"itemName": "ಡೆಟಾಲ್ ಸೋಪು 125g", "daysWithoutSale": 21, "units": 9})],
     {"r1": "ಡೆಟಾಲ್ ಸೋಪು 125g 21 ದಿನಗಳಿಂದ ಮಾರಾಟವಾಗಿಲ್ಲ, 9 ಇವೆ; ಆಫರ್ ಅಥವಾ ಉತ್ತಮ ಜಾಗ ಪ್ರಯತ್ನಿಸಿ."}, ["r1"], 0),
    ("te_missed_sales", "te",
     [row("r1", "missed_sales", {"itemName": "Maggi Noodles 70g", "searchDaysOutOfStock": 4})],
     {"r1": "Maggi Noodles 70g స్టాక్ లేని 4 రోజుల్లో దీని కోసం వెతికారు; మళ్లీ స్టాక్ చేయండి."}, ["r1"], 0),
    ("ta_wanted_nearby", "ta",
     [row("r1", "wanted_nearby", {"query": "oats", "searches": 15})],
     {"r1": "அருகில் oats 15 முறை தேடப்பட்டது; இதை உங்கள் கடையில் சேர்க்கவும்."}, ["r1"], 0),
    ("en_two_rows", "en",
     [row("r1", "expired_on_shelf", {"itemName": "Real Mango Juice 1L", "units": 3}),
      row("r2", "bought_together", {"itemName": "Bread", "withItemName": "Amul Butter 100g", "bills": 26})],
     {"r1": "3 packs of Real Mango Juice 1L are past expiry; take them off the shelf.",
      "r2": "Bread and Amul Butter 100g were on 26 bills together; try a pair offer."}, ["r1", "r2"], 0),
    ("grounding_invented_pct", "en",
     [row("r1", "sitting_on_shelf", {"itemName": "Lux Soap 100g", "daysWithoutSale": 21, "units": 6}),
      row("r2", "selling_fast", {"itemName": "Tata Salt 1kg", "daysOfCover": 3, "action": {"quantity": 10}})],
     {"r1": "Lux Soap 100g has not sold in 21 days; a 15% offer would clear the 6 left.",
      "r2": "Tata Salt 1kg lasts 3 more days; restock 10."}, ["r2"], 1),
    ("grounding_arithmetic", "en",
     [row("r1", "going_to_waste", {"itemName": "Amul Taaza 500ml", "units": 12, "price": "29"})],
     {"r1": "12 packs of Amul Taaza 500ml worth 348 expire soon; run a flash offer."}, [], 1),
    ("grounding_phone", "en",
     [row("r1", "regulars_slipping", {"customers": 11, "days": 21})],
     {"r1": "11 regulars have not come in 21 days; call 98450 12345 to remind them."}, [], 1),
    ("unknown_ref_ignored", "en",
     [row("r1", "stock_check_due", {"items": 5, "daysSinceCount": 45})],
     {"r9": "Count 5 items.", "r1": "5 items were last counted 45 days ago; count them today."}, ["r1"], 0),
]


def gen_insight():
    out = ROOT / "insighttext" / "insighteval" / "fixtures"
    for name, lang, rows, texts, kept, dropped in INSIGHT:
        write(out, name, {"lang": lang, "rows": rows},
              {"texts": {r: texts[r] for r in kept}, "dropped": dropped},
              tool_reply("it_" + name, "write_insight_texts", {"texts": [{"ref": r, "text": t} for r, t in texts.items()]}))
    rows = [row("r1", "stock_check_due", {"items": 5, "daysSinceCount": 45})]
    write(out, "schema_text_too_long", {"lang": "en", "rows": rows}, {"texts": {}, "dropped": 0, "reject": "schema"},
          tool_reply("it_long", "write_insight_texts", {"texts": [{"ref": "r1", "text": "Count them. " * 30}]}))
    write(out, "no_tool_call", {"lang": "en", "rows": rows}, {"texts": {}, "dropped": 0, "reject": "invalid"},
          text_reply("it_notool", "5 items need counting."))


# ------------------------------------------------------------------ review_reply (AI-R5)

def review(stars, tags, text, lang, shop="Sri Lakshmi Stores"):
    return {"stars": stars, "tags": tags, "text": text, "language": lang, "shop": shop}


REPLIES = [
    ("en_five_star", review(5, ["fresh", "quick_pickup"], "Fresh vegetables and my order was ready in 10 minutes.", "en"),
     "Thank you for the kind words. We are glad your order was ready in 10 minutes and hope to see you again soon.", ""),
    ("hi_late_order", review(2, ["late"], "ऑर्डर 40 मिनट देर से तैयार हुआ।", "hi"),
     "आपको 40 मिनट इंतज़ार करना पड़ा, इसके लिए माफ़ी चाहते हैं। अगली बार हम आपका ऑर्डर समय पर तैयार रखेंगे।", ""),
    ("kn_no_text", review(4, ["friendly"], None, "kn"),
     "ನಿಮ್ಮ ರೇಟಿಂಗ್‌ಗೆ ಧನ್ಯವಾದಗಳು. ಮತ್ತೆ ನಮ್ಮ ಅಂಗಡಿಗೆ ಬನ್ನಿ.", ""),
    ("te_wrong_item", review(1, ["wrong_item"], "Got the wrong brand of oil.", "te"),
     "తప్పు బ్రాండ్ నూనె వచ్చినందుకు క్షమించండి. దయచేసి అంగడికి రండి, మేము మార్చి ఇస్తాం.", ""),
    ("ta_good_price", review(5, ["good_price"], None, "ta"),
     "உங்கள் அன்பான மதிப்பீட்டிற்கு நன்றி. மீண்டும் வாருங்கள்.", ""),
    ("grounding_invented_discount", review(3, ["expensive"], "Prices are a bit high.", "en"),
     "Sorry about the prices. Show this reply for 10% off your next visit.", "grounding"),
    ("grounding_phone", review(2, ["late"], "Late again.", "en"),
     "Sorry for the delay. Please call us on 98450 12345 so we can make it right.", "grounding"),
]


def gen_replies():
    out = ROOT / "replydraft" / "replyeval" / "fixtures"
    for name, rv, text, reject in REPLIES:
        write(out, name, rv, {"text": "" if reject else text, "reject": reject},
              tool_reply("rr_" + name, "draft_review_reply", {"text": text}))
    rv = review(5, [], None, "en")
    write(out, "schema_empty_text", rv, {"text": "", "reject": "schema"},
          tool_reply("rr_empty", "draft_review_reply", {"text": "  "}))
    write(out, "refused", rv, {"text": "", "reject": "refused"},
          {**text_reply("rr_refused", ""), "stop_reason": "refusal", "content": []})


# ------------------------------------------------------------------ banner_copy (AI-R9)

def opt(h, s, he, se, tint, item=None):
    return {"headline": h, "subline": s, "headline_en": he, "subline_en": se, "tint": tint, "item": item}


BANNERS = [
    ("en_percent_offer",
     {"shop": "Sri Lakshmi Stores", "language": "en",
      "offer": {"kind": "percent", "percentOff": 10, "wholeShop": True, "endsOn": "31 Oct"},
      "items": [{"n": 1, "name": "Haldiram Bhujia 200g", "priceRupees": "55"}]},
     [opt("10% off everything", "Till 31 Oct", "10% off everything", "Till 31 Oct", "amber", 1),
      opt("Diwali savings are here", "10% off the whole shop", "Diwali savings are here", "10% off the whole shop", "rose"),
      opt("Stock up for the festival", "Offer ends 31 Oct", "Stock up for the festival", "Offer ends 31 Oct", "green")],
     3, 0),
    ("hi_flat_offer",
     {"shop": "बालाजी किराना", "language": "hi",
      "offer": {"kind": "flat", "rupeesOff": "50", "minBillRupees": "500", "wholeShop": True},
      "items": []},
     [opt("₹500 की ख़रीद पर ₹50 की छूट", "पूरी दुकान पर", "₹50 off on ₹500", "On the whole shop", "green"),
      opt("बचत का मौका", "₹500 से ऊपर ₹50 कम", "A chance to save", "₹50 less above ₹500", "sky"),
      opt("आज ही ख़रीदें", "₹50 की छूट", "Shop today", "₹50 off", "peach")],
     3, 0),
    ("kn_items",
     {"shop": "ಗಣೇಶ್ ಸ್ಟೋರ್ಸ್", "language": "kn",
      "items": [{"n": 1, "name": "Amul Butter 100g", "priceRupees": "58", "mrpRupees": "62"},
                {"n": 2, "name": "Maggi Noodles 70g", "priceRupees": "14"}]},
     [opt("Amul Butter 100g ಈಗ ₹58", "MRP ₹62", "Amul Butter 100g now ₹58", "MRP ₹62", "amber", 1),
      opt("ತಾಜಾ ಸ್ಟಾಕ್ ಬಂದಿದೆ", "Maggi Noodles 70g ₹14", "Fresh stock is in", "Maggi Noodles 70g ₹14", "lilac", 2),
      opt("ನಿಮ್ಮ ಹತ್ತಿರದ ಅಂಗಡಿ", "ಬುಕ್ ಮಾಡಿ, ಬಂದು ತೆಗೆದುಕೊಳ್ಳಿ", "Your shop nearby", "Book and pick up", "green")],
     3, 0),
    ("te_bxgy",
     {"shop": "Sai Ram Stores", "language": "te",
      "offer": {"kind": "bxgy", "buy": 2, "get": 1, "wholeShop": False},
      "items": [{"n": 1, "name": "Lux Soap 100g", "priceRupees": "38"}]},
     [opt("2 కొంటే 1 ఉచితం", "Lux Soap 100g", "Buy 2 get 1 free", "Lux Soap 100g", "rose", 1),
      opt("సబ్బులపై ఆఫర్", "2 కొనండి, 1 ఉచితం", "Soap offer", "Buy 2, get 1 free", "sky", 1),
      opt("ఈ వారం ప్రత్యేకం", "2 + 1 ఉచితం", "This week's special", "2 + 1 free", "peach")],
     3, 0),
    ("ta_one_dropped",
     {"shop": "Murugan Stores", "language": "ta",
      "offer": {"kind": "percent", "percentOff": 5, "wholeShop": True},
      "items": []},
     [opt("எல்லாவற்றிலும் 5% தள்ளுபடி", "முழு கடைக்கும்", "5% off everything", "On the whole shop", "green"),
      opt("50% வரை தள்ளுபடி", "இன்று மட்டும்", "Up to 50% off", "Today only", "rose"),
      opt("சேமிக்கும் நேரம்", "5% குறைவு", "Time to save", "5% less", "amber")],
     2, 1),
    ("grounding_all_invented",
     {"shop": "Om Sai Kirana", "language": "en", "items": [{"n": 1, "name": "Tata Salt 1kg", "priceRupees": "28"}]},
     [opt("Tata Salt 1kg at ₹25", "Save ₹3", "Tata Salt 1kg at ₹25", "Save ₹3", "amber", 1),
      opt("Call 98450 12345", "Home delivery", "Call 98450 12345", "Home delivery", "sky")],
     0, 2),
]


def gen_banners():
    out = ROOT / "bannercopy" / "bannereval" / "fixtures"
    for name, facts, options, kept, dropped in BANNERS:
        write(out, name, facts, {"kept": kept, "dropped": dropped, "reject": "grounding" if kept == 0 else ""},
              tool_reply("bc_" + name, "draft_banner_copy", {"options": options}))
    facts = BANNERS[0][1]
    write(out, "schema_bad_tint", facts, {"kept": 0, "dropped": 0, "reject": "schema"},
          tool_reply("bc_tint", "draft_banner_copy", {"options": [opt("Big sale", "", "Big sale", "", "neon")]}))


if __name__ == "__main__":
    gen_insight()
    gen_replies()
    gen_banners()
