# Localization

TodayZ ships in English (default), Hindi, Kannada, Telugu and Tamil. Interface text is translated; product names stay as written in the catalogue.

- Ask for the language on the first screen of onboarding with LanguagePicker, each option written in its own script: English, हिन्दी, ಕನ್ನಡ, తెలుగు, தமிழ். Let people change it any time in Account or Settings.
- Set `lang` on the root element (`en`, `hi`, `kn`, `te`, `ta`). The bundle raises line height to 1.6 for the four Indic scripts, because Kannada and Telugu vowel signs stack above and below the line.
- Indic strings run 20–40% longer than English. Buttons and tabs grow with their label; never fix a width on a text container. Two-line labels are acceptable on the tab bar for Tamil.
- Never set Indic text in uppercase or with letter spacing; `overline` styling applies to Latin only.
- Numbers stay in Western digits in every language (₹289, 0.6 km, 4827), as Indian apps and receipts do.
- Dates and times use the locale's month names with 12-hour time: "22 ಅಕ್ಟೋಬರ್, 6:40 pm".
- Keep every string in translation files from day one, including toasts, errors and notification templates, with English as the fallback.
- Voice updates in the business app accept Hindi, Kannada and Hinglish speech regardless of the interface language.
