# Kargul pass: white page, bordered cards, compact controls with an inner highlight,
# 16px icons in dense UI, medium-weight numbers. Edits tokens.json and bundle.css in place.
import json, re, sys
P = sys.argv[1]

# ---------- tokens ----------
tp = P + '/tokens.json'
t = json.load(open(tp))
col = t['color']['tokens']
def ctok(name):
    return next(x for x in col if x['name'] == name)
ctok('bg')['value'] = {'light': '#ffffff', 'dark': '#0a0a0a'}
ctok('bg')['usage'] = 'Page background in all three apps. White: cards and panels separate from it with a 1px line border, not with a grey ground.'
if not any(x['name'] == 'surface-subtle' for x in col):
    i = col.index(ctok('surface-sunken'))
    col.insert(i, {'name': 'surface-subtle', 'value': {'light': '#fafafa', 'dark': '#101010'},
                   'usage': 'The faintest grey: table header row, KPI footer band, row hover, the help card at the foot of the sidebar.'})
ctok('surface-sunken')['usage'] = 'Segmented-control track, the active sidebar item, soft buttons ("Change"), letter avatars, product image wells.'
ctok('line')['usage'] = 'Hairline border on every card, table, panel and the sidebar edge. This border, not a shadow, is what separates white from white.'
ctok('line-control')['usage'] = 'Border of inputs, secondary buttons, icon buttons, checkboxes and count badges.'

rad = {x['name']: x for x in t['radius']['tokens']}
rad['radius-xs']['value'] = '6px'; rad['radius-xs']['usage'] = 'Status pills, tags, count badges, checkbox, keyboard hints.'
rad['radius-sm']['value'] = '8px'; rad['radius-sm']['usage'] = 'Inputs, small buttons, icon buttons, sidebar items, filter buttons, segmented track.'
rad['radius-md']['value'] = '10px'; rad['radius-md']['usage'] = 'Default buttons, search field, toasts, banners on phone, product images.'
rad['radius-lg']['value'] = '12px'; rad['radius-lg']['usage'] = 'Cards, tables, KPI strip, store and product cards, POS cart panel.'
rad['radius-xl']['value'] = '20px'; rad['radius-xl']['usage'] = 'Top corners of bottom sheets; the cart bar.'
rad['radius-pill']['usage'] = 'Category chips, quantity stepper, round avatars, progress bars.'
t['radius']['note'] = 'Tight, even corners in the Kargul style: 8px on controls, 10px on buttons, 12px on cards. Nested corners stay concentric.'

sh = t['shadow']['tokens']
names = [x['name'] for x in sh]
sh[names.index('shadow-card')] = {'name': 'shadow-xs', 'value': {'light': '0 1px 2px rgba(10, 10, 10, 0.05)', 'dark': '0 1px 2px rgba(0, 0, 0, 0.4)'},
    'usage': 'Secondary buttons, inputs, the selected segment. Barely there; it lifts a control off white.'}
if 'shadow-btn' not in names:
    sh.insert(1, {'name': 'shadow-btn', 'value': {
        'light': '0 1px 2px rgba(10, 10, 10, 0.2), inset 0 1px 0 rgba(255, 255, 255, 0.16), inset 0 -2px 4px rgba(0, 0, 0, 0.12)',
        'dark': '0 1px 2px rgba(0, 0, 0, 0.6), inset 0 1px 0 rgba(255, 255, 255, 0.6), inset 0 -2px 4px rgba(0, 0, 0, 0.08)'},
        'usage': 'Filled buttons (primary and brand): a soft drop plus an inner top highlight, as on Kargul Studio buttons.'})
t['shadow']['note'] = 'Cards have no shadow; they use a line border. Shadows are for controls (xs, btn) and for things that float (pop, sheet).'

sz = {x['name']: x for x in t['size']['tokens']}
sz['control-sm']['value'] = '32px'; sz['control-sm']['usage'] = 'Small buttons, icon buttons, filter buttons, segmented control and table actions on desktop.'
sz['control-md']['value'] = '36px'; sz['control-md']['usage'] = 'Default height for inputs and buttons in the business and admin apps.'
sz['control-lg']['value'] = '44px'; sz['control-lg']['usage'] = 'Default height for inputs and buttons in the customer app and on any touch screen (density "touch").'
if 'control-xl' not in sz:
    lst = t['size']['tokens']; lst.insert(lst.index(sz['control-lg']) + 1,
        {'name': 'control-xl', 'value': '48px', 'usage': 'Checkout, Place order and onboarding buttons on phone.'})
sz['sidebar']['value'] = '248px'

for g in t['type']['groups']:
    for s in g['styles']:
        n = s['name']
        if n == 'display': s.update(fontSize='28px', lineHeight='32px', fontWeight=650)
        if n == 'kpi': s.update(fontSize='28px', lineHeight='32px', fontWeight=500, letterSpacing='-0.02em'); s['usage'] = 'Dashboard numbers: medium weight, not bold, as in the Kargul dashboards. Figtree with tabular figures.'
        if n == 'title': s.update(fontSize='22px', lineHeight='28px', fontWeight=600, letterSpacing='-0.01em'); s['usage'] = 'Desktop page title, with a 14px ink-2 subline under it.'
        if n == 'heading': s.update(fontSize='15px', lineHeight='22px', fontWeight=600); s['usage'] = 'Card titles and section titles; a 13px ink-2 description may follow.'
        if n == 'body': s.update(fontSize='14px', lineHeight='20px'); s['usage'] = 'Default text in the business and admin apps, table cells, nav items.'
        if n == 'body-lg': s['usage'] = 'Default text in the customer app, and inputs on phone so iOS never zooms.'
        if n == 'label': s.update(fontSize='13px', lineHeight='18px', fontWeight=500); s['usage'] = 'Form labels, button text in small buttons, filter buttons.'
        if n == 'overline': s.update(fontWeight=500, letterSpacing='0.06em'); s['usage'] = 'Sidebar group labels only. Table headers are sentence case.'
open(tp, 'w').write(json.dumps(t, indent=2, ensure_ascii=False) + '\n')

# ---------- bundle.css: replace whole rules by selector ----------
cp = P + '/components/bundle.css'
css = open(cp).read()
R = {
'body': 'body { margin: 0; font-family: var(--font-sans); font-size: 14px; line-height: 20px; background: var(--bg); color: var(--ink); -webkit-font-smoothing: antialiased; }',
'.tz-ico': '.tz-ico { width: 20px; height: 20px; flex: none; fill: none; stroke: currentColor; stroke-width: 1.5; stroke-linecap: round; stroke-linejoin: round; }',
'.tz-btn': '.tz-btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: var(--control-md); padding: 0 14px; border-radius: var(--radius-md); border: 1px solid var(--line-control); background: var(--surface); color: var(--ink); box-shadow: var(--shadow-xs); font: 500 14px/20px var(--font-sans); cursor: pointer; white-space: nowrap; }',
'.tz-btn:hover': '.tz-btn:hover { background: var(--surface-subtle); }',
'.tz-btn-primary': '.tz-btn-primary { background: var(--inverse); border-color: var(--inverse); color: var(--on-inverse); box-shadow: var(--shadow-btn); }',
'.tz-btn-brand': '.tz-btn-brand { background: var(--brand); border-color: var(--brand); color: var(--on-brand); box-shadow: var(--shadow-btn); }',
'.tz-btn-ghost': '.tz-btn-ghost { background: transparent; border-color: transparent; box-shadow: none; color: var(--ink-2); }',
'.tz-btn-ghost:hover': '.tz-btn-ghost:hover { background: var(--surface-sunken); color: var(--ink); }',
'.tz-btn-danger': '.tz-btn-danger { background: var(--danger); border-color: var(--danger); color: var(--on-danger); box-shadow: var(--shadow-btn); }',
'.tz-btn-sm': '.tz-btn-sm { min-height: var(--control-sm); padding: 0 10px; border-radius: var(--radius-sm); font-size: 13px; }',
'.tz-btn-lg': '.tz-btn-lg { min-height: var(--control-xl); padding: 0 var(--space-5); font-size: 15px; font-weight: 600; }',
'.tz-btn[disabled]': '.tz-btn[disabled] { background: var(--surface-sunken); border-color: var(--line); color: var(--ink-3); box-shadow: none; cursor: not-allowed; }',
'.tz-btn-icon': '.tz-btn-icon { width: var(--control-md); padding: 0; border-radius: var(--radius-sm); color: var(--ink-2); }',
'.tz-field': '.tz-field { display: flex; flex-direction: column; gap: 6px; }',
'.tz-field-label': '.tz-field-label { font: 500 13px/18px var(--font-sans); color: var(--ink); }',
'.tz-input': '.tz-input { min-height: var(--control-md); width: 100%; padding: 0 var(--space-3); border: 1px solid var(--line-control); border-radius: var(--radius-sm); background: var(--surface); color: var(--ink); box-shadow: var(--shadow-xs); font: 400 14px/20px var(--font-sans); }',
'.tz-search': '.tz-search { display: flex; align-items: center; gap: var(--space-2); min-height: var(--control-md); padding: 0 var(--space-3); border-radius: var(--radius-md); background: var(--surface); border: 1px solid var(--line-control); box-shadow: var(--shadow-xs); color: var(--ink-3); }',
'.tz-search input': '.tz-search input { flex: 1; min-width: 0; border: 0; background: transparent; color: var(--ink); font: 400 14px/20px var(--font-sans); outline: none; }',
'.tz-chip': '.tz-chip { display: inline-flex; align-items: center; gap: 6px; height: var(--control-sm); padding: 0 var(--space-3); border-radius: var(--radius-pill); border: 1px solid var(--line-control); background: var(--surface); color: var(--ink); font: 500 13px/18px var(--font-sans); white-space: nowrap; cursor: pointer; }',
'.tz-pill': '.tz-pill { display: inline-flex; align-items: center; gap: 6px; height: 22px; padding: 0 7px; border-radius: var(--radius-xs); border: 1px solid var(--line); font: 500 12px/16px var(--font-sans); white-space: nowrap; background: var(--surface); color: var(--ink); }',
'.tz-pill-ready': '.tz-pill-ready { --dot: var(--brand); background: var(--brand-soft); border-color: color-mix(in srgb, var(--brand) 22%, transparent); color: var(--brand); }',
'.tz-stock': '.tz-stock { display: inline-flex; align-items: center; gap: 6px; font: 500 12px/16px var(--font-sans); color: var(--ink); }',
'.tz-stock::before': '.tz-stock::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: var(--brand); }',
'.tz-stepper': '.tz-stepper { display: inline-flex; align-items: center; height: var(--control-sm); border-radius: var(--radius-pill); background: var(--inverse); color: var(--on-inverse); box-shadow: var(--shadow-btn); font: 600 14px/20px var(--font-sans); font-variant-numeric: tabular-nums; }',
'.tz-stepper button': '.tz-stepper button { width: 32px; height: 32px; border: 0; background: transparent; color: inherit; display: grid; place-items: center; cursor: pointer; border-radius: var(--radius-pill); }',
'.tz-add': '.tz-add { height: var(--control-sm); padding: 0 var(--space-4); border-radius: var(--radius-pill); border: 1px solid var(--line-control); background: var(--surface); color: var(--ink); box-shadow: var(--shadow-xs); font: 600 13px/18px var(--font-sans); cursor: pointer; }',
'.tz-thumb': '.tz-thumb { aspect-ratio: 1; max-width: 100%; border-radius: var(--radius-md); background: var(--media); display: grid; place-items: center; color: var(--ink-2); font: 600 22px/1 var(--font-display); }',
'.tz-pcard': '.tz-pcard { display: flex; flex-direction: column; gap: var(--space-2); padding: var(--space-2) var(--space-2) var(--space-3); border-radius: var(--radius-lg); background: var(--surface); border: 1px solid var(--line); min-width: 0; }',
'.tz-price': '.tz-price { font: 600 15px/20px var(--font-sans); font-variant-numeric: tabular-nums; color: var(--ink); }',
'.tz-off': '.tz-off { font: 600 12px/16px var(--font-sans); color: var(--ink); white-space: nowrap; }',
'.tz-scard': '.tz-scard { display: grid; grid-template-columns: 48px 1fr auto; gap: var(--space-3); align-items: center; padding: var(--space-3); border-radius: var(--radius-lg); background: var(--surface); border: 1px solid var(--line); }',
'.tz-avatar': '.tz-avatar { width: 48px; height: 48px; border-radius: var(--radius-md); background: var(--surface-sunken); border: 1px solid var(--line); color: var(--ink); display: grid; place-items: center; font: 600 18px/1 var(--font-display); }',
'.tz-scard-name': '.tz-scard-name { font: 600 15px/22px var(--font-sans); color: var(--ink); }',
'.tz-tag': '.tz-tag { display: inline-flex; align-items: center; gap: 4px; height: 22px; padding: 0 7px; border-radius: var(--radius-xs); border: 1px solid var(--line); background: var(--surface); color: var(--ink-2); font: 500 12px/16px var(--font-sans); }',
'.tz-tag-open': '.tz-tag-open { color: var(--ink); }',
'.tz-tag-offer': '.tz-tag-offer { color: var(--ink); }',
'.tz-cartbar': '.tz-cartbar { display: flex; align-items: center; gap: var(--space-3); min-height: 60px; padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4); border-radius: var(--radius-xl); background: var(--brand); color: var(--on-brand); box-shadow: var(--shadow-btn), var(--shadow-pop); }',
'.tz-cartbar-cta': '.tz-cartbar-cta { display: inline-flex; align-items: center; gap: 4px; height: var(--control-lg); padding: 0 var(--space-4); border-radius: var(--radius-md); background: var(--on-brand); color: var(--brand); font: 600 14px/20px var(--font-sans); border: 0; }',
'.tz-bnav a': '.tz-bnav a { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2px; color: var(--ink-3); text-decoration: none; font: 500 11px/14px var(--font-sans); }',
'.tz-bnav a[aria-current="page"] .tz-ico': '.tz-bnav a[aria-current="page"] .tz-ico { stroke-width: 2; }',
'.tz-side': '.tz-side { width: var(--sidebar); background: var(--surface); color: var(--ink); border-right: 1px solid var(--line); padding: var(--space-3); display: flex; flex-direction: column; gap: 1px; }',
'.tz-side-brand': '.tz-side-brand { display: flex; align-items: center; gap: var(--space-2); height: 40px; padding: 0 var(--space-2); margin-bottom: var(--space-2); font: 700 18px/1 var(--font-display); }',
'.tz-side-group': '.tz-side-group { padding: var(--space-4) var(--space-2) 6px; font: 500 11px/14px var(--font-sans); letter-spacing: .06em; text-transform: uppercase; color: var(--ink-3); }',
'.tz-side a': '.tz-side a { display: flex; align-items: center; gap: var(--space-2); height: 32px; padding: 0 var(--space-2); border-radius: var(--radius-sm); color: var(--ink-2); text-decoration: none; font: 400 14px/20px var(--font-sans); }',
'.tz-side a:hover': '.tz-side a:hover { color: var(--ink); background: var(--surface-subtle); }',
'.tz-side a[aria-current="page"]': '.tz-side a[aria-current="page"] { background: var(--surface-sunken); color: var(--ink); font-weight: 500; }',
'.tz-side a .tz-count': '.tz-side a .tz-count { min-width: 20px; height: 18px; padding: 0 5px; border-radius: var(--radius-xs); display: inline-grid; place-items: center; background: var(--surface); border: 1px solid var(--line-control); color: var(--ink-2); }',
'.tz-side a[aria-current="page"] .tz-count': '.tz-side a[aria-current="page"] .tz-count { color: var(--ink); }',
'.tz-side-tag': '.tz-side-tag { font: 500 11px/1 var(--font-sans); color: var(--ink-2); background: var(--surface); border: 1px solid var(--line-control); border-radius: var(--radius-xs); padding: 3px 6px; }',
'.tz-kpi': '.tz-kpi { display: flex; flex-direction: column; gap: var(--space-1); padding: var(--space-4) var(--space-5); border-radius: var(--radius-lg); background: var(--surface); border: 1px solid var(--line); min-width: 0; }',
'.tz-kpi-label': '.tz-kpi-label { display: inline-flex; align-items: center; gap: 6px; font: 500 13px/18px var(--font-sans); color: var(--ink-2); }',
'.tz-kpi-value': '.tz-kpi-value { font: 500 28px/32px var(--font-sans); font-variant-numeric: tabular-nums; letter-spacing: -0.02em; color: var(--ink); }',
'.tz-kpi-lead .tz-kpi-value': '.tz-kpi-lead .tz-kpi-value { font-size: 32px; line-height: 36px; }',
'.tz-kpi-foot': '.tz-kpi-foot { display: flex; align-items: center; gap: var(--space-2); font: 400 13px/18px var(--font-sans); color: var(--ink-2); }',
'.tz-kpi-delta': '.tz-kpi-delta { display: inline-flex; align-items: center; gap: 2px; font: 500 13px/18px var(--font-sans); font-variant-numeric: tabular-nums; color: var(--brand); }',
'.tz-kpi-delta-down': '.tz-kpi-delta-down { color: var(--danger); }',
'.tz-kpi-delta-flat': '.tz-kpi-delta-flat { color: var(--ink-2); }',
'.tz-code-digits': '.tz-code-digits { font: 600 40px/44px var(--font-display); letter-spacing: .12em; font-variant-numeric: tabular-nums; }',
'.tz-otp input': '.tz-otp input { width: 52px; height: 60px; text-align: center; border: 1px solid var(--line-control); border-radius: var(--radius-md); background: var(--surface); color: var(--ink); box-shadow: var(--shadow-xs); font: 600 26px/1 var(--font-display); }',
'.tz-otp input:focus': '.tz-otp input:focus { border-color: var(--ink); outline: 2px solid var(--focus); outline-offset: 2px; }',
'.tz-steps b': '.tz-steps b { display: block; font: 600 14px/20px var(--font-sans); }',
'.tz-banner': '.tz-banner { display: flex; gap: var(--space-3); align-items: flex-start; padding: var(--space-3) var(--space-4); border-radius: var(--radius-lg); border: 1px solid color-mix(in srgb, var(--info) 22%, transparent); background: var(--info-soft); color: var(--ink); font: 400 14px/20px var(--font-sans); }',
'.tz-banner-warning': '.tz-banner-warning { background: var(--warning-soft); border-color: color-mix(in srgb, var(--warning) 28%, transparent); }',
'.tz-banner b': '.tz-banner b { font-weight: 600; }',
'.tz-toast': '.tz-toast { display: inline-flex; align-items: center; gap: var(--space-3); padding: 10px var(--space-4); border-radius: var(--radius-md); background: var(--inverse); color: var(--on-inverse); box-shadow: var(--shadow-pop); font: 400 14px/20px var(--font-sans); }',
'.tz-toast a': '.tz-toast a { color: var(--on-inverse); font-weight: 600; text-decoration: underline; text-underline-offset: 3px; }',
'.tz-table-wrap': '.tz-table-wrap { overflow-x: auto; border-radius: var(--radius-lg); background: var(--surface); border: 1px solid var(--line); }',
'.tz-table th': '.tz-table th { text-align: left; padding: 0 var(--space-4); height: 36px; background: var(--surface-subtle); color: var(--ink-2); font: 500 13px/18px var(--font-sans); white-space: nowrap; border-bottom: 1px solid var(--line); }',
'.tz-table td': '.tz-table td { padding: 0 var(--space-4); height: 44px; border-top: 1px solid var(--line); white-space: nowrap; }',
'.tz-table tr:hover td': '.tz-table tbody tr:hover td { background: var(--surface-subtle); }',
'.tz-pos-tile': '.tz-pos-tile { display: flex; flex-direction: column; justify-content: space-between; gap: var(--space-2); min-height: 96px; padding: var(--space-3); border-radius: var(--radius-lg); background: var(--surface); border: 1px solid var(--line); text-align: left; cursor: pointer; font: 500 14px/18px var(--font-sans); color: var(--ink); }',
'.tz-pos-total .tz-amt': '.tz-pos-total .tz-amt { font: 600 28px/32px var(--font-sans); letter-spacing: -0.02em; font-variant-numeric: tabular-nums; }',
'.tz-wallet-pts': '.tz-wallet-pts { font: 500 36px/40px var(--font-sans); letter-spacing: -0.02em; font-variant-numeric: tabular-nums; color: var(--on-inverse); }',
'.tz-lang button': '.tz-lang button { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; min-height: 72px; padding: var(--space-3) var(--space-4); border-radius: var(--radius-lg); border: 1px solid var(--line-control); background: var(--surface); color: var(--ink); cursor: pointer; text-align: left; }',
'.tz-lang button[aria-pressed="true"]': '.tz-lang button[aria-pressed="true"] { border-color: var(--ink); box-shadow: inset 0 0 0 1px var(--ink); }',
'.tz-lang .native': '.tz-lang .native { font: 600 20px/1.5 var(--font-display); }',
'.tz-loc b': '.tz-loc b { display: flex; align-items: center; gap: 4px; font: 600 15px/22px var(--font-sans); }',
'.tz-h': '.tz-h { margin: 0; font: 600 16px/22px var(--font-sans); color: var(--ink); }',
'.tz-card': '.tz-card { background: var(--surface); border-radius: var(--radius-lg); border: 1px solid var(--line); padding: var(--space-5); display: flex; flex-direction: column; gap: var(--space-4); min-width: 0; }',
'.tz-card-title': '.tz-card-title { font: 600 15px/22px var(--font-sans); margin: 0; }',
'.tz-card-foot': '.tz-card-foot { display: flex; justify-content: space-between; font: 400 13px/18px var(--font-sans); color: var(--ink-2); border-top: 1px dashed var(--line-control); padding-top: var(--space-3); }',
'.tz-card-foot a': '.tz-card-foot a { color: var(--ink); font-weight: 500; text-decoration: none; }',
'.tz-ocard': '.tz-ocard { background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-lg); padding: var(--space-4); display: flex; flex-direction: column; gap: var(--space-3); }',
'.tz-ocard-top': '.tz-ocard-top { display: flex; justify-content: space-between; align-items: baseline; gap: var(--space-2); font: 600 14px/20px var(--font-sans); }',
'.tz-timer': '.tz-timer { display: inline-flex; align-items: center; gap: 4px; font: 500 12px/16px var(--font-sans); font-variant-numeric: tabular-nums; color: var(--warning); }',
'.tz-table tfoot td': '.tz-table tfoot td { font-weight: 600; background: var(--surface-subtle); }',
'.tz-filter': '.tz-filter { display: inline-flex; align-items: center; gap: 6px; height: var(--control-sm); padding: 0 10px; border-radius: var(--radius-sm); border: 1px dashed var(--line-control); background: var(--surface); color: var(--ink-2); font: 400 13px/18px var(--font-sans); }',
'.tz-filter b': '.tz-filter b { color: var(--ink); font-weight: 500; }',
'.tz-filter[data-set]': '.tz-filter[data-set] { border-style: solid; box-shadow: var(--shadow-xs); }',
'.tz-side-card': '.tz-side-card { margin-top: auto; padding: var(--space-3); border-radius: var(--radius-lg); border: 1px solid var(--line); background: var(--surface-subtle); color: var(--ink); font: 400 13px/18px var(--font-sans); display: flex; flex-direction: column; gap: var(--space-2); }',
'.tz-side-card b': '.tz-side-card b { font: 600 13px/18px var(--font-sans); }',
'.tz-progress': '.tz-progress { height: 4px; border-radius: var(--radius-pill); background: color-mix(in srgb, var(--ink) 10%, transparent); overflow: hidden; }',
'.tz-topbar .tz-search': '.tz-topbar .tz-search { flex: 1; max-width: 360px; }',
'.tz-kpi-icon': '.tz-kpi-icon { width: 32px; height: 32px; border-radius: var(--radius-sm); display: grid; place-items: center; background: var(--surface); border: 1px solid var(--line-control); color: var(--ink-2); }',
'.tz-seg': '.tz-seg { display: inline-flex; padding: 2px; gap: 2px; border-radius: var(--radius-sm); background: var(--surface-sunken); }',
'.tz-seg button': '.tz-seg button { height: 28px; padding: 0 var(--space-3); border: 0; border-radius: 6px; background: transparent; color: var(--ink-2); font: 500 13px/18px var(--font-sans); cursor: pointer; }',
'.tz-seg button[aria-pressed="true"]': '.tz-seg button[aria-pressed="true"] { background: var(--surface); color: var(--ink); box-shadow: 0 0 0 1px var(--line), var(--shadow-xs); }',
'.tz-activity > div': '.tz-activity > div { flex: 1; min-width: 0; font: 500 14px/20px var(--font-sans); }',
'.tz-action': '.tz-action { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) 0; border-top: 1px solid var(--line); font: 400 14px/20px var(--font-sans); color: var(--ink); text-decoration: none; }',
'.tz-action b': '.tz-action b { font-weight: 500; font-variant-numeric: tabular-nums; }',
'.tz-kpi-name': '.tz-kpi-name { display: inline-flex; align-items: center; gap: 6px; font: 500 13px/18px var(--font-sans); color: var(--ink-2); }',
'.tz-kpi-cell-lead .tz-kpi-value': '.tz-kpi-cell-lead .tz-kpi-value { font-size: 32px; line-height: 36px; }',
'.tz-kpi-cell-foot': '.tz-kpi-cell-foot { display: flex; align-items: center; gap: var(--space-2); min-height: 40px; padding: 0 var(--space-5); border-top: 1px solid var(--line); background: var(--surface-subtle); font: 400 13px/18px var(--font-sans); color: var(--ink-2); }',
'.tz-kpi-cell-foot a': '.tz-kpi-cell-foot a { color: var(--ink); font-weight: 500; text-decoration: none; }',
'.tz-sync': '.tz-sync { display: inline-flex; align-items: center; gap: 6px; font: 400 13px/18px var(--font-sans); color: var(--ink-2); }',
'.tz-props': '.tz-props { display: grid; grid-template-columns: 140px 1fr; gap: var(--space-3) var(--space-4); font: 400 14px/20px var(--font-sans); }',
}
lines = css.split('\n'); hit = set()
for i, ln in enumerate(lines):
    m = re.match(r'^(.+?) \{', ln)
    if m and m.group(1) in R:
        lines[i] = R[m.group(1)]; hit.add(m.group(1))
miss = set(R) - hit
if miss: print('MISSING', sorted(miss))
css = '\n'.join(lines)
css = css.replace('.tz-tag-open::before { content: ""; width: 6px; height: 6px;', '.tz-tag-open::before { content: ""; width: 6px; height: 6px;')
if '/* Kargul pass' not in css:
    css += '''
/* Kargul pass: icon sizes, soft buttons, page titles, touch density */
.tz-btn .tz-ico, .tz-side .tz-ico, .tz-kpi-name .tz-ico, .tz-kpi-label .tz-ico, .tz-filter .tz-ico, .tz-seg .tz-ico, .tz-table .tz-ico, .tz-kpi-icon .tz-ico, .tz-props .tz-ico, .tz-sync .tz-ico, .tz-tag .tz-ico, .tz-pill .tz-ico, .tz-search .tz-ico { width: 16px; height: 16px; }
.tz-btn-icon.tz-btn-sm { width: var(--control-sm); }
.tz-btn-soft { background: var(--surface-sunken); border-color: transparent; box-shadow: none; }
.tz-btn-soft:hover { background: color-mix(in srgb, var(--ink) 8%, var(--surface)); }
.tz-btn-soft-danger { background: var(--danger-soft); border-color: transparent; box-shadow: none; color: var(--danger); }
.tz-btn-soft-danger:hover { background: color-mix(in srgb, var(--danger) 16%, var(--surface)); }
.tz-btn-primary .tz-ico, .tz-btn-brand .tz-ico { opacity: .9; }
.tz-iconring { width: 40px; height: 40px; flex: none; border-radius: 50%; border: 1px solid var(--line-control); background: var(--surface); display: grid; place-items: center; color: var(--ink-2); box-shadow: var(--shadow-xs); }
.tz-page-title { margin: 0; font: 600 22px/28px var(--font-sans); letter-spacing: -0.01em; color: var(--ink); }
.tz-page-sub { margin: 4px 0 0; font: 400 14px/20px var(--font-sans); color: var(--ink-2); }
.tz-card-desc { margin: 2px 0 0; font: 400 13px/18px var(--font-sans); color: var(--ink-2); }
.tz-card-head-split { padding-bottom: var(--space-4); margin: 0 calc(-1 * var(--space-5)); padding-left: var(--space-5); padding-right: var(--space-5); border-bottom: 1px solid var(--line); }
.tz-sep { border: 0; border-top: 1px dashed var(--line-control); margin: 0; }
.tz-lettermark { width: 24px; height: 24px; flex: none; border-radius: var(--radius-xs); border: 1px solid var(--line-control); background: var(--surface-sunken); display: inline-grid; place-items: center; font: 500 11px/1 var(--font-sans); color: var(--ink-2); }
/* Touch density: the customer app and phones get 44px controls and 16px inputs */
:where([data-density="touch"], .tz-phone) { font-size: 15px; line-height: 22px; }
:where([data-density="touch"], .tz-phone) .tz-btn:not(.tz-btn-sm), :where([data-density="touch"], .tz-phone) .tz-input { min-height: var(--control-lg); }
:where([data-density="touch"], .tz-phone) .tz-btn-lg { min-height: var(--control-xl); }
:where([data-density="touch"], .tz-phone) .tz-btn-icon:not(.tz-btn-sm) { width: var(--control-lg); }
:where([data-density="touch"], .tz-phone) .tz-search { min-height: var(--control-lg); }
:where([data-density="touch"], .tz-phone) :is(.tz-input, .tz-search input) { font-size: 16px; }
:where([data-density="touch"], .tz-phone) :is(.tz-chip, .tz-add, .tz-stepper) { height: 36px; }
:where([data-density="touch"], .tz-phone) .tz-stepper button { width: 36px; height: 36px; }
'''
open(cp, 'w').write(css)
print('ok', len(hit))
