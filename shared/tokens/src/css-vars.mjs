// Small CSS custom-property helpers used by the tokens tests (and usable by app tests).

/** Names (without --) of every custom property declared in the CSS, e.g. `--bg: #fff` -> bg. */
export function declaredVars(css) {
  const names = new Set();
  for (const m of css.matchAll(/(?:^|[;{\s])--([a-zA-Z0-9-_]+)\s*:/g)) names.add(m[1]);
  return names;
}

/** Names (without --) of every custom property read through var(--name ...). */
export function usedVars(css) {
  const names = new Set();
  for (const m of css.matchAll(/var\(\s*--([a-zA-Z0-9-_]+)/g)) names.add(m[1]);
  return names;
}

/** Vars the CSS reads that neither it nor any of the `defining` stylesheets declare. */
export function undefinedVars(css, ...defining) {
  const declared = new Set(declaredVars(css));
  for (const d of defining) for (const n of declaredVars(d)) declared.add(n);
  return [...usedVars(css)].filter((n) => !declared.has(n)).sort();
}
