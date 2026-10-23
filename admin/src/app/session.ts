import { createSession } from '@todayz/auth';
import { API_URL, CLIENT_ID } from './env';

export function makeSession() {
  return createSession({ app: 'admin', baseUrl: API_URL, clientId: CLIENT_ID });
}
