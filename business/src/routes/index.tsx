import { createFileRoute } from '@tanstack/react-router';
import { OverviewPage } from '../overview/OverviewPage';

/** B-03 Overview is the business landing page (W3-03 replaced the D-84 redirect to Orders). */
export const Route = createFileRoute('/')({
  component: OverviewPage,
});
