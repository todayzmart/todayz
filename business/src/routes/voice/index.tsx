import { createFileRoute } from '@tanstack/react-router';
import { validateVoiceSearch } from '../../voice/lines';
import { VoicePage } from '../../voice/VoicePage';

/**
 * B-06 Voice update. A static route outranks `/$section`; the page hides itself while voice is off.
 * `?draft=<id>` reopens a draft (a manager finishing the lines a cashier could not confirm).
 */
export const Route = createFileRoute('/voice/')({
  validateSearch: validateVoiceSearch,
  component: function VoiceRoute() {
    return <VoicePage draft={Route.useSearch().draft} />;
  },
});
