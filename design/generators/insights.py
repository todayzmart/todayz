import sys,json,os
S=sys.argv[1]; P=f'{S}/ds/project'; ic=json.load(open(f'{S}/gen/icons.json'))
I=lambda n: f'<svg class="tz-ico" viewBox="0 0 24 24" aria-hidden="true">{ic[n]}</svg>'
rows=[('green','trend','Selling fast','<strong>Amul Taaza Milk 500 ml</strong> sold 64 this week. At this pace you run out on Saturday.','Restock'),
      ('amber','hourglass','Sitting on the shelf','<strong>Kissan Mixed Fruit Jam 500 g</strong>: 12 in stock, no sale in 21 days.','Create offer'),
      ('rose','warn','Going to waste','<strong>3 items expire within 5 days</strong>: Nandini Curd, Modern Bread, Milky Mist Paneer.','Flash offer'),
      ('sky','search','Wanted nearby','<strong>Nivea Body Lotion 200 ml</strong>: 14 searches within 2 km this week. No shop nearby has it.','Add product')]
card=('<section class="tz-card"><div class="tz-card-head"><div><h2 class="tz-card-title">Demand insights</h2><p class="tz-card-desc">What sells, what sits, what is about to go to waste, and what people nearby want</p></div><span class="tz-tag">Last 7 days</span></div><div class="tz-insights">'
      + ''.join(f'<div class="tz-insight" data-tint="{t}"><span class="tz-insight-icon">{I(i)}</span><div><b>{h}</b><small>{d}</small></div><a href="#">{a} →</a></div>' for t,i,h,d,a in rows)
      + '</div><div class="tz-card-foot"><a href="#">All insights</a><span>From your sales, stock and searches near your shop</span></div></section>')
pf=f'{P}/components/BusinessOverview/preview.html'; h=open(pf).read()
a=h.index('<section class="tz-card"><div class="tz-card-head"><h2 class="tz-card-title">Needs your attention</h2>')
b=h.index('<section class="tz-card" style="padding:0;gap:0">',a)
h=h[:a]+card+'\n'+h[b:]
h=h.replace('<div style="display:grid;grid-template-columns:1fr 1.7fr;gap:var(--space-4);align-items:start">','<div style="display:grid;grid-template-columns:1fr;gap:var(--space-4);align-items:start">')
h=h.replace(card, card.replace('class="tz-insights"','class="tz-insights tz-insights-2col"'))
old='<div style="display:flex;justify-content:space-between;align-items:flex-end;gap:var(--space-3)"><div><h1 class="tz-page-title">Good evening, Kavya</h1>'
if old in h: h=h.replace(old,'<div class="tz-greet"><div><h1 class="tz-page-title">Good evening, Kavya</h1>')
h=h.replace('height=900','height=1280').replace('min-height:900px','min-height:1280px')
open(pf,'w').write(h)
os.makedirs(f'{P}/components/DemandInsights',exist_ok=True)
open(f'{P}/components/DemandInsights/preview.html','w').write('<!-- @dsCard group="Business" height=470 width=620 -->\n<!doctype html>\n<html lang="en">\n<head><meta charset="utf-8"><title>Demand insights</title></head>\n<body>\n<div style="padding:24px;max-width:560px">'+card+'</div>\n</body>\n</html>\n')
open(f'{P}/components/DemandInsights/README.md','w').write("""# DemandInsights

The business overview card the founders liked on the old site, rebuilt in the system. Four signals, each a tinted row with one action.

| Row | Tint | Icon | Signal | Action |
|---|---|---|---|---|
| Selling fast | `green` | trending-up | sales velocity vs stock: days until it runs out | Restock (opens reorder list) |
| Sitting on the shelf | `amber` | hourglass | in stock, no sale in N days (default 21) | Create offer |
| Going to waste | `rose` | alert-triangle | batches expiring within N days (default 5) | Flash offer (offer to nearby customers) |
| Wanted nearby | `sky` | search | customer searches within 2 km for items this shop does not stock, and how many nearby shops do | Add product |

- Each row: 32px tinted icon square, a 13px/600 heading, one sentence with the product in 500 `ink`, and a text action with →.
- Show at most one row per signal on the overview; "All insights" opens the full list with filters.
- Rows with nothing to say are hidden; if all four are empty, show "Nothing needs you right now" with the setup checklist instead.
- The tag top right shows the window (Last 7 days). The footer says where the numbers come from.
- "Wanted nearby" is the seed of TodayZ's demand data for brands: it never names the searching customer.
""")
print('ok')
