import { createFileRoute } from '@tanstack/react-router';
import { ShelfScanPage } from '../onboarding/shelf/ShelfScanPage';

/** B-18 Add stock from a shelf photo. `onboarding_` keeps it out of the B-02 layout. */
export const Route = createFileRoute('/onboarding_/shelf')({
  component: ShelfScanPage,
});
