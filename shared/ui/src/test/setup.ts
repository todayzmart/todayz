// Vitest setup: jest-dom matchers, DOM cleanup between tests, and the Storybook preview
// annotations so composeStories renders stories exactly as Storybook does.
import '@testing-library/jest-dom/vitest';
import { setProjectAnnotations } from '@storybook/react-vite';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';
import * as preview from '../../.storybook/preview';

setProjectAnnotations([preview]);

afterEach(() => {
  cleanup();
  document.documentElement.classList.remove('tz-ui-scroll-lock');
});
