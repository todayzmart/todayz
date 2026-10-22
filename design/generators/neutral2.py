import sys, re
S = sys.argv[1]; P = S + '/ds/project/'; C = P + 'components/'
def sub(path, pairs, must=True):
    t = open(path).read()
    for a, b in pairs:
        if a not in t:
            if must: print('MISSING in', path.split('/')[-2:], ':', a[:70])
            continue
        t = t.replace(a, b)
    open(path, 'w').write(t)

# screens.py
sub(S + '/gen/screens.py', [
 ('<span style="color:var(--brand)">', '<span style="color:var(--ink-2)">'),
 ('color:var(--brand);font:600 14px/20px', 'color:var(--ink);font:600 14px/20px'),
 ('color:var(--brand);font-weight:700', 'color:var(--ink);font-weight:700'),
 ('color:var(--on-forest-2)">11 items', 'color:var(--on-inverse-2)">11 items'),
 ('tz-btn-accent', 'tz-btn-brand'),
 ('<span style="font:600 12px/1 var(--font-sans);color:var(--on-forest-2)">Admin</span>', '<span class="tz-side-tag">Admin</span>'),
 ("' tz-side-light'", "''"),
 ('the pickup code on lime', 'the pickup code on the black `inverse` card'),
 ('same forest total bar', 'same black total bar'),
 ('forest sidebar with role-gated sections', 'the same light sidebar as the business app with an Admin tag, role-gated sections'),
 ("Light sidebar (`tz-side-light`) with the active item in `brand`", "Light sidebar with a grey active item and a `brand` icon"),
 ("the current hour solid `brand` and earlier hours `brand-soft`", "the current hour solid `brand` and earlier hours light grey"),
 ("dark `forest` tooltip", "black `inverse` tooltip"),
])
# previews
sub(C + 'SearchField/preview.html', [('<span style="color:var(--brand)">', '<span style="color:var(--ink-2)">')])
sub(C + 'PosCart/preview.html', [('var(--on-forest-2)', 'var(--on-inverse-2)'), ('tz-btn-accent', 'tz-btn-brand')])
sub(C + 'WalletCard/preview.html', [('tz-btn tz-btn-accent tz-btn-sm', 'tz-btn tz-btn-sm')])
b = open(C + 'Button/preview.html').read()
b = b.replace('<button class="tz-btn tz-btn-primary">Place order</button>', '<button class="tz-btn tz-btn-primary">Accept order</button>\n<button class="tz-btn tz-btn-brand">Place order</button>')
b = re.sub(r'<button class="tz-btn tz-btn-accent">.*?</button>\n', '', b)
b = b.replace('<button class="tz-btn tz-btn-primary tz-btn-lg">Charge ₹1,246</button>', '<button class="tz-btn tz-btn-primary tz-btn-lg">Save changes</button>\n<button class="tz-btn tz-btn-brand tz-btn-lg">Charge ₹1,246</button>')
open(C + 'Button/preview.html', 'w').write(b)

# cover: white and grey blocks, one black slab, one small green block
cv = open(C + 'Cover/preview.html').read()
cv = re.sub(r'\.slab\{.*?\.dot\{fill:var\(--lime\)\}', '''.slab{fill:var(--inverse)}
.white{fill:var(--surface);stroke:var(--line);stroke-width:1}
.grey{fill:var(--surface-sunken)}
.green{fill:var(--brand)}
.tile{rx:var(--radius-lg)}
.ring{fill:none;stroke:var(--on-inverse);stroke-width:1.5;opacity:.28}
.dot{fill:var(--brand)}''', cv, flags=re.S)
cv = re.sub(r'<!--\nblocks:.*?-->', '''<!--
blocks: inverse 192x264 slab, white surface 160x176 with a hairline, grey surface-sunken 120x152, brand 120x96; corners radius-lg.
arrangement: one tall slab with satellites, the slab and the grey block bleeding off the top, grey and green bleeding off the right edge.
pattern: literal motif, the "nearby" radius rings, because TodayZ is about how far the shop is: 4 rings at a space-8 (32px) pitch around a green centre, clipped to the slab.
colour: mostly white, grey and black; green is the one small block and the centre dot, as in the product.
-->''', cv, flags=re.S)
cv = cv.replace('<rect class="brand tile" x="208"', '<rect class="white tile" x="208.5"').replace('width="160" height="176"/>', 'width="159" height="175"/>')
cv = cv.replace('<rect class="lime tile"', '<rect class="grey tile"').replace('<rect class="forest tile"', '<rect class="green tile"')
open(C + 'Cover/preview.html', 'w').write(cv)
print('ok')
