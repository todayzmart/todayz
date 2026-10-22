# MQ-10 Slow network

The customer app on Chrome "Slow 4G" with a 4× CPU slowdown, the closest emulation of a mid-range Android phone on a weak 4G signal. The tester records LCP on Home and Search, checks the loading states, and checks that the RUM numbers reach dashboard 05.

- **Covers:** 08 §5 MQ-10; 02 §11 budgets (customer LCP < 2.5 s p75, INP < 200 ms, CLS < 0.1, initial JS < 170 KB gzip); G1 loading states; OB-06 manual side.
- **Screens:** C-03, C-05, C-07, C-09.
- **Devices:** P360 emulation in Chrome DevTools.
- **Time:** about 40 minutes.

## Preconditions

1. `make reset`; `clock reset`; run between 10:00 and 18:00 IST.
2. Customer app served from a production build (`vite build` + preview through Caddy), not the dev server. Note in the run header which build was used. A dev server is slower and does not count for LCP.
3. Chrome DevTools → Network: throttling "Slow 4G", "Disable cache" off. Performance panel: CPU 4× slowdown.
4. Location QA point, radius 2 km, already set (so the location screen does not count).

## How to record LCP

Open DevTools → Performance → "Record and reload". In the timeline find the LCP marker. Record the value in seconds and the LCP element (for example the hero heading, the StoreMap card or the first ProductCard). Do 5 loads per route and record the median and the worst. The budget is the p75 of real users; 5 lab loads give a guide, not the p75.

## Steps

### A. Home

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-10.01 ★ | Cold load https://app.todayz.localhost/ (clear site data first) with Slow 4G and 4× CPU. | Within 1 s a skeleton shaped like Home shows (band, map card, shop cards). No blank white page and no spinner over 300 ms. | G1 C-03 | ▢ | |
| MQ-10.02 ★ | Record LCP for 5 cold loads. | Median LCP under 2.5 s. Record median, worst and the LCP element. | 02 §11 C-03 | ▢ | |
| MQ-10.03 | Record LCP for 5 warm loads (reload without clearing data). | Median under 2.5 s and lower than the cold median (service worker and cache serve the shell). | 02 §11 C-03 | | |
| MQ-10.04 | Watch the map card while it loads. | The map is lazy-loaded after the stats and shop cards. The stats ("4 shops nearby") show before the map tiles. | 02 §4.2 C-03 | ▢ | |
| MQ-10.05 | DevTools → Network: filter JS, sum the transferred size of the scripts loaded before the first interaction. | Initial JS under 170 KB gzip. Map and scanner chunks are not in the initial load. Record the total. | 02 §11 | ▢ | |
| MQ-10.06 | Performance → "Layout shift" track during load. | CLS under 0.1. No card jumps when the map or images load (they have reserved space). | 02 §11 C-03 | | |
| MQ-10.07 | Tap the 5 km chip. | The chip responds within 200 ms (INP). The list shows a skeleton then 6 shops. | 02 §11 `R-11.1` C-03 | | |

### B. Search and shop

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-10.08 ★ | Cold load https://app.todayz.localhost/search?q=amul 5 times. | Median LCP under 2.5 s. Record median, worst and element. | 02 §11 C-05 | ▢ | |
| MQ-10.09 | Type "maggi" letter by letter in the search field. | Each key shows in the field at once (no lag over 200 ms). Suggestions arrive after typing pauses, not one request per key without debounce (check the Network panel). | 02 §11 C-05 | | |
| MQ-10.10 | Press Enter. | Results skeleton, then results grouped by shop. | G1 C-05 | | |
| MQ-10.11 | Open Sri Lakshmi (C-07). | Header and first items within 2.5 s. Product images load lazily as you scroll and have fixed boxes (no shift). | 02 §11 C-07 | | |
| MQ-10.12 | Add Amul Butter to the cart and open C-09. | The cart opens from IndexedDB at once, then the quote refreshes. The total shows "updating" while the quote is pending, never a wrong total. | G1 C-09 | ▢ | |

### C. Offline and very slow

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-10.13 | DevTools → Network → Offline. Reload Home. | The offline banner shows. Cached shops and results show marked "may be out of date". | G1 06 §1 C-03 | ▢ | |
| MQ-10.14 | Open Search and type "atta". | A clear offline message; no endless spinner. | G1 C-05 | | |
| MQ-10.15 | Back to Slow 4G. | The banner goes away and data refreshes without a reload. | G1 C-03 | | |
| MQ-10.16 | Set a custom profile: 50 kbit/s down, 2,000 ms latency. Open Home. | Skeletons show; after 15 s or more the page still loads or shows the error state with "Try again". No crash and no blank page. | G1 C-03 | ▢ | |

### D. RUM reaches Grafana

| Step | Action | Expected result | Refs | Shot | P/F |
|---|---|---|---|---|---|
| MQ-10.17 ★ | After sections A and B, open https://grafana.todayz.localhost → TodayZ → 05 Customer app experience. Time range "Last 15 minutes". | Within 2 minutes of the last load, LCP, INP and CLS rows show for routes `/` and `/search`, `app=customer`. | OB-06 05 §4 | ▢ | |
| MQ-10.18 | Compare the dashboard's p75 LCP for `/` with your lab median. | Same order of size (within about 1 s). Record both. | OB-06 | | |
| MQ-10.19 | In DevTools Network filter `rum`. | Beacons go to `POST https://api.todayz.localhost/v1/rum` in batches (not one per metric). The body has route, metric and value, and no phone, name or token. | 05 §2.5 OB-02 | | |
| MQ-10.20 | Open dashboard 00 "What to fix today" → "Worst client screens". | If your loads were over budget, `/` or `/search` shows in this panel. Record what shows. The RUM regression alert fires only after 1 hour over 3 s, so it is not expected now. | 05 §4 §5 | | |

## Cleanup

1. DevTools: throttling "No throttling", CPU "No throttling".
2. `make reset` is not needed.
