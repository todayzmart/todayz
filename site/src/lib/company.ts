import { resolveAppLinks } from './env.ts';

// Company facts the site shows. Anything not decided yet is null and the pages say so plainly
// instead of inventing a value (open questions are listed in docs/qa/W1-09/REPORT.md).
export const company = {
  name: 'TodayZ',
  /** Public contact email. Null until the team chooses one. */
  email: null as string | null,
  /** Grievance officer under India's data protection law. Null until named. */
  grievance: { name: null as string | null, email: null as string | null },
  /** Social profiles for JSON-LD sameAs. Empty until they exist. */
  sameAs: [] as string[],
} as const;

/** Apps the site links to. Overridable per build so local and staging builds point at their own hosts. */
export const links = resolveAppLinks(import.meta.env);
