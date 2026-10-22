import preset from '@todayz/config/stylelint';

/** The shared preset plus <style> blocks inside .astro files. */
export default {
  ...preset,
  overrides: [{ files: ['**/*.astro'], customSyntax: 'postcss-html' }],
};
