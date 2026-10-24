import { createFileRoute } from '@tanstack/react-router';
import { OnboardingPage } from '../onboarding/OnboardingPage';

/** B-02 Shop setup. Not in the nav: shops reach it from the banner on Stores and Settings. */
export const Route = createFileRoute('/onboarding')({
  component: OnboardingPage,
});
