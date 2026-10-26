import { disposeAll, removeState } from '../fixtures/principals';

export default async function globalTeardown() {
  await disposeAll();
  removeState();
}
