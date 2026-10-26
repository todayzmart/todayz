# Translation glossary

The product terms as they appear in `messages/**` (hi, kn, te, ta), checked on 26 Oct (W4-05). Use these when you write or review a message, so one idea has one word in every app. All hi, kn, te and ta text is machine-written and approved without a native-speaker review (see `docs/qa/*/REVIEW.md`), so every entry here is open to a native speaker's correction. If a term is changed, change it in every app in the same commit.

## How to choose a term

1. The word people already say in India wins: a transliterated English word (पिकअप, డెలివరీ, ஸ்டாக்) over a formal coinage.
2. If the apps already disagree, the form used in most messages wins, and the minority is fixed.
3. Where there is no clear majority, the term is listed under "Open" below and left alone for a native speaker.
4. Polite register, sentence case, ICU placeholders unchanged. Product, shop and item names in mock screens stay English.

## Terms

| English | Hindi | Kannada | Telugu | Tamil |
|---|---|---|---|---|
| credits | क्रेडिट | ಕ್ರೆಡಿಟ್ | క్రెడిట్ (plural క్రెడిట్‌లు) | கிரெடிட் |
| pickup | पिकअप | ಪಿಕಪ್ | పికప్ | பிக்அப் |
| delivery | डिलीवरी | ಡೆಲಿವರಿ | డెలివరీ | டெலிவரி |
| order code, pickup code | कोड | ಕೋಡ್ | కోడ్ | கோடு (D-246) |
| standee | स्टैंडी | ಸ್ಟ್ಯಾಂಡಿ | స్టాండీ | ஸ்டாண்டி |
| stock | स्टॉक | ಸ್ಟಾಕ್ | స్టాక్ | ஸ்டாக் |
| offer | ऑफ़र | ಆಫರ್ | ఆఫర్ (plural ఆఫర్లు) | ஆஃபர் |
| POS, counter | काउंटर | ಕೌಂಟರ್ | కౌంటర్ | கவுண்டர் |
| bill | बिल | ಬಿಲ್ | బిల్లు | பில் |
| wallet | वॉलेट | ವಾಲೆಟ್ | వాలెట్ | வாலெட் |
| payout | पेआउट | ಪೇಔಟ್ | పేఅవుట్ | பேஅவுட் (D-246) |
| ask (noun) | माँगें | ಕೇಳಿಕೆ | అభ్యర్థనలు | கோரிக்கைகள் |
| ask (verb) | पूछें, कहें | ಕೇಳಿ | అడగండి | கேளுங்கள் |
| coach | कोच | ಕೋಚ್ | కోచ్ | கோச் |
| shop | दुकान | ಅಂಗಡಿ | దుకాణం | கடை |
| order | ऑर्डर | ಆರ್ಡರ್ | ఆర్డర్ (plural ఆర్డర్లు) | ஆர்டர் |
| cart | कार्ट | ಕಾರ್ಟ್ | కార్ట్ | கார்ட் |
| basket | बास्केट | ಬಾಸ್ಕೆಟ್ | బాస్కెట్ | பாஸ்கெட் |
| customer | ग्राहक | ಗ್ರಾಹಕರು | కస్టమర్ (plural కస్టమర్లు) | வாடிக்கையாளர் |
| statement | स्टेटमेंट | ಸ್ಟೇಟ್‌ಮೆಂಟ್ | స్టేట్‌మెంట్ | ஸ்டேட்மென்ட் |
| cashback | कैशबैक | ಕ್ಯಾಶ್ ಬ್ಯಾಕ್ | క్యాష్ బ్యాక్ | கேஷ்பேக் |
| campaign | कैंपेन | ಅಭಿಯಾನ | క్యాంపెయిన్ | பிரச்சாரம் |
| address | पता | ವಿಳಾಸ | చిరునామా | முகவரி |
| account | खाता | ಖಾತೆ | ఖాతా | கணக்கு |
| refund | रिफ़ंड | ರಿಫಂಡ್ (D-246) | రీఫండ్ | ரீஃபண்ட் (D-246) |
| voice | आवाज़, बोलकर | ಧ್ವನಿ, ಮಾತಿನ | వాయిస్ | குரல் |
| cancel | कैंसल (D-246) | ಕ್ಯಾನ್ಸಲ್ (D-246) | క్యాన్సిల్ (D-246) | ரத்து |

Telugu plurals: after a consonant ending in a virama the suffix takes a ZWNJ (U+200C) in క్రెడిట్‌లు (majority form); ఆర్డర్లు, కస్టమర్లు and ఆఫర్లు take none (majority form).

## Cross-app inconsistencies

Fixed on 26 Oct (the majority form was clear):

| Language | Was | Now | Where | Messages |
|---|---|---|---|---|
| Telugu | షాపు, షాప్ (shop) | దుకాణం (and its case forms) | site (53), business (14) | 67 |
| Telugu | బిల్ (bill) | బిల్లు | business `ai.bill.*` (9), site `home.pos.bill` (1) | 10 |
| Telugu | క్రెడిట్లు | క్రెడిట్‌లు | 15 messages across site, business, customer | 15 |
| Telugu | ఆర్డర్‌లు | ఆర్డర్లు | 31 messages | 31 |
| Telugu | కస్టమర్‌లు | కస్టమర్లు | 22 messages | 22 |
| Telugu | ఆఫర్‌లు | ఆఫర్లు | 3 messages | 3 |
| Tamil | பிக்கப் (pickup) | பிக்அப் | business `help.faq.code.q` | 1 |

The per-message list is in `docs/qa/W4-05/REPORT.md`.

Decided by the founder's delegation on 26 Oct (D-246 B6), by rule 1 where it applies. The messages follow these terms since 26 Oct (go-live A8). Not changed on purpose: "Discard" and "Undo" keep रद्द / ರದ್ದು / రద్దు, because they are not "cancel"; the hi plan-pricing page keeps कीमत (the price of TodayZ, not an item's); and "ask" as a verb and "demand" keep माँग. The native-speaker review (C12) should read the changed messages in context:

| English | Hindi | Kannada | Telugu | Tamil |
|---|---|---|---|---|
| cancel (both apps) | कैंसल | ಕ್ಯಾನ್ಸಲ್ | క్యాన్సిల్ | ரத்து |
| sign in | साइन इन | ಸೈನ್ ಇನ್ | సైన్ ఇన్ | சைன் இன் |
| order or pickup code | कोड | ಕೋಡ್ | కోడ్ | கோடு |
| refund | रिफ़ंड | ರಿಫಂಡ್ | రీఫండ్ | ரீஃபண்ட் |
| payout | — | — | — | பேஅவுட் |
| price | दाम | — | — | — |
| ask (noun) | पूछताछ | — | — | — |

Open (for the native speaker):

- accept (an order) in hi: flagged during the survey, not re-counted here.

The survey that led to the decisions above (26 Oct), for reference:

Counts are messages that carry the English term.

- cancel: the customer app and the business app use different words in hi (कैंसल 13 of 14 in customer, रद्द 14 of 15 in business), kn (ಕ್ಯಾನ್ಸಲ್ in customer, ರದ್ದುಮಾಡಿ in business) and te (క్యాన్సిల్ in customer, రద్దు in business). Tamil uses ரத்து in both. Pick one word per language for both apps. ರದ್ದುಮಾಡಿ is also the basket "undo", so it can be misread as cancelling.
- price in hi: दाम (customer 10, business 18) and क़ीमत (business 22, site 3) both appear; there is no clear winner.
- sign in in ta: customer uses both சைன் இன் (10) and உள்நுழை (5); business uses உள்நுழை only. Hi, kn and te use साइन इन, ಸೈನ್ ಇನ್, సైన్ ఇన్ everywhere.
- order or pickup code in ta: கோடு in customer, குறியீடு in business help.
- refund in kn and ta: on the site both the loanword (ರಿಫಂಡ್, ரீஃபண்ட்) and a native word (ಮರುಪಾವತಿ, பணத்திருப்பம்) appear in the same two messages. In hi and te only the loanword is used.
- payout in ta: பணம் வழங்கல் is a description, not a word shops are likely to say.
- ask (noun) in hi: माँगें can read as "demands"; a native speaker should confirm.
