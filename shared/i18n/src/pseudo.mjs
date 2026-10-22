// en-XA pseudo-locale (06-screens.md G3, W1-03): accented letters, wrapped in [ ], and at least
// 40% longer than English, so untranslated strings, clipped labels and hard-coded text stand out.
// Works on the ICU AST, so {placeholders}, plurals, selects and <tags> keep working.
import { TYPE } from '@formatjs/icu-messageformat-parser';

const ACCENTS = {
  a: 'á', b: 'ƀ', c: 'ç', d: 'ð', e: 'é', f: 'ƒ', g: 'ĝ', h: 'ĥ', i: 'í', j: 'ĵ', k: 'ķ', l: 'ļ', m: 'ɱ',
  n: 'ñ', o: 'ö', p: 'þ', q: 'ǫ', r: 'ŕ', s: 'š', t: 'ţ', u: 'û', v: 'ṽ', w: 'ŵ', x: 'ẋ', y: 'ý', z: 'ž',
  A: 'Á', B: 'Ɓ', C: 'Ç', D: 'Ð', E: 'É', F: 'Ƒ', G: 'Ĝ', H: 'Ĥ', I: 'Í', J: 'Ĵ', K: 'Ķ', L: 'Ļ', M: 'Ṁ',
  N: 'Ñ', O: 'Ö', P: 'Þ', Q: 'Ǫ', R: 'Ŕ', S: 'Š', T: 'Ţ', U: 'Û', V: 'Ṽ', W: 'Ŵ', X: 'Ẋ', Y: 'Ý', Z: 'Ž',
};

/** Filler words appended to reach the target length; spaces let long strings wrap like real ones. */
const FILLER = 'ļöŕéɱ íþšûɱ ðöļöŕ šíţ áɱéţ çöñšéçţéţûŕ áðíþíšçíñĝ éļíţ šéð ðö éíûšɱöð ţéɱþöŕ';

export const PSEUDO_LOCALE = 'en-XA';
export const PSEUDO_GROWTH = 1.4;

export const accent = (s) => s.replace(/[A-Za-z]/g, (c) => ACCENTS[c] ?? c);

/** Visible text length of an AST: literals count as is, every argument counts as 4 characters. */
function textLength(ast) {
  let n = 0;
  for (const el of ast) {
    if (el.type === TYPE.literal) n += el.value.length;
    else if (el.type === TYPE.tag) n += textLength(el.children);
    else if (el.type === TYPE.plural || el.type === TYPE.select) {
      n += Math.max(...Object.values(el.options).map((o) => textLength(o.value)));
    } else n += 4;
  }
  return n;
}

function accentAst(ast) {
  return ast.map((el) => {
    if (el.type === TYPE.literal) return { ...el, value: accent(el.value) };
    if (el.type === TYPE.tag) return { ...el, children: accentAst(el.children) };
    if (el.type === TYPE.plural || el.type === TYPE.select) {
      const options = {};
      for (const [k, o] of Object.entries(el.options)) options[k] = { ...o, value: accentAst(o.value) };
      return { ...el, options };
    }
    return el;
  });
}

/** Padding that makes a string of `length` at least 40% longer once wrapped in [ ]. */
export function padding(length) {
  const extra = Math.max(1, Math.ceil(length * (PSEUDO_GROWTH - 1)) - 2);
  let out = '';
  while (out.length < extra) out += (out ? ' ' : '') + FILLER;
  return out.slice(0, extra);
}

/** Pseudo-localises a parsed ICU message (array of elements). */
export function pseudoAst(ast) {
  const len = textLength(ast);
  const pad = padding(len);
  return [
    { type: TYPE.literal, value: '[' },
    ...accentAst(ast),
    { type: TYPE.literal, value: ` ${pad}]` },
  ];
}
