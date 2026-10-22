import json, sys, os
S=sys.argv[1]; P=f'{S}/ds/project'
# ---- icons
ic=json.load(open(f'{S}/gen/icons.json'))
ic.update({
 'trend':'<path d="M22 7 13.5 15.5l-5-5L2 17"/><path d="M16 7h6v6"/>',
 'hourglass':'<path d="M5 22h14M5 2h14"/><path d="M17 22v-4.2a2 2 0 0 0-.6-1.4L12 12l-4.4 4.4a2 2 0 0 0-.6 1.4V22M7 2v4.2a2 2 0 0 0 .6 1.4L12 12l4.4-4.4a2 2 0 0 0 .6-1.4V2"/>',
 'pill':'<path d="m10.5 20.5 10-10a4.95 4.95 0 1 0-7-7l-10 10a4.95 4.95 0 1 0 7 7z"/><path d="m8.5 8.5 7 7"/>',
 'sparkle':'<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="M19 17v4M17 19h4"/>',
 'phone':'<rect x="6" y="2" width="12" height="20" rx="2"/><path d="M11 18h2"/>',
 'pen':'<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
 'wrench':'<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.8-3.8a6 6 0 0 1-7.9 7.9l-6.9 6.9a2.1 2.1 0 0 1-3-3l6.9-6.9a6 6 0 0 1 7.9-7.9z"/>',
 'map':'<path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2z"/><path d="M9 4v14M15 6v14"/>',
 'locate':'<circle cx="12" cy="12" r="7"/><circle cx="12" cy="12" r="2"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/>',
 'expand':'<path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"/>',
})
json.dump(ic,open(f'{S}/gen/icons.json','w'))
# ---- tokens
t=json.load(open(f'{P}/tokens.json')); toks=t['color']['tokens']
names=[x['name'] for x in toks]
new=[
 ('wash','#f4faf6','The light colour of TodayZ: a pale mint for hero bands (top of the customer home, the greeting band on the business overview, marketing sections), the active sidebar item, the KPI lead cell and the setup card. Pale enough that white cards on it still read as white.'),
 ('wash-strong','#e5f3ea','Top stop of the hero-band gradient and the active sidebar item. Never behind body text smaller than 13px.'),
 ('wash-line','#d3eadb','Border of a washed band or washed card, in place of `line`.'),
 ('tint-green','#e8f6ed','Pastel fill, paired with tint-green-ink: Grocery category, "Selling fast" insight.'),
 ('tint-green-ink','#0b6b37','Icon and text on tint-green.'),
 ('tint-amber','#fff3d9','Pastel fill: Stationery category, "Sitting on the shelf" insight.'),
 ('tint-amber-ink','#7a4a00','Icon and text on tint-amber.'),
 ('tint-rose','#fdecec','Pastel fill: Pharmacy category, "Going to waste" insight.'),
 ('tint-rose-ink','#a3302b','Icon and text on tint-rose.'),
 ('tint-sky','#eaf2ff','Pastel fill: Electronics category, "Wanted nearby" insight.'),
 ('tint-sky-ink','#1f55b0','Icon and text on tint-sky.'),
 ('tint-lilac','#f2edfe','Pastel fill: Beauty category.'),
 ('tint-lilac-ink','#5a33b5','Icon and text on tint-lilac.'),
 ('tint-peach','#ffefe3','Pastel fill: Hardware category.'),
 ('tint-peach-ink','#963f0c','Icon and text on tint-peach.'),
 ('map-land','#f0f4ee','Map base: land.'),
 ('map-park','#d9eed6','Map: parks and open ground.'),
 ('map-water','#d6e7f6','Map: lakes and rivers.'),
 ('map-road','#ffffff','Map: road fill.'),
 ('map-road-edge','#dde3db','Map: road casing.'),
]
for n,v,u in new:
    if n in names: continue
    toks.append({'name':n,'value':{'light':v},'usage':u})
for x in toks:
    if x['name']=='bg': x['usage']='Page background in all apps: white. Light colour comes from `wash` bands and `tint-*` pastels placed on top, never from tinting the whole page.'
    if x['name']=='brand': x['usage']=x['usage'].replace('Never a page, card or sidebar fill.','Never a page, card or sidebar fill (the pale `wash` mint is the light colour for bands, not brand).')
json.dump(t,open(f'{P}/tokens.json','w'),indent=2,ensure_ascii=False)
# contrast check of each tint ink on its fill
def L(h):
    h=h.lstrip('#'); c=[int(h[i:i+2],16)/255 for i in (0,2,4)]
    c=[x/12.92 if x<=0.03928 else ((x+0.055)/1.055)**2.4 for x in c]; return 0.2126*c[0]+0.7152*c[1]+0.0722*c[2]
d={n:v for n,v,u in new}
for h in ['green','amber','rose','sky','lilac','peach']:
    a,b=L(d[f'tint-{h}']),L(d[f'tint-{h}-ink']); print(h, round((max(a,b)+.05)/(min(a,b)+.05),2))
for n in ['wash','wash-strong']:
    a,b=L(d[n]),L('#5c5c5c'); print(n,'ink-2',round((a+.05)/(b+.05),2))
# ---- css
css='''
/* Light colour pass (cofounder feedback: "pure plain white"). White stays the page; mint wash bands and pastel tints add colour on top. */
.tz-side a[aria-current="page"] { background: var(--wash-strong); }
.tz-side a:hover { background: var(--wash); }
.tz-side-card { background: var(--wash); border-color: var(--wash-line); }
.tz-kpi-cell-lead { background: linear-gradient(180deg, var(--wash) 0%, var(--surface) 100%); }
.tz-kpi-cell-lead .tz-kpi-cell-foot { background: transparent; }
/* Hero band: the top of the customer home and the business greeting. Mint fading to white. */
.tz-hero-wash { background: linear-gradient(180deg, var(--wash-strong) 0%, var(--wash) 60%, var(--surface) 100%); }
.tz-greet { display: flex; justify-content: space-between; align-items: flex-end; gap: var(--space-3); padding: var(--space-5) var(--space-6); border-radius: var(--radius-lg); border: 1px solid var(--wash-line); background: linear-gradient(100deg, var(--wash-strong) 0%, var(--wash) 45%, var(--surface) 100%); }
/* Tints: set data-tint on any element, children read --t-bg / --t-ink */
[data-tint="green"] { --t-bg: var(--tint-green); --t-ink: var(--tint-green-ink); }
[data-tint="amber"] { --t-bg: var(--tint-amber); --t-ink: var(--tint-amber-ink); }
[data-tint="rose"] { --t-bg: var(--tint-rose); --t-ink: var(--tint-rose-ink); }
[data-tint="sky"] { --t-bg: var(--tint-sky); --t-ink: var(--tint-sky-ink); }
[data-tint="lilac"] { --t-bg: var(--tint-lilac); --t-ink: var(--tint-lilac-ink); }
[data-tint="peach"] { --t-bg: var(--tint-peach); --t-ink: var(--tint-peach-ink); }
.tz-avatar[data-tint] { background: var(--t-bg); color: var(--t-ink); border-color: transparent; }
/* Category tiles */
.tz-cats { display: flex; gap: var(--space-3); overflow-x: auto; scrollbar-width: none; }
.tz-cat { flex: none; width: 64px; display: flex; flex-direction: column; align-items: center; gap: 6px; text-decoration: none; color: var(--ink); font: 500 12px/16px var(--font-sans); text-align: center; }
.tz-cat i { width: 56px; height: 56px; border-radius: var(--radius-lg); display: grid; place-items: center; background: var(--t-bg); color: var(--t-ink); }
.tz-cat .tz-ico { width: 24px; height: 24px; }
/* Store map: map with radius ring, counts, radius chips */
.tz-map-card { background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-lg); overflow: hidden; display: flex; flex-direction: column; }
.tz-map { position: relative; line-height: 0; border-bottom: 1px solid var(--line); }
.tz-map svg { width: 100%; height: auto; display: block; }
.tz-map-live { position: absolute; left: 10px; top: 10px; display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 8px; border-radius: var(--radius-xs); background: var(--surface); border: 1px solid var(--line); font: 500 12px/16px var(--font-sans); color: var(--ink); box-shadow: var(--shadow-xs); }
.tz-map-live::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: var(--brand); }
.tz-map-btn { position: absolute; right: 10px; bottom: 10px; width: 32px; height: 32px; border-radius: var(--radius-sm); background: var(--surface); border: 1px solid var(--line-control); display: grid; place-items: center; color: var(--ink); box-shadow: var(--shadow-xs); }
.tz-map-btn .tz-ico { width: 16px; height: 16px; }
.tz-map-legend { position: absolute; right: 10px; top: 10px; display: inline-flex; gap: 8px; height: 24px; align-items: center; padding: 0 8px; border-radius: var(--radius-xs); background: var(--surface); border: 1px solid var(--line); font: 500 11px/14px var(--font-sans); color: var(--ink-2); }
.tz-map-legend span { display: inline-flex; align-items: center; gap: 4px; }
.tz-map-legend span::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: var(--brand-mark); }
.tz-map-legend span.closed::before { background: #a3a3a3; }
.tz-map-legend span.you::before { background: var(--info); }
.tz-map-stats { display: grid; grid-template-columns: repeat(3, 1fr); }
.tz-map-stats div { display: flex; flex-direction: column; gap: 2px; padding: var(--space-3) var(--space-4); border-left: 1px solid var(--line); }
.tz-map-stats div:first-child { border-left: 0; }
.tz-map-stats b { font: 500 22px/26px var(--font-sans); font-variant-numeric: tabular-nums; letter-spacing: -0.02em; color: var(--ink); }
.tz-map-stats small { font: 500 12px/16px var(--font-sans); color: var(--ink-2); }
.tz-map-radius { display: flex; align-items: center; gap: var(--space-2); padding: var(--space-3) var(--space-4); border-top: 1px solid var(--line); background: var(--surface-subtle); }
.tz-map-radius > span { font: 500 12px/16px var(--font-sans); color: var(--ink-2); white-space: nowrap; }
.tz-map-radius .tz-chip { height: 28px; padding: 0 10px; font-size: 12px; }
/* Demand insights: four tinted rows, one per signal */
.tz-insights { display: flex; flex-direction: column; gap: var(--space-2); }
.tz-insight { display: grid; grid-template-columns: 32px 1fr auto; gap: var(--space-3); align-items: center; padding: var(--space-3); border-radius: var(--radius-md); background: color-mix(in srgb, var(--t-bg) 55%, var(--surface)); border: 1px solid color-mix(in srgb, var(--t-bg) 100%, var(--line)); }
.tz-insight-icon { width: 32px; height: 32px; border-radius: var(--radius-sm); display: grid; place-items: center; background: var(--t-bg); color: var(--t-ink); }
.tz-insight-icon .tz-ico { width: 16px; height: 16px; }
.tz-insight b { display: block; font: 600 13px/18px var(--font-sans); color: var(--ink); }
.tz-insight small { display: block; font: 400 13px/18px var(--font-sans); color: var(--ink-2); }
.tz-insight small strong { font-weight: 500; color: var(--ink); }
.tz-insight a { font: 500 13px/18px var(--font-sans); color: var(--ink); text-decoration: none; white-space: nowrap; }
'''
b=open(f'{P}/components/bundle.css').read()
if 'Light colour pass' not in b: open(f'{P}/components/bundle.css','w').write(b+css)
print('mint ok')
