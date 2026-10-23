import { createSession } from '@todayz/auth';
import { API_URL, CLIENT_ID } from './env';

let language: string | null = null;
/** The interface language, sent as Accept-Language. */
export function setSessionLanguage(code: string): void {
  language = code;
}

export function makeSession() {
  return createSession({ app: 'customer', baseUrl: API_URL, clientId: CLIENT_ID, language: () => language });
}
