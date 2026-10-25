import { createFileRoute } from '@tanstack/react-router';
import { VoicePage } from '../../voice/VoicePage';

/** B-06 Voice update. A static route outranks `/$section`; the page hides itself while voice is off. */
export const Route = createFileRoute('/voice/')({
  component: VoicePage,
});
