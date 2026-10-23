import { TYPE, parse } from '@formatjs/icu-messageformat-parser';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { enHash, extractMessageIds, extractMessageRefs, lintApp } from '../scripts/catalogue.mjs';
import { compile, loadApp, loadLocaleMessages } from '../scripts/gen.mjs';
import { approve, importDrafts, todo } from '../scripts/translations.mjs';
import { PSEUDO_GROWTH, pseudoAst } from '../src/pseudo.mjs';

const pkg = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const realMessages = join(pkg, 'messages');

let dir: string;
let counter = 0;
beforeAll(() => {
  dir = mkdtempSync(join(tmpdir(), 'todayz-i18n-'));
});
afterAll(() => rmSync(dir, { recursive: true, force: true }));

type Entry = string | { defaultMessage: string };
/** Makes <dir>/m<n>/shop/{area.en.json, area.<lc>.json, ...} and returns the messages dir. */
function fixture(files: Record<string, unknown>): string {
  const root = join(dir, `m${counter++}`);
  mkdirSync(join(root, 'shop'), { recursive: true });
  for (const [name, content] of Object.entries(files)) writeFileSync(join(root, 'shop', name), JSON.stringify(content));
  return root;
}
const en = (msgs: Record<string, string>): Record<string, Entry> =>
  Object.fromEntries(Object.entries(msgs).map(([k, v]) => [k, { defaultMessage: v }]));
const tr = (msgs: Record<string, [string, 'machine' | 'reviewed', string?]>) =>
  Object.fromEntries(Object.entries(msgs).map(([k, [m, review, src]]) => [k, { message: m, review, enHash: enHash(src ?? m) }]));

describe('SC-I18N-01 pseudo-locale en-XA on every real catalogue', () => {
  it.each(readdirSync(realMessages))('%s: every key is accented, bracketed and at least 40% longer', async (app) => {
    const messages = await loadApp(app);
    const xa = compile(messages, { pseudo: true });
    for (const [key, msg] of Object.entries(messages)) {
      const plain = (a: ReturnType<typeof parse>): string =>
        a.map((el) => (el.type === TYPE.literal ? el.value : el.type === TYPE.tag ? plain(el.children) : 'xxxx')).join('');
      const out = plain(xa[key] ?? []);
      expect(out.startsWith('['), key).toBe(true);
      expect(out.length, key).toBeGreaterThanOrEqual(Math.ceil(plain(parse(msg)).length * PSEUDO_GROWTH));
    }
  });

  it('keeps plural options and tags', () => {
    const ast = pseudoAst(parse('{n, plural, one {# item} other {# items}} <b>x</b>'));
    expect(ast.some((e) => e.type === TYPE.plural)).toBe(true);
    expect(ast.some((e) => e.type === TYPE.tag)).toBe(true);
  });
});

describe('SC-I18N-05 missing-key lint', () => {
  it('extracts literal ids from formatMessage and FormattedMessage', () => {
    const src = `
      intl.formatMessage({ id: 'shop.title' });
      intl.formatMessage({ id: "shop.cart", defaultMessage: 'x' }, { n: 1 });
      <FormattedMessage id="shop.empty" />
      <FormattedMessage id='shop.other' values={{ a: 1 }} />
      const x = foo.id('not.a.key');
    `;
    expect([...extractMessageIds(src, 'x.tsx')].sort()).toEqual(['shop.cart', 'shop.empty', 'shop.other', 'shop.title']);
  });

  it('fails when code uses an id that English does not define', async () => {
    const messagesDir = fixture({ 'a.en.json': en({ 'shop.title': 'Shop' }) });
    const src = join(dir, `src${counter++}`);
    mkdirSync(src);
    writeFileSync(join(src, 'x.tsx'), `intl.formatMessage({ id: 'shop.title' }); intl.formatMessage({ id: 'shop.typo' });`);
    const r = await lintApp({ messagesDir, app: 'shop', srcDirs: [src] });
    expect(r.errors).toHaveLength(1);
    expect(r.errors[0]).toContain('missing key shop.typo');
  });

  it('passes when every used id exists, and only warns about unused ones', async () => {
    const messagesDir = fixture({ 'a.en.json': en({ 'shop.title': 'Shop', 'shop.unused': 'Unused' }) });
    const src = join(dir, `src${counter++}`);
    mkdirSync(src);
    writeFileSync(join(src, 'x.ts'), `intl.formatMessage({ id: 'shop.title' });`);
    const r = await lintApp({ messagesDir, app: 'shop', srcDirs: [src] });
    expect(r.errors).toEqual([]);
    expect(r.warnings.some((w) => w.includes('shop.unused'))).toBe(true);
  });

  /** A fixture source folder with the given files. */
  function srcOf(files: Record<string, string>): string {
    const src = join(dir, `src${counter++}`);
    mkdirSync(src, { recursive: true });
    for (const [name, code] of Object.entries(files)) writeFileSync(join(src, name), code);
    return src;
  }
  const messagesDirFor = (keys: string[]) => fixture({ 'a.en.json': en(Object.fromEntries(keys.map((k) => [k, 'x']))) });

  it('never passes by looking at nothing: a missing or empty source folder is an error', async () => {
    const messagesDir = messagesDirFor(['shop.title']);
    const missing = await lintApp({ messagesDir, app: 'shop', srcDirs: [join(dir, 'does-not-exist')] });
    expect(missing.errors.join('\n')).toContain('does not exist');
    const empty = await lintApp({ messagesDir, app: 'shop', srcDirs: [srcOf({ 'readme.txt': 'nothing' })] });
    expect(empty.errors.join('\n')).toContain('holds no source files');
    const none = await lintApp({ messagesDir, app: 'shop', srcDirs: [] });
    expect(none.errors.join('\n')).toContain('no source folder');
  });

  it('skips test files, generated files and node_modules', async () => {
    const messagesDir = messagesDirFor(['shop.title']);
    const src = srcOf({ 'ok.ts': `t('shop.title')`, 'a.test.ts': `t('shop.ghost')`, 'routeTree.gen.ts': `t('shop.ghost2')` });
    mkdirSync(join(src, 'node_modules'));
    writeFileSync(join(src, 'node_modules', 'x.ts'), `t('shop.ghost3')`);
    expect((await lintApp({ messagesDir, app: 'shop', srcDirs: [src] })).errors).toEqual([]);
  });

  describe('lint holes (one fixture each)', () => {
    const lintCode = async (name: string, code: string) => {
      const messagesDir = messagesDirFor(['shop.title']);
      return (await lintApp({ messagesDir, app: 'shop', srcDirs: [srcOf({ [name]: code })] })).errors;
    };

    it('defineMessages({ ... })', async () => {
      const errors = await lintCode('x.ts', `const m = defineMessages({ a: { id: 'shop.title' }, b: { id: 'shop.nope', defaultMessage: 'x' } });`);
      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('missing key shop.nope');
    });

    it("t('shop.x') helper calls (also in .astro)", async () => {
      expect(await lintCode('x.tsx', `const a = t('shop.title'); const b = t('shop.nope');`)).toHaveLength(1);
      const astro = await lintCode('p.astro', `---\nconst l = t('shop.nope2');\n---\n<h1>{t('shop.title')}</h1>`);
      expect(astro).toHaveLength(1);
      expect(astro[0]).toContain('shop.nope2');
    });

    it("a key-shaped string held in data: label: 'shop.nav.serch'", async () => {
      const errors = await lintCode('nav.ts', `export const items = [{ id: 1, label: 'shop.nav.serch' }, { id: 2, label: 'shop.title' }];`);
      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('shop.nav.serch');
    });

    it('key-shaped candidates have a low false-positive rate', async () => {
      const code = `
        import x from 'shop.css';
        import y from './shop.module.css';
        export * from 'shop.config';
        const a = 'shop.png';        // file extension
        const b = 'example.com';     // namespace not in the catalogue
        const c = 'Shop.Title';      // first segment differs in case
        const d = 'a shop.title b';  // not key-shaped (spaces)
        const e = 'v1.2.3';
        type T = 'shop.literal.type';
      `;
      expect(await lintCode('fp.ts', code)).toEqual([]);
    });

    it('<FormattedMessage values={{ b: (c) => <b>{c}</b> }} id="x"> with the id after nested braces', async () => {
      const errors = await lintCode(
        'x.tsx',
        `export const A = () => <FormattedMessage values={{ b: (c) => <b>{c}</b>, n: { a: 1 } }} id="shop.nope" />;
         export const B = () => <FormattedMessage values={{ b: (c) => <b>{c}</b> }} id="shop.title" />;`,
      );
      expect(errors).toHaveLength(1);
      expect(errors[0]).toContain('shop.nope');
    });

    it('extractor unit: explicit ids vs candidates', () => {
      const ns = new Set(['shop']);
      const { explicit, candidates } = extractMessageRefs(`t('shop.a'); const x = { l: 'shop.b.c' }; foo('other.d');`, 'x.ts', ns);
      expect([...explicit]).toEqual(['shop.a']);
      expect([...candidates].sort()).toEqual(['shop.a', 'shop.b.c']);
    });
  });

  it('the CLI fails without --src and passes with a real folder', () => {
    const script = join(pkg, 'scripts', 'lint.mjs');
    const bare = spawnSync('node', [script], { encoding: 'utf8' });
    expect(bare.status).toBe(1);
    expect(bare.stderr).toContain('no --src');
    const empty = join(dir, `empty${counter++}`);
    mkdirSync(empty);
    const apps = readdirSync(realMessages).flatMap((a) => ['--src', `${a}=${empty}`]);
    const r = spawnSync('node', [script, ...apps], { encoding: 'utf8' });
    expect(r.status).toBe(1);
    expect(r.stderr).toContain('holds no source files');
  });

  it('rejects an English message that does not parse', async () => {
    const messagesDir = fixture({ 'a.en.json': en({ 'shop.bad': 'Hello {name' }) });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors[0]).toContain('shop.bad');
  });

  it('rejects a duplicate key across areas', async () => {
    const messagesDir = fixture({ 'a.en.json': en({ 'k': 'A' }), 'b.en.json': en({ 'k': 'B' }) });
    await expect(lintApp({ messagesDir, app: 'shop' })).rejects.toThrow(/duplicate key k/);
  });
});

describe('SC-I18N-05 translation checks', () => {
  const base = { 'shop.count': '{n, plural, one {# item} other {# items}}', 'shop.hello': 'Hello {name}', 'shop.plain': 'Shop' };

  it('accepts a good translation and counts review states', async () => {
    const messagesDir = fixture({
      'a.en.json': en(base),
      'a.hi.json': tr({
        'shop.hello': ['नमस्ते {name}', 'reviewed', base['shop.hello']],
        'shop.plain': ['दुकान', 'machine', base['shop.plain']],
      }),
    });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors).toEqual([]);
    expect(r.stats['hi']).toEqual({ total: 3, reviewed: 1, machine: 1, missing: 1, stale: 0 });
    expect(r.stats['kn']).toEqual({ total: 3, reviewed: 0, machine: 0, missing: 3, stale: 0 });
  });

  it.each([
    ['a dropped placeholder', 'shop.hello', 'नमस्ते'],
    ['a renamed placeholder', 'shop.hello', 'नमस्ते {naam}'],
    ['a broken plural', 'shop.count', '{n, plural, one {# वस्तु}}'],
    ['a plural turned into plain text', 'shop.count', '{n} वस्तुएं'],
  ])('fails on %s', async (_label, key, message) => {
    const messagesDir = fixture({ 'a.en.json': en(base), 'a.hi.json': tr({ [key]: [message, 'reviewed', base[key as keyof typeof base]] }) });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors).toHaveLength(1);
    expect(r.errors[0]).toContain(key);
  });

  it('fails on an unknown key, an empty message and a bad review flag', async () => {
    const messagesDir = fixture({
      'a.en.json': en(base),
      'a.ta.json': {
        'shop.gone': { message: 'x', review: 'machine', enHash: 'x' },
        'shop.plain': { message: ' ', review: 'machine', enHash: 'x' },
        'shop.hello': { message: 'வணக்கம் {name}', review: 'maybe', enHash: 'x' },
      },
    });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors.map((e) => e.split(': ').slice(1).join(': '))).toEqual([
      'shop.gone: key is not in English (removed or renamed?)',
      'shop.plain: empty message',
      'shop.hello: review must be one of machine, reviewed',
    ]);
  });

  it('rejects {x, number}, {x, date} and {x, time} (pass pre-formatted values); plural # is fine', async () => {
    const messagesDir = fixture({
      'a.en.json': en({ 'shop.a': 'Total {n, number}', 'shop.b': 'On {d, date, short}', 'shop.c': 'At {t, time}', 'shop.d': '{n, plural, one {# item} other {# items}}' }),
    });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors).toHaveLength(3);
    expect(r.errors.join('\n')).toMatch(/shop\.a.*\{n\}/);
    expect(r.errors.join('\n')).not.toContain('shop.d');
    const hi = fixture({
      'a.en.json': en({ 'shop.a': 'Total {n}' }),
      'a.hi.json': tr({ 'shop.a': ['कुल {n, number}', 'reviewed', 'Total {n}'] }),
    });
    expect((await lintApp({ messagesDir: hi, app: 'shop' })).errors.join('\n')).toContain('formats {n} itself');
  });

  it('stale check comes first: a stale translation with other arguments only warns and ships English', async () => {
    const messagesDir = fixture({
      'a.en.json': en({ 'shop.hello': 'Hello {name}' }),
      'a.hi.json': tr({ 'shop.hello': ['नमस्ते', 'reviewed', 'Hello'] }), // reviewed against the old "Hello"
    });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors).toEqual([]);
    expect(r.warnings.some((w) => w.includes('shop.hello') && w.includes('English changed'))).toBe(true);
    expect(r.stats['hi']).toMatchObject({ stale: 1, reviewed: 0 });
    const warned: string[] = [];
    expect((await loadLocaleMessages('shop', 'hi', { dir: messagesDir, onWarn: (m) => warned.push(m) }))['shop.hello']).toBe('Hello {name}');
    expect(warned).toEqual([]); // stale is skipped quietly, not a bad translation
  });

  it('flags a translation as stale when English changed, and never ships it', async () => {
    const messagesDir = fixture({
      'a.en.json': en({ 'shop.plain': 'Shop now' }),
      'a.te.json': tr({ 'shop.plain': ['దుకాణం', 'reviewed', 'Shop'] }),
    });
    const r = await lintApp({ messagesDir, app: 'shop' });
    expect(r.errors).toEqual([]);
    expect(r.stats['te']).toMatchObject({ stale: 1, reviewed: 0 });
    expect((await loadLocaleMessages('shop', 'te', { dir: messagesDir }))['shop.plain']).toBe('Shop now');
  });
});

describe('SC-I18N-05 what ships: reviewed translations only, English for the rest', () => {
  it('lays reviewed over English; machine drafts only with drafts', async () => {
    const messagesDir = fixture({
      'a.en.json': en({ 'shop.a': 'A', 'shop.b': 'B', 'shop.c': 'C' }),
      'a.kn.json': tr({ 'shop.a': ['ಎ', 'reviewed', 'A'], 'shop.b': ['ಬಿ', 'machine', 'B'] }),
    });
    expect(await loadLocaleMessages('shop', 'kn', { dir: messagesDir })).toEqual({ 'shop.a': 'ಎ', 'shop.b': 'B', 'shop.c': 'C' });
    expect(await loadLocaleMessages('shop', 'kn', { dir: messagesDir, drafts: true })).toEqual({ 'shop.a': 'ಎ', 'shop.b': 'ಬಿ', 'shop.c': 'C' });
  });
});

describe('gen ships a translation only if it parses and matches English arguments', () => {
  it('ships English with a warning for a bad translation, and never aborts', async () => {
    const messagesDir = fixture({
      'a.en.json': en({ 'shop.good': 'Hello {name}', 'shop.noargs': 'Hello {name}', 'shop.broken': 'Hi {name}', 'shop.fmt': 'Total {n}', 'shop.tag': 'Read <b>this</b>' }),
      'a.hi.json': tr({
        'shop.good': ['नमस्ते {name}', 'reviewed', 'Hello {name}'],
        'shop.noargs': ['नमस्ते', 'reviewed', 'Hello {name}'],
        'shop.broken': ['नमस्ते {name', 'reviewed', 'Hi {name}'],
        'shop.fmt': ['कुल {n, number}', 'reviewed', 'Total {n}'],
        'shop.tag': ['यह पढ़ें', 'reviewed', 'Read <b>this</b>'],
      }),
    });
    const warned: string[] = [];
    const out = await loadLocaleMessages('shop', 'hi', { dir: messagesDir, onWarn: (m) => warned.push(m) });
    expect(out).toEqual({
      'shop.good': 'नमस्ते {name}',
      'shop.noargs': 'Hello {name}',
      'shop.broken': 'Hi {name}',
      'shop.fmt': 'Total {n}',
      'shop.tag': 'Read <b>this</b>',
    });
    expect(warned).toHaveLength(4);
    expect(warned.join('\n')).toContain('shop.broken');
    expect(() => compile(out)).not.toThrow();
  });
});

describe('SC-I18N-05 translation workflow: todo, import as machine draft, approve, ship', () => {
  it('walks a key from missing to shipped, and back when English changes', async () => {
    const messagesDir = fixture({ 'a.en.json': en({ 'shop.hello': 'Hello {name}', 'shop.plain': 'Shop' }) });
    expect((await todo(messagesDir, 'shop', 'hi')).map((t) => t.key)).toEqual(['shop.hello', 'shop.plain']);

    await expect(importDrafts(messagesDir, 'shop', 'a', 'hi', { 'shop.hello': 'नमस्ते' })).rejects.toThrow(/arguments differ/);
    await expect(importDrafts(messagesDir, 'shop', 'a', 'hi', { 'shop.nope': 'x' })).rejects.toThrow(/not in shop\/a\.en\.json/);
    await importDrafts(messagesDir, 'shop', 'a', 'hi', { 'shop.hello': 'नमस्ते {name}', 'shop.plain': 'दुकान' });
    expect((await todo(messagesDir, 'shop', 'hi')).length).toBe(0);
    expect(await loadLocaleMessages('shop', 'hi', { dir: messagesDir })).toEqual({ 'shop.hello': 'Hello {name}', 'shop.plain': 'Shop' });

    await approve(messagesDir, 'shop', 'a', 'hi', ['shop.hello']);
    expect(await loadLocaleMessages('shop', 'hi', { dir: messagesDir })).toEqual({ 'shop.hello': 'नमस्ते {name}', 'shop.plain': 'Shop' });
    expect((await lintApp({ messagesDir, app: 'shop' })).stats['hi']).toMatchObject({ reviewed: 1, machine: 1 });

    // English changes: the key needs translating again and cannot be approved as is.
    writeFileSync(join(messagesDir, 'shop', 'a.en.json'), JSON.stringify(en({ 'shop.hello': 'Welcome {name}', 'shop.plain': 'Shop' })));
    const again = await todo(messagesDir, 'shop', 'hi');
    expect(again.map((t) => [t.key, t.was])).toEqual([['shop.hello', 'नमस्ते {name}']]);
    await expect(approve(messagesDir, 'shop', 'a', 'hi', ['shop.hello'])).rejects.toThrow(/English changed/);
    expect((await loadLocaleMessages('shop', 'hi', { dir: messagesDir }))['shop.hello']).toBe('Welcome {name}');
  });
});
