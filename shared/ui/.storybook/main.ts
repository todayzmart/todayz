import type { StorybookConfig } from '@storybook/react-vite';

// Storybook for @todayz/ui. Runs offline (make check runs under no-egress): no telemetry, no
// update checks, fonts self-hosted by @todayz/tokens.
const config: StorybookConfig = {
  framework: { name: '@storybook/react-vite', options: {} },
  stories: ['../src/**/*.stories.tsx'],
  addons: ['@storybook/addon-a11y'],
  core: { disableTelemetry: true, disableWhatsNewNotifications: true },
  typescript: { reactDocgen: false },
};

export default config;
