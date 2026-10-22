import os, sys, json
ROOT=sys.argv[1]; P=json.load(open(os.path.join(os.path.dirname(__file__),'icons.json')))
AC=' aria-current="page"'
DIM=' style="opacity:.55"'
WARN=' style="color:var(--warning)"'
CNT=lambda x: ('<span class="tz-count">'+x[2]+'</span>') if len(x)>2 else ''
def I(n,cls=''): return f'<svg class="tz-ico {cls}" viewBox="0 0 24 24" aria-hidden="true">{P[n]}</svg>'
def doc(marker,title,body,style=''):
    return f'{marker}\n<!doctype html>\n<html lang="en">\n<head><meta charset="utf-8"><title>{title}</title><style>{style}</style></head>\n<body>\n{body}\n</body>\n</html>\n'
STEP=lambda q: f'<div class="tz-stepper"><button aria-label="Remove one">{I("minus","tz-ico-sm")}</button><span>{q}</span><button aria-label="Add one">{I("plus","tz-ico-sm")}</button></div>'
ADD='<button class="tz-add">Add</button>'
def pc(ini,name,unit,price,mrp,off,stock,fresh,btn,shop=''):
    m=f'<span class="tz-mrp">{mrp}</span> <span class="tz-off">{off}</span>' if mrp else '<span class="tz-mrp" style="text-decoration:none">&nbsp;</span>'
    return f'<article class="tz-pcard"><div class="tz-thumb" style="aspect-ratio:16/10">{ini}</div><div class="tz-pcard-name">{name}</div><div class="tz-pcard-unit">{unit}{shop}</div><div>{stock}<div class="tz-fresh">{fresh}</div></div><div class="tz-pcard-foot"><div><div class="tz-price">{price}</div>{m}</div>{btn}</div></article>'
CART='<div style="padding:0 var(--space-3) var(--space-3)"><div class="tz-cartbar"><span class="tz-cartbar-count">3</span><span class="tz-cartbar-text">₹345<small>from Sri Lakshmi Provision Stores</small></span><button class="tz-cartbar-cta">View cart'+I('chev','tz-ico-sm')+'</button></div></div>'
def nav(cur):
    items=[('home','Home'),('search','Search'),('receipt','Orders'),('wallet','Wallet'),('user','Account')]
    return '<nav class="tz-bnav" aria-label="Customer">'+''.join(f'<a href="#"{AC if l==cur else ""}>{I(i)}{l}</a>' for i,l in items)+'</nav>'
def MAP(w=312,h=150):
    cx,cy=w*0.5,h*0.52
    pins=[(0.30,0.40,1),(0.62,0.30,1),(0.70,0.62,1),(0.40,0.70,1),(0.55,0.48,0),(0.80,0.40,1),(0.24,0.62,0),(0.47,0.26,1)]
    P=''.join(f'<circle cx="{w*x:.0f}" cy="{h*y:.0f}" r="5" fill="{"var(--brand-mark)" if o else "#a3a3a3"}" stroke="#fff" stroke-width="2"/>' for x,y,o in pins)
    road=lambda d,wd: f'<path d="{d}" fill="none" stroke="var(--map-road-edge)" stroke-width="{wd+2}" stroke-linecap="round"/><path d="{d}" fill="none" stroke="var(--map-road)" stroke-width="{wd}" stroke-linecap="round"/>'
    return (f'<svg viewBox="0 0 {w} {h}" role="img" aria-label="Map: 43 shops within 2 km, 38 open now">'
      f'<rect width="{w}" height="{h}" fill="var(--map-land)"/>'
      f'<path d="M0 {h*0.78} Q {w*0.2} {h*0.7} {w*0.28} {h} L0 {h}z" fill="var(--map-park)"/>'
      f'<path d="M{w*0.78} 0 Q {w*0.86} {h*0.18} {w} {h*0.2} L{w} 0z" fill="var(--map-park)"/>'
      f'<ellipse cx="{w*0.88}" cy="{h*0.84}" rx="{w*0.12}" ry="{h*0.14}" fill="var(--map-water)"/>'
      + road(f'M-10 {h*0.35} L{w+10} {h*0.55}',7) + road(f'M{w*0.36} -10 L{w*0.5} {h+10}',7)
      + road(f'M-10 {h*0.9} Q {w*0.45} {h*0.72} {w+10} {h*0.95}',4) + road(f'M{w*0.7} -10 L{w*0.62} {h+10}',4) + road(f'M{w*0.12} -10 L{w*0.2} {h+10}',3)
      + f'<circle cx="{cx}" cy="{cy}" r="{h*0.44}" fill="var(--brand)" fill-opacity=".06" stroke="var(--brand-mark)" stroke-width="1.5" stroke-dasharray="4 4"/>'
      + P
      + f'<circle cx="{w*0.66}" cy="{h*0.48}" r="12" fill="#fff" stroke="var(--brand-mark)" stroke-width="2"/><text x="{w*0.66}" y="{h*0.48+4}" text-anchor="middle" style="font:600 11px var(--font-sans);fill:var(--ink)">12</text>'
      + f'<circle cx="{cx}" cy="{cy}" r="14" fill="var(--info)" fill-opacity=".14"/><circle cx="{cx}" cy="{cy}" r="6" fill="var(--info)" stroke="#fff" stroke-width="2"/>'
      + '</svg>')
def MAPCARD(radius='2 km'):
    chips=''.join(f'<button class="tz-chip" aria-pressed="{"true" if k==radius else "false"}">{k}</button>' for k in ['1 km','2 km','5 km','10 km','20 km'])
    return (f'<section class="tz-map-card" aria-label="Shops on the map"><div class="tz-map">{MAP()}<span class="tz-map-live">Live near you</span><span class="tz-map-legend"><span>Open</span><span class="closed">Closed</span><span class="you">You</span></span><button class="tz-map-btn" aria-label="Open full map">{I("expand")}</button></div>'
      '<div class="tz-map-stats"><div><b>43</b><small>Shops nearby</small></div><div><b>38</b><small>Open now</small></div><div><b>1,189</b><small>Products</small></div></div>'
      f'<div class="tz-map-radius" role="group" aria-label="Search radius"><div class="tz-chips">{chips}</div></div></section>')
CATS=[('Grocery','bag','green'),('Pharmacy','pill','rose'),('Beauty','sparkle','lilac'),('Electronics','phone','sky'),('Stationery','pen','amber'),('Hardware','wrench','peach')]
CATROW='<div class="tz-cats">'+''.join(f'<a class="tz-cat" href="#" data-tint="{t}"><i>{I(ic)}</i>{n}</a>' for n,ic,t in CATS)+'</div>'
home=f'''<div class="tz-phone"><div class="tz-phone-body" style="padding:0;gap:0">
<div class="tz-hero-wash" style="padding:var(--space-4) var(--space-4) var(--space-3);display:flex;flex-direction:column;gap:var(--space-3)">
<div class="tz-appbar"><div class="tz-loc"><b>{I('pin','tz-ico-sm')}Jayanagar 4th Block{I('down','tz-ico-sm')}</b><small>Pickup and delivery · Bengaluru 560011</small></div><button class="tz-btn tz-btn-icon" aria-label="Inbox">{I('bell')}</button></div>
<label class="tz-search" for="q1">{I('search')}<input id="q1" type="search" placeholder="Search atta, milk, medicines…"><span style="color:var(--ink-2)">{I('mic')}</span></label>
</div>
<div style="padding:0 var(--space-4) var(--space-4);display:flex;flex-direction:column;gap:var(--space-4)">
{MAPCARD()}
{CATROW}
<div style="display:flex;justify-content:space-between;align-items:center"><h2 class="tz-h">Shops near you</h2><a href="#" style="color:var(--ink);font:500 14px/20px var(--font-sans);text-decoration:none">See all 43</a></div>
<a class="tz-scard" href="#" style="text-decoration:none;color:inherit"><div class="tz-avatar" data-tint="green">SL</div><div style="min-width:0"><div class="tz-scard-name">Sri Lakshmi Provision</div><div class="tz-meta"><span>0.6 km</span><span>· 1,240 items</span></div><div class="tz-meta" style="margin-top:6px"><span class="tz-tag tz-tag-open">Open till 10 pm</span><span class="tz-tag">5% back</span></div></div><span style="color:var(--ink-2)">{I('chev')}</span></a>
<a class="tz-scard" href="#" style="text-decoration:none;color:inherit"><div class="tz-avatar" data-tint="rose">MP</div><div style="min-width:0"><div class="tz-scard-name">MedPlus Pharmacy</div><div class="tz-meta"><span>0.9 km</span><span>· 860 items</span></div><div class="tz-meta" style="margin-top:6px"><span class="tz-tag tz-tag-open">Open 24 hours</span><span class="tz-tag">Delivery</span></div></div><span style="color:var(--ink-2)">{I('chev')}</span></a>
</div>
</div>{CART}{nav('Home')}</div>'''
results=f'''<div class="tz-phone"><div class="tz-phone-body">
<div class="tz-appbar" style="gap:var(--space-2)"><button class="tz-btn tz-btn-icon" aria-label="Back" style="transform:scaleX(-1);border-color:transparent">{I('chev')}</button><label class="tz-search" for="q2" style="flex:1;min-height:44px">{I('search')}<input id="q2" type="search" value="atta"><span style="color:var(--ink-2)">{I('x','tz-ico-sm')}</span></label></div>
<div class="tz-chips"><button class="tz-chip" aria-pressed="true">Within 2 km</button><button class="tz-chip" aria-pressed="false">Open now</button><button class="tz-chip" aria-pressed="false">Delivery</button><button class="tz-chip" aria-pressed="false">Offers</button></div>
<div style="font:500 13px/18px var(--font-sans);color:var(--ink-2)">18 products in 5 shops</div>
<div style="display:flex;justify-content:space-between;align-items:baseline"><div><h2 class="tz-h">Sri Lakshmi Provision</h2><div class="tz-meta"><span>Pickup in 15 min · 0.6 km</span></div><div class="tz-meta" style="margin-top:4px"><span class="tz-tag tz-tag-open">Stock updated 4 min ago</span></div></div><a href="#" style="color:var(--ink);font:600 14px/20px var(--font-sans);text-decoration:none">Shop</a></div>
<div class="tz-grid2">{pc('AT','Aashirvaad Shudh Chakki Atta','5 kg','₹289','₹320','10% off','<span class="tz-stock">In stock</span>','12 min ago',STEP(1))}{pc('PA','Pillsbury Chakki Fresh Atta','5 kg','₹265','₹295','10% off','<span class="tz-stock tz-stock-low">Only 3 left</span>','1 hr ago',ADD)}</div>
<div style="display:flex;justify-content:space-between;align-items:baseline"><div><h2 class="tz-h">Sharma General Store</h2><div class="tz-meta"><span>Closed · opens 7 am · 1.2 km</span></div><div class="tz-meta" style="margin-top:4px"><span class="tz-tag">Stock updated 3 hr ago</span></div></div><a href="#" style="color:var(--ink);font:600 14px/20px var(--font-sans);text-decoration:none">Shop</a></div>
<div class="tz-grid2">{pc('AT','Aashirvaad Shudh Chakki Atta','5 kg','₹299','₹320','7% off','<span class="tz-stock">In stock</span>','3 hr ago',ADD)}{pc('FA','Fortune Chakki Fresh Atta','10 kg','₹520','','','<span class="tz-stock tz-stock-out">Out of stock</span>','today, 9:15 am','<button class="tz-add" style="border-color:var(--line-control);color:var(--ink-2)">Notify</button>')}</div>
</div>{CART}{nav('Search')}</div>'''
order=f'''<div class="tz-phone"><div class="tz-phone-body" style="gap:var(--space-4)">
<div class="tz-appbar"><div><div style="font:500 13px/18px var(--font-sans);color:var(--ink-2)">Order TZ-24817 · Pickup</div><div style="font:600 22px/28px var(--font-sans);letter-spacing:-0.01em">Ready for pickup</div></div><span class="tz-pill tz-pill-paid" style="background:var(--surface-sunken);color:var(--ink-2)">Pay at pickup</span></div>
<div class="tz-code"><span style="font:600 14px/20px var(--font-sans)">Your pickup code</span><span class="tz-code-digits">4827</span><span style="font:500 13px/18px var(--font-sans)">Show this at the counter</span></div>
<div class="tz-scard" style="grid-template-columns:1fr auto"><div style="min-width:0"><div class="tz-scard-name">Sri Lakshmi Provision Stores</div><div class="tz-meta"><span>0.6 km</span><span>· 14, 9th Main, Jayanagar 4th Block</span></div></div><button class="tz-btn tz-btn-sm">{I('nav','tz-ico-sm')}Directions</button></div>
<div style="background:var(--surface);border-radius:var(--radius-lg);border:1px solid var(--line);padding:var(--space-4) var(--space-4) 0">
<ol class="tz-steps">
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Order placed</b><small>6:02 pm · 3 items · ₹345</small></div></li>
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Accepted by the shop</b><small>6:04 pm</small></div></li>
<li data-done><span class="tz-dot">{I('check','tz-ico-sm')}</span><div><b>Preparing</b><small>6:05 pm</small></div></li>
<li data-now><span class="tz-dot">{I('bag','tz-ico-sm')}</span><div><b>Ready for pickup</b><small>6:18 pm · held for you till 9:00 pm</small></div></li>
<li><span class="tz-dot"></span><div><b>Collected</b></div></li>
</ol></div>
</div>{nav('Orders')}</div>'''
os.makedirs(f'{ROOT}/components/CustomerScreens',exist_ok=True)
open(f'{ROOT}/components/CustomerScreens/preview.html','w').write(doc('<!-- @dsCard group="Screens" height=800 width=1180 -->','Customer app screens',f'<div style="display:flex;gap:32px;padding:24px 16px;justify-content:center">{home}{results}{order}</div>'))
open(f'{ROOT}/components/CustomerScreens/README.md','w').write('''# CustomerScreens

Three key customer screens assembled from the system: home, search results grouped by shop, and an order that is ready for pickup.

- Home: location, search hero, radius chips, nearby shops, popular products, cart bar, tab bar.
- Search results: products grouped by shop and sorted by distance, each with live stock and freshness.
- Order: the pickup code on the black `inverse` card, shop with Directions, and the full time-stamped timeline.
''')

# POS
tiles=[('Aashirvaad Atta','5 kg','₹289','24 in stock'),('Fortune Sunflower Oil','1 L','₹165','3 left'),('Amul Taaza Milk','500 ml','₹28','36 in stock'),('Tata Salt','1 kg','₹28','Out of stock'),('Maggi Noodles','4 × 70 g','₹56','40 in stock'),('Surf Excel Easy Wash','1 kg','₹140','12 in stock'),('Good Day Cashew','200 g','₹40','18 in stock'),('Toor Dal (loose)','per kg','₹168','22 kg'),('Sona Masoori Rice','5 kg','₹395','9 in stock'),('Nandini Curd','400 g','₹32','14 in stock'),('Colgate Strong Teeth','200 g','₹105','20 in stock'),('Parle-G','800 g','₹90','30 in stock')]
tl=''.join(f'<button class="tz-pos-tile"{DIM if "Out" in s else ""}><span>{n}<small style="display:block">{u}</small></span><span style="display:flex;justify-content:space-between;align-items:baseline;gap:8px"><span class="tz-price">{p}</span><small{WARN if "left" in s else ""}>{s}</small></span></button>' for n,u,p,s in tiles)
side=lambda items,cur,brand,groups,extra='': f'<nav class="tz-side{extra}" aria-label="Main"><div class="tz-side-brand">{brand}</div>'+''.join((f'<div class="tz-side-group">{x[1]}</div>' if x[0]=='g' else f'<a href="#"{AC if x[1]==cur else ""}>{I(x[0])}{x[1]}{CNT(x)}</a>') for x in items)+'</nav>'
bitems=[('chart','Overview'),('receipt','Orders','3'),('scan','Sell'),('box','Stock'),('users','Customers'),('g','Grow'),('tag','Offers'),('gift','Referrals and wallet'),('chart','Analytics'),('g','Shop'),('store','Stores and staff'),('msg','Help'),('cog','Settings')]
cartlines=''.join(f'<div class="tz-pos-line"><div>{n}<span class="tz-sub">{u}</span></div><div class="tz-stepper" style="height:32px"><button aria-label="Remove one" style="width:32px;height:32px">{I("minus","tz-ico-sm")}</button><span>{q}</span><button aria-label="Add one" style="width:32px;height:32px">{I("plus","tz-ico-sm")}</button></div><b class="tz-num">{t}</b></div>' for n,u,q,t in [('Aashirvaad Atta','5 kg · ₹289','2','₹578'),('Fortune Sunflower Oil','1 L · ₹165','3','₹495'),('Amul Taaza Milk','500 ml · ₹28','6','₹168')])
pos=f'''<div style="display:flex;height:760px;background:var(--bg)">
{side(bitems,'Sell','<span>Today<b>Z</b></span>',None,'')}
<main style="flex:1;min-width:0;display:grid;grid-template-columns:1fr 380px;gap:var(--space-6);padding:var(--space-6)">
<section style="display:flex;flex-direction:column;gap:var(--space-4);min-width:0">
<div style="display:flex;justify-content:space-between;align-items:center;gap:var(--space-3)"><div><h1 class="tz-page-title">Sell</h1><p class="tz-page-sub">Sri Lakshmi Provision Stores · Counter 1</p></div><button class="tz-btn">{I('mic')}Voice update</button></div>
<label class="tz-search" for="pq">{I('search')}<input id="pq" type="search" placeholder="Search by name or scan barcode"><span style="color:var(--ink-2)">{I('scan')}</span></label>
<div class="tz-chips"><button class="tz-chip" aria-pressed="true">All</button><button class="tz-chip" aria-pressed="false">Staples</button><button class="tz-chip" aria-pressed="false">Dairy</button><button class="tz-chip" aria-pressed="false">Oils</button><button class="tz-chip" aria-pressed="false">Snacks</button><button class="tz-chip" aria-pressed="false">Household</button><button class="tz-chip" aria-pressed="false">Personal care</button></div>
<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:var(--space-3)">{tl}</div>
</section>
<aside style="background:var(--surface);border-radius:var(--radius-lg);border:1px solid var(--line);padding:var(--space-4);display:flex;flex-direction:column;gap:var(--space-3)">
<div style="display:flex;justify-content:space-between;align-items:center"><h2 class="tz-h">Current sale</h2><button class="tz-btn tz-btn-ghost tz-btn-sm">Clear</button></div>
<div class="tz-search" style="min-height:44px">{I('user')}<input aria-label="Customer phone" value="98450 14521"></div>
<div style="font:500 13px/18px var(--font-sans);color:var(--ink-2)">Ananya R. · 6 visits · 420 points</div>
<div style="flex:1">{cartlines}<div style="display:flex;justify-content:space-between;padding:var(--space-3) 0;color:var(--ink-2);font:500 14px/20px var(--font-sans)"><span>Discount (round off)</span><span class="tz-num">−₹5</span></div></div>
<div class="tz-chips"><button class="tz-chip" aria-pressed="false">Cash</button><button class="tz-chip" aria-pressed="true">UPI</button><button class="tz-chip" aria-pressed="false">Card</button><button class="tz-chip" aria-pressed="false">Credit</button></div>
<div class="tz-pos-total"><div><div style="font:500 12px/16px var(--font-sans);color:var(--on-inverse-2)">11 items</div><div class="tz-amt">₹1,236</div></div><button class="tz-btn tz-btn-brand tz-btn-lg">Charge</button></div>
</aside></main></div>'''
os.makedirs(f'{ROOT}/components/BusinessPOS',exist_ok=True)
open(f'{ROOT}/components/BusinessPOS/preview.html','w').write(doc('<!-- @dsCard group="Screens" height=760 width=1280 -->','Business POS screen',pos))
open(f'{ROOT}/components/BusinessPOS/README.md','w').write('''# BusinessPOS

The desktop counter screen of the business app: sidebar, product grid with search, barcode and category chips, and the current sale.

- Tiles show price and live stock; low stock in `warning`, out of stock dimmed.
- The cart identifies the customer by phone so the sale lands in their history and earns points.
- On phone the grid becomes a search-first list and the cart opens as a bottom sheet with the same black total bar.
''')

aitems=[('chart','Overview'),('store','Businesses','14'),('users','Customers'),('receipt','Orders'),('msg','Support','12'),('g','Growth'),('gift','Points and referrals'),('box','Master catalogue'),('bell','Notifications'),('g','Access'),('shield','Admin users'),('cog','Settings')]
rows=[('Sri Lakshmi Provision Stores','Jayanagar · Grocery','Kavya S.','GSTIN 29ABCDE1234F1Z5','2 hr ago','pending'),('MedPlus Pharmacy','BTM Layout · Pharmacy','Rahul M.','Drug licence KA-B12-2234','5 hr ago','pending'),('Fresh Basket','HSR Layout · Fruits and vegetables','Imran K.','Udyam UDYAM-KR-03-0012','Yesterday','info'),('Sharma General Store','JP Nagar · Grocery','Deepak S.','GSTIN 29AAKPS7781K1Z2','2 days ago','pending')]
pill={'pending':'<span class="tz-pill tz-pill-pending">Pending review</span>','info':'<span class="tz-pill tz-pill-accepted">Needs info</span>'}
trs=''.join(f'<tr><td>{a}<span class="tz-sub">{b}</span></td><td>{c}</td><td>{d}</td><td>{e}</td><td>{pill[f]}</td><td class="num"><button class="tz-btn tz-btn-sm">Review</button></td></tr>' for a,b,c,d,e,f in rows)
admin=f'''<div style="display:flex;height:720px;background:var(--bg)">
{side(aitems,'Businesses','<span>Today<b>Z</b></span><span class="tz-side-tag">Admin</span>',None)}
<main style="flex:1;min-width:0;display:flex;flex-direction:column;gap:var(--space-5);padding:var(--space-6) var(--space-8)">
<div style="display:flex;justify-content:space-between;align-items:flex-end;gap:var(--space-3)"><div><h1 class="tz-page-title">Businesses</h1><p class="tz-page-sub">128 live · 14 awaiting review · SMS is off</p></div><div style="display:flex;gap:var(--space-2)"><button class="tz-btn tz-btn-sm">Export CSV</button><button class="tz-btn tz-btn-primary tz-btn-sm">{I('plus','tz-ico-sm')}Add business</button></div></div>
<div class="tz-banner tz-banner-warning">{I('warn')}<span><b>SMS is off.</b> OTP logins fall back to WhatsApp, which is also off. Add an SMS provider key in Settings before launch.</span></div>
<div style="display:grid;grid-template-columns:repeat(4,1fr);gap:var(--space-3)">
<div class="tz-kpi"><span class="tz-kpi-label">Live businesses</span><span class="tz-kpi-value">128</span><span class="tz-kpi-foot"><span class="tz-kpi-delta">▲ 9</span>this week</span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Awaiting review</span><span class="tz-kpi-value">14</span><span class="tz-kpi-foot"><span class="tz-kpi-delta tz-kpi-delta-flat">Oldest 2 days</span></span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Orders today</span><span class="tz-kpi-value">1,204</span><span class="tz-kpi-foot"><span class="tz-kpi-delta">▲ 18%</span>vs last Thu</span></div>
<div class="tz-kpi"><span class="tz-kpi-label">Open tickets</span><span class="tz-kpi-value">12</span><span class="tz-kpi-foot"><span class="tz-kpi-delta tz-kpi-delta-down">3 over 4 hr</span>unanswered</span></div>
</div>
<div class="tz-chips"><button class="tz-chip" aria-pressed="true">Awaiting review · 14</button><button class="tz-chip" aria-pressed="false">Live · 128</button><button class="tz-chip" aria-pressed="false">Needs info · 3</button><button class="tz-chip" aria-pressed="false">Suspended · 2</button></div>
<div class="tz-table-wrap"><table class="tz-table"><thead><tr><th>Business</th><th>Owner</th><th>Verification</th><th>Submitted</th><th>Status</th><th class="num"></th></tr></thead><tbody>{trs}</tbody></table></div>
</main></div>'''
os.makedirs(f'{ROOT}/components/AdminConsole',exist_ok=True)
open(f'{ROOT}/components/AdminConsole/preview.html','w').write(doc('<!-- @dsCard group="Screens" height=720 width=1280 -->','Admin console screen',admin))
open(f'{ROOT}/components/AdminConsole/README.md','w').write('''# AdminConsole

The admin console's Businesses screen: the same light sidebar as the business app with an Admin tag, role-gated sections, integration warning, KPIs, status chips and the verification queue.

- Review opens a right-hand drawer with documents, store photos and Approve, Ask for info, Reject.
- Admin users and Settings appear only for super admins.
''')


# ---------- Business overview dashboard ----------
hours=['8a','9a','10a','11a','12p','1p','2p','3p','4p','5p','6p','7p','8p','9p']
today=[1.2,2.6,3.1,2.4,3.8,4.6,2.9,2.2,3.0,5.2,6.4]          # thousands of rupees, 8am-6pm
lastw=[1.0,2.2,2.8,2.6,3.5,4.1,3.0,2.5,2.8,4.6,5.9,6.8,5.1,2.4]
W,H,L,T,B=600,210,36,12,28; ymax=8
x=lambda i: L+i*(W-L-8)/(len(hours)-1)
y=lambda v: T+(H-T-B)*(1-v/ymax)
grid=''.join(f'<line class="grid" x1="{L}" x2="{W}" y1="{y(v):.1f}" y2="{y(v):.1f}"/><text class="axis" x="{L-8}" y="{y(v)+4:.1f}" text-anchor="end">₹{v}k</text>' for v in [0,2,4,6,8])
xl=''.join(f'<text class="axis" x="{x(i):.1f}" y="{H-8}" text-anchor="middle">{h}</text>' for i,h in enumerate(hours) if i%2==0)
bw=22
bars=''.join(f'<rect class="{"bar-hl" if i==len(today)-1 else "bar-pale"}" x="{x(i)-bw/2:.1f}" y="{y(v):.1f}" width="{bw}" height="{y(0)-y(v):.1f}" rx="6"/>' for i,v in enumerate(today))
prev='M'+' L'.join(f'{x(i):.1f},{y(v):.1f}' for i,v in enumerate(lastw))
chart=f'<svg class="tz-chart" viewBox="0 0 {W} {H}" role="img" aria-label="Sales by hour today against last Thursday">{grid}{xl}{bars}<path class="line-prev" d="{prev}"/></svg>'
tip=f'<div class="tz-tip" style="left:{x(len(today)-1)/W*100-14:.1f}%;top:6px">6 pm, so far<b>₹6,400</b>Last Thu ₹5,900</div>'
oc=lambda id,who,items,amt,mode,t: f'<div class="tz-ocard"><div class="tz-ocard-top"><span>{id} · {who}</span><span class="tz-timer">{I("clock","tz-ico-sm")}{t}</span></div><div style="font:500 13px/18px var(--font-sans);color:var(--ink-2)">{items}</div><div style="display:flex;justify-content:space-between;align-items:center"><span class="tz-price">{amt}</span><span class="tz-pill tz-pill-pending">{mode}</span></div><div class="tz-ocard-actions"><button class="tz-btn tz-btn-sm">Reject</button><button class="tz-btn tz-btn-primary tz-btn-sm">Accept</button></div></div>'
act=lambda ic,cls,title,meta,tag: f'<div class="tz-activity"><span class="tz-kpi-icon {cls}">{I(ic,"tz-ico-sm")}</span><div>{title}<small>{meta}</small></div>{tag}</div>'
bitems2=[('chart','Overview'),('receipt','Orders','3'),('scan','Sell'),('box','Stock','5'),('users','Customers'),('g','Grow'),('tag','Offers'),('gift','Referrals and wallet'),('chart','Analytics'),('g','Shop'),('store','Stores and staff'),('msg','Help'),('cog','Settings')]
sidecard='<div class="tz-side-card"><b>Shop setup 80%</b><div class="tz-progress"><i style="width:80%"></i></div>Add opening hours to appear in “Open now” searches.<a href="#" style="color:var(--ink);font-weight:700;text-decoration:none">Finish setup</a></div>'
sidehtml=side(bitems2,'Overview','<span>Today<b>Z</b></span>',None,'').replace('</nav>',sidecard+'</nav>')
overview=f"""<div style="display:flex;min-height:900px;background:var(--bg)">
{sidehtml}
<main style="flex:1;min-width:0;display:flex;flex-direction:column;gap:var(--space-5);padding:var(--space-5) var(--space-6) var(--space-6)">
<div class="tz-topbar"><label class="tz-search" for="gs">{I('search')}<input id="gs" type="search" placeholder="Search products, orders, customers"><span class="tz-kbd">⌘K</span></label><span style="flex:1"></span><span class="tz-sync" role="status">{I('check','tz-ico-sm')}Synced 6:40 pm</span><button class="tz-btn tz-btn-icon" aria-label="Notifications">{I('bell')}</button><button class="tz-btn tz-btn-primary">{I('scan','tz-ico-sm')}New sale</button></div>
<div style="display:flex;justify-content:space-between;align-items:flex-end;gap:var(--space-3)"><div><h1 class="tz-page-title">Good evening, Kavya</h1><p class="tz-page-sub">Thursday, 22 October · Sri Lakshmi Provision Stores · 2 orders waiting</p></div><div style="display:flex;gap:var(--space-2);align-items:center"><div class="tz-seg"><button aria-pressed="true">Today</button><button aria-pressed="false">7 days</button><button aria-pressed="false">30 days</button></div><button class="tz-filter" data-set>vs <b>last Thursday</b></button></div></div>
<section class="tz-kpi-strip" aria-label="Today at a glance">
<div class="tz-kpi-cell tz-kpi-cell-lead"><div class="tz-kpi-cell-body"><span class="tz-kpi-name">{I('wallet','tz-ico-sm')}Sales today</span><span class="tz-kpi-value">₹48,260</span><span class="tz-kpi-note">142 bills · 37 online orders</span></div><div class="tz-kpi-cell-foot"><span class="tz-kpi-delta">▲ 12%</span>vs last Thursday</div></div>
<div class="tz-kpi-cell"><div class="tz-kpi-cell-body"><span class="tz-kpi-name">{I('receipt','tz-ico-sm')}Orders</span><span class="tz-kpi-value">37</span><span class="tz-kpi-note">2 waiting to accept</span></div><div class="tz-kpi-cell-foot"><span class="tz-kpi-delta">▲ 5</span>vs last Thu</div></div>
<div class="tz-kpi-cell"><div class="tz-kpi-cell-body"><span class="tz-kpi-name">{I('bag','tz-ico-sm')}Items sold</span><span class="tz-kpi-value">214</span><span class="tz-kpi-note">86 different items</span></div><div class="tz-kpi-cell-foot"><span class="tz-kpi-delta tz-kpi-delta-down">▼ 3%</span>vs last Thu</div></div>
<div class="tz-kpi-cell"><div class="tz-kpi-cell-body"><span class="tz-kpi-name">{I('box','tz-ico-sm')}Low stock</span><span class="tz-kpi-value">9</span><span class="tz-kpi-note">5 sell out by tomorrow</span></div><div class="tz-kpi-cell-foot"><a href="#">Reorder list →</a></div></div>
</section>
<div style="display:grid;grid-template-columns:1.7fr 1fr;gap:var(--space-4);align-items:start">
<section class="tz-card"><div class="tz-card-head"><h2 class="tz-card-title">Sales by hour</h2><div class="tz-legend"><span><i style="height:8px;width:8px;border-radius:2px"></i>Today</span><span><i class="prev"></i>Last Thursday</span></div></div><div style="position:relative">{chart}{tip}</div><div class="tz-card-foot"><a href="#">View sales report</a><span>Updated 6:40 pm</span></div></section>
<section class="tz-card"><div class="tz-card-head"><h2 class="tz-card-title">New orders</h2><span class="tz-pill tz-pill-pending">2 waiting</span></div>
{oc('TZ-24821','Ananya R.','Aashirvaad Atta 5 kg, Amul Taaza ×2, Maggi','₹401','Pickup','3:42')}
{oc('TZ-24822','Vikram P.','Fortune Oil 1 L, Tata Salt, Surf Excel','₹333','Delivery','4:58')}
</section>
</div>
<div style="display:grid;grid-template-columns:1fr 1.7fr;gap:var(--space-4);align-items:start">
<section class="tz-card"><div class="tz-card-head"><h2 class="tz-card-title">Needs your attention</h2></div><div>
{act('box','warning','5 items running low','Fortune Oil, Tata Salt and 3 more','<span class="tz-btn tz-btn-sm">Reorder</span>')}
{act('clock','','Stock not updated in 3 days','Snacks and household shelves','<span class="tz-btn tz-btn-sm">Update</span>')}
{act('users','info','12 customers have not visited in 30 days','Send them an offer','<span class="tz-btn tz-btn-sm">Create offer</span>')}
</div></section>
<section class="tz-card" style="padding:0;gap:0"><div class="tz-card-head" style="padding:var(--space-5) var(--space-5) var(--space-3)"><h2 class="tz-card-title">Top items today</h2><a href="#" style="color:var(--ink);font:600 14px/20px var(--font-sans);text-decoration:none">View all</a></div>
<table class="tz-table"><thead><tr><th>Item</th><th class="num">Sold</th><th>7 days</th><th class="num">Revenue</th><th class="num">In stock</th><th>Updated</th></tr></thead><tbody>
<tr><td>Amul Taaza Milk<span class="tz-sub">500 ml</span></td><td class="num">64</td><td><span class="tz-spark" aria-label="Sold last 7 days"><i style="height:75%"></i><i style="height:81%"></i><i style="height:78%"></i><i style="height:95%"></i><i style="height:86%"></i><i style="height:91%"></i><i style="height:100%"></i></span></td><td class="num">₹1,792</td><td class="num">36</td><td>POS · 6:38 pm</td></tr>
<tr><td>Aashirvaad Atta<span class="tz-sub">5 kg</span></td><td class="num">18</td><td><span class="tz-spark" aria-label="Sold last 7 days"><i style="height:100%"></i><i style="height:68%"></i><i style="height:86%"></i><i style="height:55%"></i><i style="height:91%"></i><i style="height:73%"></i><i style="height:82%"></i></span></td><td class="num">₹5,202</td><td class="num">24</td><td>Online · 6:21 pm</td></tr>
<tr><td>Fortune Sunflower Oil<span class="tz-sub">1 L</span></td><td class="num">14</td><td><span class="tz-spark" aria-label="Sold last 7 days"><i style="height:43%"></i><i style="height:57%"></i><i style="height:50%"></i><i style="height:64%"></i><i style="height:79%"></i><i style="height:86%"></i><i style="height:100%"></i></span></td><td class="num">₹2,310</td><td class="num"><span class="tz-stock tz-stock-low">3</span></td><td>Voice · 5:02 pm</td></tr>
</tbody><tfoot><tr><td>Total, 3 of 86 items</td><td class="num">96</td><td></td><td class="num">₹9,304</td><td class="num"></td><td></td></tr></tfoot></table></section>
</div>
</main></div>"""
os.makedirs(f'{ROOT}/components/BusinessOverview',exist_ok=True)
open(f'{ROOT}/components/BusinessOverview/preview.html','w').write(doc('<!-- @dsCard group="Screens" height=900 width=1280 -->','Business overview dashboard',overview))
open(f'{ROOT}/components/BusinessOverview/README.md','w').write("""# BusinessOverview

The business app's home on desktop: what happened today, what needs doing now, and what is selling.

- Light sidebar with a mint `wash-strong` active item and a `brand` icon, counts on items, and a setup-progress card at the bottom.
- Top bar: global search with ⌘K, notifications, and the primary action "New sale".
- The greeting sits in a mint `tz-greet` band (wash-strong fading to white) with the range control (Today, 7 days, 30 days) and the comparison ("vs last Thursday") on its right.
- Today's numbers sit in one KPI strip (see KpiTile): sales is the wider lead cell with a faint mint wash, each cell has a context line and a footer band with the comparison. The top bar shows the sync state ("Synced 6:40 pm").
- Sales by hour: today's bars, the current hour solid `brand` and earlier hours light grey; last week as a dotted `ink-3` line; black `inverse` tooltip.
- New orders show a countdown to accept and half-width Reject and Accept buttons.
- Demand insights (see DemandInsights) turns sales, stock and nearby searches into four tinted rows, each with one action. Top items show the source of the last stock change (POS, Online, Voice).
- On phone the same blocks stack in this order: KPI strip (two by two), new orders, demand insights, chart, top items.
""")
print('screens ok')

os.makedirs(f'{ROOT}/components/StoreMap',exist_ok=True)
open(f'{ROOT}/components/StoreMap/preview.html','w').write(doc('<!-- @dsCard group="Customer" height=400 width=420 -->','Store map',f'<div style="padding:24px;max-width:344px">{MAPCARD()}</div>'))
open(f'{ROOT}/components/StoreMap/README.md','w').write("""# StoreMap

The first thing on the customer home: every shop around you on one map, how many there are, how many are open, how many products they hold, and the radius you are searching.

- Map: green pins for open shops, grey for closed, a white ring with a number for a cluster, the customer as a blue dot with a halo, and the search radius as a dashed green ring with a 6% green fill.
- "Live near you" sits top left; the legend top right; the expand button bottom right opens the full-screen map with a shop list sheet.
- Counts under the map are tabular numbers in three hairline cells and change with the radius.
- Radius chips: 1, 2, 5, 10 and 20 km. The selected chip is black like every selected chip. Default 2 km; the choice is remembered.
- The apps draw the map with MapLibre GL and a TodayZ style built from the `map-*` tokens (land, park, water, road, road edge), over OpenStreetMap data. No Google-style colours.
- Tapping a pin opens that shop's card in a bottom sheet; tapping a cluster zooms in.
- The marketing site and the desktop storefront list use the same card at a wider size with the counts to the left of the map.
""")
os.makedirs(f'{ROOT}/components/CategoryTile',exist_ok=True)
open(f'{ROOT}/components/CategoryTile/preview.html','w').write(doc('<!-- @dsCard group="Customer" height=140 width=480 -->','Category tiles',f'<div style="padding:24px">{CATROW}</div>'))
open(f'{ROOT}/components/CategoryTile/README.md','w').write("""# CategoryTile

Shop categories on the customer home and in search filters, each in its own pastel tint.

- Grocery `green`, Pharmacy `rose`, Beauty `lilac`, Electronics `sky`, Stationery `amber`, Hardware `peach`. New categories reuse one of these six tints; never invent a new hue.
- Tile: 56px square, `radius-lg`, `tint-*` fill, 24px Lucide icon in the matching `tint-*-ink`. Label 12px, 500, `ink`, under the tile.
- A shop's letter avatar takes the tint of its main category (`.tz-avatar[data-tint]`), so a pharmacy reads rose and a kirana green across the app.
- Set the tint with `data-tint="green"` etc. on the element; children read `--t-bg` and `--t-ink`.
""")
