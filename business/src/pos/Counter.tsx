// B-05 Sell: the counter. Search or scan to fill the bill, take the payment, record the sale on this
// device first and let the sync engine deliver it. Nothing here waits for the network to ring a sale.
import { newUlid, useHoldUpdates } from '@todayz/auth';
import { formatMoney } from '@todayz/i18n';
import { Banner, Button, CartBar, EmptyState, PosCart, SearchField, Sheet, Skeleton } from '@todayz/ui';
import { Link } from '@tanstack/react-router';
import { History, ListOrdered, PackagePlus, ScanLine, Trash2, UserRoundPlus, X } from 'lucide-react';
import { useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { useIntl } from 'react-intl';
import { useUpdateController } from '../app/update-context';
import { ReceiptSheet } from '../receipt/ReceiptSheet';
import { CameraScanner } from '../scanner/CameraScanner';
import { cameraScanSupported } from '../scanner/support';
import { looksLikeBarcode, useWedgeScanner } from '../scanner/wedge';
import { useNarrow } from '../shell/useNarrow';
import { AddProductSheet } from './AddProductSheet';
import { cartReducer, cartTotals, emptyCart, firstShortLine, type CartAction, type CartLine } from './cart';
import { blockedUnits, findByBarcode, searchItems, sellableUnits, sellPricePaise, topTiles } from './catalogue';
import { buildSale, creditsFor } from './checkout';
import { usePos } from './context';
import { DiscountSheet } from './DiscountSheet';
import { needsAttention } from './history';
import { LinkPanel } from './LinkPanel';
import { amountDue, creditsAllowed, creditsToPaise, lineTotal } from './money';
import { SalesSheet } from './SalesSheet';
import { UpdateStockDialog, type StockAskReason } from './UpdateStockDialog';
import type { CatalogueItem, PaymentMethod, SaleLink } from './types';
import styles from './pos.module.css';

const money = (p: number) => formatMoney(p, 'product');
const signed = (p: number) => `${p < 0 ? '−' : '+'}${money(Math.abs(p))}`;
const PAYMENTS: readonly PaymentMethod[] = ['cash', 'upi', 'card'];
type Sub = 'link' | 'discount' | 'sales' | 'camera' | 'add';

function isEditable(el: Element | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  return el.isContentEditable || el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT';
}

function maskPhone(phone: string): string {
  return `•••• ${phone.slice(-4)}`;
}

/** A count the counter asks for before the bill may hold more of an item (D-270). */
interface StockAsk {
  item: CatalogueItem;
  lineId: string;
  wanted: number;
  reason: StockAskReason;
}

export function Counter() {
  const intl = useIntl();
  const { store, engine, items, catalogueLoaded, settings, sync, online, durable, records, reloadRecords, now } = usePos();
  const narrow = useNarrow();
  const updates = useUpdateController();
  const [cart, dispatch] = useReducer(cartReducer, emptyCart);
  // One id per bill: the bill QR claim and a verified counter code are tied to it (D-12).
  const [saleId, setSaleId] = useState(() => newUlid());
  const [query, setQuery] = useState('');
  const [phone, setPhone] = useState('');
  const [sub, setSub] = useState<Sub | null>(null);
  const [cartOpen, setCartOpen] = useState(false);
  const [unknownCode, setUnknownCode] = useState<string | null>(null);
  const [linkDropped, setLinkDropped] = useState(false);
  const [active, setActive] = useState<string | null>(null);
  const [charging, setCharging] = useState(false);
  const [chargeFailed, setChargeFailed] = useState(false);
  const [lastSaleId, setLastSaleId] = useState<string | null>(null);
  const [receiptId, setReceiptId] = useState<string | null>(null);
  const [stockAsk, setStockAsk] = useState<StockAsk | null>(null);
  const searchBox = useRef<HTMLDivElement>(null);

  const canManage = store.role === 'owner' || store.role === 'manager';
  const totals = cartTotals(cart);
  const credits = creditsFor(cart, settings);
  const due = amountDue(totals.totalPaise, creditsToPaise(credits));
  const hasBill = cart.lines.length > 0;

  useHoldUpdates(updates, hasBill || charging);

  const results = useMemo(() => searchItems(items, query), [items, query]);
  const byId = useMemo(() => new Map(items.map((i) => [i.id, i])), [items]);
  // An online sale the server found short (D-270) asks for a count too; nothing in the bill waits on it.
  const soldShort = sync.short ? byId.get(sync.short.storeItemId) : undefined;
  const ask: StockAsk | null = stockAsk ?? (soldShort ? { item: soldShort, lineId: '', wanted: 0, reason: 'sold' } : null);
  const tiles = useMemo(() => topTiles(items), [items]);
  const lastRec = records.find((r) => r.sale.clientId === lastSaleId) ?? null;
  const receiptRec = records.find((r) => r.sale.clientId === receiptId) ?? null;
  const attention = records.filter(needsAttention).length;

  // Every change goes through here so a bill QR link never outlives the amount it was quoted for.
  const act = (a: CartAction) => {
    dispatch(a);
    if (cart.link?.method === 'bill_qr' && a.type !== 'link' && a.type !== 'clear' && a.type !== 'payment') {
      if (cartTotals(cartReducer(cart, a)).totalPaise !== totals.totalPaise) {
        dispatch({ type: 'link', link: null });
        setLinkDropped(true);
      }
    }
  };

  // The bill never holds more of an item than is available (D-270): past it, ask for a count first.
  const addItem = (picked: CatalogueItem) => {
    const item = byId.get(picked.id) ?? picked;
    const existing = cart.lines.find((l) => l.storeItemId === item.id);
    const lineId = existing?.id ?? newUlid();
    setQuery('');
    setLastSaleId(null);
    const wanted = (existing?.quantity ?? 0) + 1;
    if (wanted > sellableUnits(item)) return setStockAsk({ item, lineId, wanted, reason: 'add' });
    act({ type: 'add', item, lineId });
    setActive(lineId);
  };

  /** Moves a line to `quantity`; raising it past the item's available asks for a count instead. */
  const setLineQuantity = (lineId: string, quantity: number) => {
    setActive(lineId);
    const line = cart.lines.find((l) => l.id === lineId);
    const item = line?.storeItemId ? byId.get(line.storeItemId) : undefined;
    if (quantity <= 0) return act({ type: 'remove', lineId });
    if (!line || !item || quantity <= line.quantity) return act({ type: 'setQuantity', lineId, quantity });
    if (quantity > sellableUnits(item)) return setStockAsk({ item, lineId, wanted: quantity, reason: 'add' });
    act({ type: 'fit', item, lineId, quantity });
  };

  const closeStock = () => {
    if (stockAsk) setStockAsk(null);
    else engine.clearShort();
  };

  const onCounted = (item: CatalogueItem) => {
    if (ask && ask.reason !== 'sold' && ask.wanted > 0) {
      act({ type: 'fit', item, lineId: ask.lineId, quantity: ask.wanted });
      setActive(ask.lineId);
    }
    closeStock();
  };

  const onCode = (code: string) => {
    const hit = findByBarcode(items, code);
    if (hit) return addItem(hit);
    setUnknownCode(code);
    setSub('add');
  };

  const addCustom = (name: string, unitPricePaise: number) => {
    const lineId = newUlid();
    act({ type: 'addCustom', lineId, name, unitPricePaise });
    setActive(lineId);
    setQuery('');
    setLastSaleId(null);
  };

  const onSearchKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return;
    e.preventDefault();
    const q = query.trim();
    if (!q) return;
    const byCode = findByBarcode(items, q);
    const first = byCode ?? results[0];
    if (first) addItem(first);
    else if (looksLikeBarcode(q)) onCode(q);
  };

  useWedgeScanner(onCode, sub === null && receiptId === null && !cartOpen);

  const resetBill = () => {
    dispatch({ type: 'clear' });
    setPhone('');
    setLinkDropped(false);
    setActive(null);
    setSaleId(newUlid());
  };

  const charge = async () => {
    if (charging || !hasBill) return;
    // A sync or an online order may have lowered an item since it was added: count before charging.
    const short = firstShortLine(cart, (id) => byId.get(id)?.available ?? null);
    const shortItem = short?.storeItemId ? byId.get(short.storeItemId) : undefined;
    if (short && shortItem) return setStockAsk({ item: shortItem, lineId: short.id, wanted: short.quantity, reason: 'charge' });
    setCharging(true);
    setChargeFailed(false);
    try {
      const sale = buildSale(cart, settings, saleId, new Date(now()).toISOString());
      await engine.recordSale(sale);
      resetBill();
      setCartOpen(false);
      setLastSaleId(saleId);
      // The sale is already safe on this device; delivery continues in the background.
      if (online) void engine.sendSaleNow(saleId).then(reloadRecords, () => undefined);
      else void reloadRecords();
    } catch {
      setChargeFailed(true);
    } finally {
      setCharging(false);
    }
  };

  // Shortcuts: F2 search, F4 charge, + and − on the line last touched.
  const keys = useRef<(e: KeyboardEvent) => void>(() => undefined);
  useEffect(() => {
    keys.current = (e) => {
      if (e.key === 'F2') {
        e.preventDefault();
        searchBox.current?.querySelector('input')?.focus();
      } else if (e.key === 'F4') {
        e.preventDefault();
        void charge();
      } else if ((e.key === '+' || e.key === '-') && !isEditable(document.activeElement) && sub === null && active) {
        e.preventDefault();
        const line = cart.lines.find((l) => l.id === active);
        if (line) setLineQuantity(active, line.quantity + (e.key === '+' ? 1 : -1));
      }
    };
  });
  useEffect(() => {
    const h = (e: KeyboardEvent) => keys.current(e);
    document.addEventListener('keydown', h);
    return () => document.removeEventListener('keydown', h);
  }, []);

  const openSub = (s: Sub) => {
    setCartOpen(false);
    setSub(s);
  };
  const closeSub = () => {
    setSub(null);
    setUnknownCode(null);
    if (narrow && hasBill) setCartOpen(true);
  };

  // Never below 0: an offline race reads "Count needed" (D-270).
  const stockLabel = (i: CatalogueItem) =>
    i.countNeeded || i.available < 0
      ? intl.formatMessage({ id: 'pos.tile.countNeeded' })
      : i.available > 0
        ? intl.formatMessage({ id: 'pos.tile.inStock' }, { count: i.available })
        : intl.formatMessage({ id: 'pos.tile.out' });
  const blockedLabel = (i: CatalogueItem) => {
    const n = blockedUnits(i);
    return n > 0 ? intl.formatMessage({ id: 'pos.tile.blocked' }, { count: n }) : null;
  };

  const linkSummary = (l: SaleLink): string => {
    if (l.method === 'bill_qr') return intl.formatMessage({ id: 'pos.cart.link.billQr' }, { name: l.customerName ?? '', credits: credits });
    if (l.method === 'counter_code') return intl.formatMessage({ id: 'pos.cart.link.counterCode' }, { phone: maskPhone(l.phone) });
    return intl.formatMessage({ id: 'pos.cart.link.unverified' }, { phone: maskPhone(l.phone) });
  };

  const lineDetail = (l: CartLine) =>
    l.discountPaise > 0
      ? intl.formatMessage({ id: 'pos.cart.lineDetailDiscount' }, { price: money(l.unitPricePaise), discount: money(l.discountPaise) })
      : money(l.unitPricePaise);

  const cartNode = (
    <div className={styles.cartSheet}>
      <PosCart
        label={intl.formatMessage({ id: 'pos.cart.label' })}
        customer={{
          label: intl.formatMessage({ id: 'pos.cart.phone' }),
          placeholder: intl.formatMessage({ id: 'pos.cart.phonePlaceholder' }),
          phone,
          onPhoneChange: setPhone,
          ...(cart.link ? { summary: linkSummary(cart.link) } : {}),
        }}
        lines={cart.lines.map((l) => ({ id: l.id, name: l.name, detail: lineDetail(l), quantity: l.quantity, total: money(lineTotal(l)) }))}
        onQuantityChange={setLineQuantity}
        stepperLabels={{
          add: intl.formatMessage({ id: 'pos.cart.add' }),
          decrement: intl.formatMessage({ id: 'pos.cart.less' }),
          increment: intl.formatMessage({ id: 'pos.cart.more' }),
        }}
        emptyLabel={intl.formatMessage({ id: 'pos.cart.empty' })}
        payment={{
          label: intl.formatMessage({ id: 'pos.pay.label' }),
          options: PAYMENTS.map((m) => ({ value: m, label: intl.formatMessage({ id: `pos.pay.${m}` }) })),
          value: cart.paymentMethod,
          onChange: (v) => act({ type: 'payment', method: v as PaymentMethod }),
        }}
        summary={intl.formatMessage({ id: 'pos.cart.summary' }, { count: totals.itemCount })}
        amount={money(due)}
        chargeLabel={intl.formatMessage({ id: 'pos.cart.charge' })}
        chargingLabel={intl.formatMessage({ id: 'pos.cart.charging' })}
        onCharge={() => void charge()}
        charging={charging}
      />
      {(totals.billDiscountPaise > 0 || totals.roundOffPaise !== 0 || credits > 0) && (
        <div className={styles.totals}>
          <div className={styles.totalsRow}>
            <span>{intl.formatMessage({ id: 'pos.cart.subtotal' })}</span>
            <span className="tz-num">{money(totals.subtotalPaise)}</span>
          </div>
          {totals.billDiscountPaise > 0 && (
            <div className={styles.totalsRow}>
              <span>{intl.formatMessage({ id: 'pos.cart.billDiscount' })}</span>
              <span className="tz-num">−{money(totals.billDiscountPaise)}</span>
            </div>
          )}
          {totals.roundOffPaise !== 0 && (
            <div className={styles.totalsRow}>
              <span>{intl.formatMessage({ id: 'pos.cart.roundOff' })}</span>
              <span className="tz-num">{signed(totals.roundOffPaise)}</span>
            </div>
          )}
          {credits > 0 && (
            <div className={styles.totalsRow}>
              <span>{intl.formatMessage({ id: 'pos.cart.credits' })}</span>
              <span className="tz-num">−{money(creditsToPaise(credits))}</span>
            </div>
          )}
        </div>
      )}
      {cart.link && (
        <div className={styles.linkRow}>
          <span>{linkSummary(cart.link)}</span>
          <Button
            variant="ghost"
            size="sm"
            icon={X}
            onClick={() => act({ type: 'link', link: null })}
          >
            {intl.formatMessage({ id: 'pos.cart.unlink' })}
          </Button>
        </div>
      )}
      <div className={styles.saleActions}>
        {!cart.link && (
          <Button variant="soft" icon={UserRoundPlus} disabled={!hasBill} onClick={() => openSub('link')}>
            {intl.formatMessage({ id: 'pos.cart.linkCustomer' })}
          </Button>
        )}
        {canManage && (
          <Button variant="soft" disabled={!hasBill} onClick={() => openSub('discount')}>
            {intl.formatMessage({ id: 'pos.cart.discount' })}
          </Button>
        )}
        <Button variant="ghost" icon={Trash2} disabled={!hasBill} onClick={resetBill}>
          {intl.formatMessage({ id: 'pos.cart.clear' })}
        </Button>
      </div>
    </div>
  );

  return (
    <div className={styles.page}>
      <div className={styles.head}>
        <h1 className="tz-page-title">{intl.formatMessage({ id: 'pos.title' })}</h1>
        <div className={styles.headActions}>
          <Button variant="soft" icon={History} onClick={() => openSub('sales')}>
            {attention > 0 ? intl.formatMessage({ id: 'pos.sales.openAttention' }, { count: attention }) : intl.formatMessage({ id: 'pos.sales.open' })}
          </Button>
          {/* The store's sales from every device live on Orders → Counter sales (D-268). */}
          <Link to="/orders" search={{ view: 'sales' }} className="tz-btn tz-btn-soft">
            <ListOrdered className="tz-ico tz-ico-sm" aria-hidden="true" />
            {intl.formatMessage({ id: 'pos.viewSales' })}
          </Link>
        </div>
      </div>

      {!durable && <Banner tone="warning" title={intl.formatMessage({ id: 'pos.banner.notDurable.title' })}>{intl.formatMessage({ id: 'pos.banner.notDurable.body' })}</Banner>}
      {sync.rejected > 0 && (
        <Banner
          tone="warning"
          title={intl.formatMessage({ id: 'pos.banner.rejected.title' }, { count: sync.rejected })}
          action={
            <Button size="sm" variant="soft" onClick={() => openSub('sales')}>
              {intl.formatMessage({ id: 'pos.banner.rejected.action' })}
            </Button>
          }
        />
      )}
      {(!online || sync.pending > 0) && (
        <Banner tone={online ? 'info' : 'warning'} live="status" title={intl.formatMessage({ id: online ? 'pos.banner.waiting.title' : 'pos.banner.offline.title' }, { count: sync.pending })}>
          {intl.formatMessage({ id: online ? 'pos.banner.waiting.body' : 'pos.banner.offline.body' })}
        </Banner>
      )}
      {linkDropped && (
        <Banner tone="info" live="status" title={intl.formatMessage({ id: 'pos.banner.linkDropped.title' })} action={<Button size="sm" variant="ghost" onClick={() => setLinkDropped(false)}>{intl.formatMessage({ id: 'pos.close' })}</Button>}>
          {intl.formatMessage({ id: 'pos.banner.linkDropped.body' })}
        </Banner>
      )}
      {chargeFailed && (
        <Banner tone="warning" live="status" title={intl.formatMessage({ id: 'pos.charge.failed.title' })}>
          {intl.formatMessage({ id: 'pos.charge.failed.body' })}
        </Banner>
      )}
      {lastRec && !hasBill && (
        <Banner
          tone={lastRec.state === 'rejected' ? 'warning' : 'info'}
          live="status"
          title={intl.formatMessage({ id: `pos.done.${lastRec.state === 'synced' ? 'synced' : lastRec.state === 'rejected' ? 'rejected' : 'waiting'}` }, { total: money(lastRec.serverTotalPaise ?? lastRec.sale.totalPaise) })}
          action={
            <Button size="sm" variant="soft" onClick={() => setReceiptId(lastRec.sale.clientId)}>
              {intl.formatMessage({ id: 'pos.done.receipt' })}
            </Button>
          }
        />
      )}

      <div className={styles.counter}>
        <section className={styles.browse} aria-label={intl.formatMessage({ id: 'pos.browse.label' })}>
          <div className={styles.searchRow}>
            <div ref={searchBox}>
              <SearchField
                label={intl.formatMessage({ id: 'pos.search.label' })}
                placeholder={intl.formatMessage({ id: 'pos.search.placeholder' })}
                value={query}
                onChange={setQuery}
                onKeyDown={onSearchKey}
                clearLabel={intl.formatMessage({ id: 'pos.search.clear' })}
              />
            </div>
            {cameraScanSupported() && (
              <Button variant="soft" icon={ScanLine} onClick={() => openSub('camera')}>
                {intl.formatMessage({ id: 'pos.scan.open' })}
              </Button>
            )}
            <Button variant="soft" icon={PackagePlus} onClick={() => openSub('add')}>
              {intl.formatMessage({ id: 'pos.add.open' })}
            </Button>
          </div>

          {!catalogueLoaded && (
            <div role="status" aria-label={intl.formatMessage({ id: 'pos.loading' })}>
              <Skeleton height="8rem" />
            </div>
          )}
          {catalogueLoaded && items.length === 0 && (
            <EmptyState title={intl.formatMessage({ id: 'pos.catalogue.empty.title' })} description={intl.formatMessage({ id: 'pos.catalogue.empty.body' })} />
          )}
          {catalogueLoaded && items.length > 0 && query.trim() === '' && (
            <>
              <h2 className="tz-sub">{intl.formatMessage({ id: 'pos.tiles.title' })}</h2>
              <ul className={styles.tiles}>
                {tiles.map((i) => (
                  <li key={i.id}>
                    <ItemTile item={i} stock={stockLabel(i)} blocked={blockedLabel(i)} onAdd={addItem} />
                  </li>
                ))}
              </ul>
            </>
          )}
          {catalogueLoaded && items.length > 0 && query.trim() !== '' && (
            <>
              {results.length > 0 ? (
                <ul className={styles.tiles}>
                  {results.map((i) => (
                    <li key={i.id}>
                      <ItemTile item={i} stock={stockLabel(i)} blocked={blockedLabel(i)} onAdd={addItem} />
                    </li>
                  ))}
                </ul>
              ) : (
                <EmptyState
                  title={intl.formatMessage({ id: 'pos.search.none.title' }, { query: query.trim() })}
                  description={intl.formatMessage({ id: 'pos.search.none.body' })}
                  action={
                    <Button
                      variant="soft"
                      icon={PackagePlus}
                      onClick={() => {
                        setUnknownCode(looksLikeBarcode(query) ? query.trim() : null);
                        openSub('add');
                      }}
                    >
                      {intl.formatMessage({ id: 'pos.add.open' })}
                    </Button>
                  }
                />
              )}
            </>
          )}
        </section>

        {!narrow && <aside className={styles.cart}>{cartNode}</aside>}
      </div>

      {narrow && hasBill && (
        <div className={styles.cartBar}>
          <CartBar
            count={totals.itemCount}
            countLabel={intl.formatMessage({ id: 'pos.cart.summary' }, { count: totals.itemCount })}
            total={money(due)}
            ctaLabel={intl.formatMessage({ id: 'pos.cart.view' })}
            onOpen={() => setCartOpen(true)}
          />
        </div>
      )}
      {narrow && (
        <Sheet open={cartOpen} onClose={() => setCartOpen(false)} title={intl.formatMessage({ id: 'pos.cart.label' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
          {cartNode}
        </Sheet>
      )}

      <AddProductSheet
        open={sub === 'add'}
        onClose={closeSub}
        barcode={unknownCode}
        onAdd={(name, price) => {
          addCustom(name, price);
        }}
      />
      {canManage && <DiscountSheet open={sub === 'discount'} onClose={closeSub} cart={cart} dispatch={act} />}
      <LinkPanel
        open={sub === 'link'}
        onClose={closeSub}
        saleClientId={saleId}
        amountPaise={totals.totalPaise}
        creditsAllowed={creditsAllowed(totals.netPaise, settings.creditMinOrderValuePaise)}
        phone={phone}
        onLink={(link) => {
          dispatch({ type: 'link', link });
          setLinkDropped(false);
        }}
      />
      <Sheet open={sub === 'camera'} onClose={closeSub} title={intl.formatMessage({ id: 'pos.scan.title' })} closeLabel={intl.formatMessage({ id: 'pos.close' })}>
        <CameraScanner
          labels={{
            unsupported: intl.formatMessage({ id: 'pos.scan.unsupported' }),
            denied: intl.formatMessage({ id: 'pos.scan.denied' }),
            starting: intl.formatMessage({ id: 'pos.scan.starting' }),
            hint: intl.formatMessage({ id: 'pos.scan.hint' }),
            close: intl.formatMessage({ id: 'pos.close' }),
          }}
          onDetect={(code) => {
            setSub(null);
            onCode(code);
          }}
          onClose={closeSub}
        />
      </Sheet>
      <SalesSheet
        open={sub === 'sales'}
        onClose={closeSub}
        canReverseSales={canManage}
        onReceipt={(rec) => {
          setSub(null);
          setReceiptId(rec.sale.clientId);
        }}
      />
      <UpdateStockDialog item={ask?.item ?? null} wanted={ask?.wanted ?? 0} reason={ask?.reason ?? 'add'} onCancel={closeStock} onSaved={onCounted} />
      <ReceiptSheet open={receiptRec !== null} onClose={() => setReceiptId(null)} shopName={store.name} record={receiptRec} />
    </div>
  );
}

function ItemTile({ item, stock, blocked, onAdd }: { item: CatalogueItem; stock: string; blocked: string | null; onAdd: (i: CatalogueItem) => void }) {
  const price = sellPricePaise(item);
  const out = item.available <= 0;
  const warn = item.countNeeded === true || item.available < 0;
  return (
    <button type="button" className={`tz-pos-tile ${out ? styles.tileDim : ''}`} onClick={() => onAdd(item)}>
      <span>
        {item.name}
        {item.variant ? <small className={styles.tileVariant}>{item.variant}</small> : null}
      </span>
      <span className={styles.tileMeta}>
        <span className="tz-price">{money(price)}</span>
        <small className={warn ? styles.tileWarn : undefined}>{stock}</small>
      </span>
      {blocked && <small className={styles.tileBlocked}>{blocked}</small>}
    </button>
  );
}
