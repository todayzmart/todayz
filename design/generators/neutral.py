import json, re, sys
S = sys.argv[1]; P = S + '/ds/project/'

# ---------- tokens ----------
tp = P + 'tokens.json'; T = json.load(open(tp))
toks = T['color']['tokens']
upd = {
 'bg': ('#f7f7f7', '#0a0a0a', 'Page background behind the cards in all three apps. A true neutral grey, a step off white.'),
 'surface': ('#ffffff', '#141414', 'Cards, sheets, list rows, table body, the sidebar, the POS cart. Most of every screen is this white.'),
 'surface-sunken': ('#f4f4f4', '#1c1c1c', 'Input fills, segmented-control track, table header row, the active sidebar item, neutral pills and tags.'),
 'surface-raised': ('#ffffff', '#1f1f1f', 'Popovers, menus, dialogs; pair with shadow-pop.'),
 'media': ('#f4f4f4', '#1c1c1c', 'Neutral tile behind product photos and initials.'),
 'line': ('#ebebeb', '#262626', 'Hairline dividers between rows and around cards. Decorative only.'),
 'line-control': ('#d4d4d4', '#3a3a3a', 'Borders of inputs, checkboxes, chips and secondary buttons. Every control also has a visible label or placeholder.'),
 'ink': ('#171717', '#ededed', 'Primary text and icons. Near-black, never pure #000.'),
 'ink-2': ('#5c5c5c', '#a3a3a3', 'Secondary text: units, store addresses, timestamps, table metadata. 4.5:1+ on every surface.'),
 'ink-3': ('#737373', '#8a8a8a', 'Placeholder text, disabled labels, group labels. Never for prices or stock.'),
 'brand': ('#067a3c', '#4cc46e', 'The green accent, spent in a few places only: success and freshness signals (in stock, ready, completed, synced, a rise), the chart line and highlighted bar, progress fills, the active sidebar icon, and the customer cart bar and checkout button (`tz-btn-brand`). Never a page, card or sidebar fill.'),
 'brand-strong': ('#0e593c', '#7fdb96', 'Hover and pressed state of brand fills.'),
 'brand-soft': ('#e8f5ec', '#13261a', 'Small tinted backgrounds only: the Ready pill, the rise chip, the "stock updated" tag. Never a card or panel fill.'),
}
add = [
 {'name': 'inverse', 'value': {'light': '#171717', 'dark': '#ededed'}, 'usage': 'The solid fill: primary buttons, selected chips, the quantity stepper, toasts, tooltips, the POS total bar, the pickup code and the wallet card. Near-black in light theme, near-white in dark.'},
 {'name': 'on-inverse', 'value': {'light': '#ffffff', 'dark': '#0a0a0a'}, 'usage': 'Text and icons on inverse.'},
 {'name': 'on-inverse-2', 'value': {'light': '#a3a3a3', 'dark': '#5c5c5c'}, 'usage': 'Secondary text on inverse.'},
]
drop = {'lime', 'lime-soft', 'on-lime', 'forest', 'on-forest', 'on-forest-2'}
out = []
for t in toks:
    n = t['name']
    if n in drop:
        if n == 'forest': out.extend(add)
        continue
    if n in upd:
        l, d, u = upd[n]; t['value'] = {'light': l, 'dark': d}; t['usage'] = u
    out.append(t)
T['color']['tokens'] = out
json.dump(T, open(tp, 'w'), indent=2, ensure_ascii=False)
names = {t['name'] for t in out}

# ---------- css ----------
cp = P + 'components/bundle.css'; css = open(cp).read()
R = {
'.tz-btn-primary {': '.tz-btn-primary { background: var(--inverse); border-color: var(--inverse); color: var(--on-inverse); }',
'.tz-btn-primary:hover {': '.tz-btn-primary:hover { background: color-mix(in srgb, var(--inverse) 85%, var(--bg)); border-color: color-mix(in srgb, var(--inverse) 85%, var(--bg)); }\n.tz-btn-brand { background: var(--brand); border-color: var(--brand); color: var(--on-brand); }\n.tz-btn-brand:hover { background: var(--brand-strong); border-color: var(--brand-strong); }',
'.tz-btn-accent {': None,
'.tz-btn-ghost {': '.tz-btn-ghost { background: transparent; border-color: transparent; color: var(--ink); }',
'.tz-btn-ghost:hover {': '.tz-btn-ghost:hover { background: var(--surface-sunken); }',
'.tz-chip[aria-pressed="true"] {': '.tz-chip[aria-pressed="true"] { background: var(--inverse); border-color: var(--inverse); color: var(--on-inverse); }',
'.tz-pill {': '.tz-pill { display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 var(--space-2); border-radius: var(--radius-xs); font: 600 12px/16px var(--font-sans); white-space: nowrap; background: var(--surface-sunken); color: var(--ink); }',
'.tz-pill::before {': '.tz-pill::before { content: ""; width: 6px; height: 6px; border-radius: 50%; background: var(--dot, var(--ink-3)); }',
'.tz-pill-pending {': '.tz-pill-pending { --dot: var(--warning); }',
'.tz-pill-accepted, .tz-pill-delivery {': '.tz-pill-accepted, .tz-pill-delivery { --dot: var(--info); }',
'.tz-pill-preparing {': '.tz-pill-preparing { --dot: var(--teal); }',
'.tz-pill-ready {': '.tz-pill-ready { --dot: var(--brand); background: var(--brand-soft); color: var(--brand); }',
'.tz-pill-completed, .tz-pill-paid {': '.tz-pill-completed, .tz-pill-paid { --dot: var(--brand); }',
'.tz-pill-rejected, .tz-pill-cancelled {': '.tz-pill-rejected, .tz-pill-cancelled { --dot: var(--danger); }',
'.tz-stock {': '.tz-stock { display: inline-flex; align-items: center; gap: 6px; font: 600 12px/16px var(--font-sans); color: var(--ink); }',
'.tz-stock::before {': '.tz-stock::before { content: ""; width: 8px; height: 8px; border-radius: 50%; background: var(--brand); }',
'.tz-stock-low {': '.tz-stock-low::before { background: var(--warning); }',
'.tz-stock-out {': '.tz-stock-out { color: var(--ink-2); }',
'.tz-stock-out::before {': '.tz-stock-out::before { background: transparent; border: 1.5px solid var(--ink-3); }',
'.tz-tag-offer {': '.tz-tag-offer { background: var(--surface-sunken); color: var(--ink); }',
'.tz-cartbar-count {': '.tz-cartbar-count { display: grid; place-items: center; min-width: 28px; height: 28px; padding: 0 6px; border-radius: var(--radius-pill); background: color-mix(in srgb, var(--on-brand) 22%, transparent); color: var(--on-brand); font: 700 14px/1 var(--font-sans); }',
'.tz-bnav a[aria-current="page"] {': '.tz-bnav a[aria-current="page"] { color: var(--ink); }',
'.tz-side {': '.tz-side { width: var(--sidebar); background: var(--surface); color: var(--ink); border-right: 1px solid var(--line); padding: var(--space-4) var(--space-3); display: flex; flex-direction: column; gap: 2px; }',
'.tz-side-brand b {': '.tz-side-brand b { color: var(--brand-mark); }',
'.tz-side-group {': '.tz-side-group { padding: var(--space-4) var(--space-3) var(--space-1); font: 700 11px/14px var(--font-sans); letter-spacing: .08em; text-transform: uppercase; color: var(--ink-3); }',
'.tz-side a {': '.tz-side a { display: flex; align-items: center; gap: var(--space-3); height: 40px; padding: 0 var(--space-3); border-radius: var(--radius-sm); color: var(--ink-2); text-decoration: none; font: 500 14px/20px var(--font-sans); }',
'.tz-side a:hover {': '.tz-side a:hover { color: var(--ink); background: var(--surface-sunken); }',
'.tz-side a[aria-current="page"] {': '.tz-side a[aria-current="page"] { background: var(--surface-sunken); color: var(--ink); font-weight: 600; }',
'.tz-side a[aria-current="page"] .tz-ico {': '.tz-side a[aria-current="page"] .tz-ico { color: var(--brand); }',
'.tz-side a .tz-count { min-width': '.tz-side a .tz-count { min-width: 20px; height: 20px; padding: 0 6px; border-radius: var(--radius-pill); display: inline-grid; place-items: center; background: var(--surface-sunken); color: var(--ink-2); }\n.tz-side a[aria-current="page"] .tz-count { background: var(--surface); color: var(--ink); }\n.tz-side-tag { font: 600 11px/1 var(--font-sans); color: var(--ink-2); background: var(--surface-sunken); border: 1px solid var(--line); border-radius: var(--radius-xs); padding: 4px 6px; }',
'.tz-code {': '.tz-code { display: flex; flex-direction: column; align-items: center; gap: var(--space-2); padding: var(--space-6) var(--space-4); border-radius: var(--radius-lg); background: var(--inverse); color: var(--on-inverse); text-align: center; }',
'.tz-steps li[data-now] .tz-dot {': '.tz-steps li[data-now] .tz-dot { background: var(--surface); border-color: var(--brand); box-shadow: inset 0 0 0 4px var(--surface), inset 0 0 0 12px var(--brand); }',
'.tz-toast {': '.tz-toast { display: inline-flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border-radius: var(--radius-md); background: var(--inverse); color: var(--on-inverse); box-shadow: var(--shadow-pop); font: 500 14px/20px var(--font-sans); }',
'.tz-toast a {': '.tz-toast a { color: var(--on-inverse); font-weight: 700; text-decoration: underline; text-underline-offset: 3px; }',
'.tz-pos-total {': '.tz-pos-total { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-4); border-radius: var(--radius-lg); background: var(--inverse); color: var(--on-inverse); }',
'.tz-wallet {': '.tz-wallet { display: flex; flex-direction: column; gap: var(--space-2); padding: var(--space-5); border-radius: var(--radius-lg); background: var(--inverse); color: var(--on-inverse); }',
'.tz-wallet-pts {': '.tz-wallet-pts { font: 700 40px/44px var(--font-display); font-variant-numeric: tabular-nums; color: var(--on-inverse); }',
'.tz-wallet small {': '.tz-wallet small { font: 500 13px/18px var(--font-sans); color: var(--on-inverse-2); }',
'.tz-lang button[aria-pressed="true"] {': '.tz-lang button[aria-pressed="true"] { border-color: var(--ink); background: var(--surface-sunken); }',
'.tz-card-foot a {': '.tz-card-foot a { color: var(--ink); font-weight: 600; text-decoration: none; }',
'.tz-chart .bar-muted {': '.tz-chart .bar-muted { fill: color-mix(in srgb, var(--ink) 12%, transparent); }',
'.tz-chart .bar-pale {': '.tz-chart .bar-pale { fill: color-mix(in srgb, var(--ink) 12%, transparent); }',
'.tz-filter b {': '.tz-filter b { color: var(--ink); font-weight: 700; }',
'.tz-side-card {': '.tz-side-card { margin-top: auto; padding: var(--space-4); border-radius: var(--radius-md); background: var(--surface-sunken); color: var(--ink); font: 500 13px/18px var(--font-sans); display: flex; flex-direction: column; gap: var(--space-2); }',
'.tz-tip {': '.tz-tip { position: absolute; background: var(--inverse); color: var(--on-inverse); border-radius: var(--radius-sm); padding: var(--space-2) var(--space-3); font: 500 12px/16px var(--font-sans); font-variant-numeric: tabular-nums; box-shadow: var(--shadow-pop); white-space: nowrap; }',
'.tz-kpi-icon {': '.tz-kpi-icon { width: 36px; height: 36px; border-radius: var(--radius-sm); display: grid; place-items: center; background: var(--surface-sunken); color: var(--ink-2); }',
'.tz-kpi-icon.info {': None, '.tz-kpi-icon.warning {': None, '.tz-kpi-icon.teal {': None,
'.tz-spark i {': '.tz-spark i { width: 5px; min-height: 2px; border-radius: 1px; background: color-mix(in srgb, var(--ink) 16%, transparent); }',
'.tz-spark i:last-child {': '.tz-spark i:last-child { background: var(--ink); }',
'.tz-kpi-cell-foot a {': '.tz-kpi-cell-foot a { color: var(--ink); font-weight: 700; text-decoration: none; }',
}
lines = css.split('\n'); new = []; hit = set(); skip_hero = False
for ln in lines:
    s = ln.strip()
    if s.startswith('/* Hero KPI card'): skip_hero = True; continue
    if skip_hero:
        if s.startswith('.tz-hero') or s == '': continue
        skip_hero = False
    if s.startswith('/* Light sidebar') or (s.startswith('.tz-side-light')): continue
    if s.startswith('/* KPI tile with a tinted'): new.append('/* Neutral icon square (activity list) */'); continue
    k = next((k for k in R if s.startswith(k)), None)
    if k is None: new.append(ln); continue
    hit.add(k)
    if R[k] is not None: new.append(R[k])
miss = set(R) - hit
css = '\n'.join(new)
css = css.replace('border: 8px solid var(--forest)', 'border: 8px solid #1a1a1a')
open(cp, 'w').write(css)
left = re.findall(r'var\(--([a-z0-9-]+)\)', css)
undefined = sorted({v for v in left if v not in names and not v.startswith(('space-','radius-','font-','shadow-','control-','dot','tap','pos-','phone','content','sidebar','focus','teal','warning','danger','info','on-danger','on-brand','brand-mark'))})
print('css misses:', miss); print('css undefined vars:', undefined)
