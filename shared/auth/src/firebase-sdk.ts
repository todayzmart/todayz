// The Firebase JS SDK for phone sign-in (D-275). Only firebase.ts imports this file, with
// import(), so it lands in its own chunk. Firebase keeps nothing in the browser (in-memory
// persistence) and is signed out as soon as the ID token is read: TodayZ's session is the only one.
import { deleteApp, getApps, initializeApp, type FirebaseApp } from '@firebase/app';
import {
  RecaptchaVerifier,
  connectAuthEmulator,
  inMemoryPersistence,
  initializeAuth,
  signInWithPhoneNumber,
  signOut,
  type Auth,
} from '@firebase/auth';
import type { FirebaseWebConfig } from '@todayz/api-client/gen/model/firebaseWebConfig';
import { FirebasePhoneError, firebaseErrorKind, type FirebaseConfirmation, type FirebasePhone } from './firebase';

const APP_NAME = 'todayz-auth';

let current: { key: string; app: FirebaseApp; auth: Auth } | null = null;

async function authFor(config: FirebaseWebConfig): Promise<Auth> {
  const key = JSON.stringify([config.apiKey, config.authDomain, config.projectId, config.appId, config.emulatorUrl ?? '']);
  if (current?.key === key) return current.auth;
  // A new config (the API moved projects, or hot reload left an app behind): start again.
  current = null;
  await Promise.all(getApps().filter((a) => a.name === APP_NAME).map((a) => deleteApp(a).catch(() => undefined)));
  const app = initializeApp(
    { apiKey: config.apiKey, authDomain: config.authDomain, projectId: config.projectId, appId: config.appId },
    APP_NAME,
  );
  const auth = initializeAuth(app, { persistence: inMemoryPersistence });
  // Local and test: the mocks server stands in for Google, and reCAPTCHA is skipped.
  if (config.emulatorUrl) connectAuthEmulator(auth, config.emulatorUrl, { disableWarnings: true });
  current = { key, app, auth };
  return auth;
}

export async function createFirebasePhone(config: FirebaseWebConfig): Promise<FirebasePhone> {
  const auth = await authFor(config);
  return {
    async send(e164: string, languageCode?: string): Promise<FirebaseConfirmation> {
      auth.languageCode = languageCode ?? null;
      // Invisible reCAPTCHA keeps its badge visible in the corner while the code is sent.
      const host = document.createElement('div');
      host.style.position = 'fixed';
      host.style.bottom = '8px';
      host.style.right = '8px';
      document.body.appendChild(host);
      const verifier = new RecaptchaVerifier(auth, host, { size: 'invisible' });
      try {
        const result = await signInWithPhoneNumber(auth, e164, verifier);
        return {
          async confirm(code: string): Promise<string> {
            try {
              const cred = await result.confirm(code);
              const token = await cred.user.getIdToken();
              await signOut(auth).catch(() => undefined);
              return token;
            } catch (e) {
              throw new FirebasePhoneError(firebaseErrorKind(e));
            }
          },
        };
      } catch (e) {
        throw new FirebasePhoneError(firebaseErrorKind(e));
      } finally {
        verifier.clear();
        host.remove();
      }
    },
  };
}
