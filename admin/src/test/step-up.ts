import { fireEvent, screen, within } from '@testing-library/react';
import { json, type Handler } from './render';

/** The request id the fake `POST /v1/auth/step-up/send` answers with. */
export const STEP_UP_REQUEST = '01JC0000000000000000000SMS';
/** The code the fake verify accepts. */
export const STEP_UP_CODE = '123456';

/**
 * The SMS step-up routes (D-264): send answers a request id and a 30 s wait; verify takes that id
 * with STEP_UP_CODE, clears `stepUpNeeded` and answers `validUntil`. A wrong code is
 * `422 code_invalid` with 4 tries left, another request id `401 otp_expired`.
 */
export function stepUpHandler(state: { stepUpNeeded: boolean }): Handler {
  return (c) => {
    if (c.method === 'POST' && c.path === '/v1/auth/step-up/send') return json(200, { requestId: STEP_UP_REQUEST, resendAfter: 30 });
    if (c.method !== 'POST' || c.path !== '/v1/auth/step-up/verify') return undefined;
    const b = c.body as { requestId: string; code: string };
    if (b.requestId !== STEP_UP_REQUEST) return json(401, { type: 'about:blank', title: 'x', status: 401, code: 'otp_expired' });
    if (b.code !== STEP_UP_CODE) return json(422, { type: 'about:blank', title: 'x', status: 422, code: 'code_invalid', extensions: { attemptsLeft: 4 } });
    state.stepUpNeeded = false;
    return json(200, { validUntil: new Date(Date.now() + 10 * 60_000).toISOString() });
  };
}

/** Types a code into the step-up dialog's boxes. */
export async function typeStepUpCode(code: string) {
  const boxes = (await screen.findByRole('group', { name: 'Code from the SMS' })).querySelectorAll('input');
  for (const [i, d] of code.split('').entries()) fireEvent.change(boxes[i] as HTMLInputElement, { target: { value: d } });
}

/** Waits for the step-up dialog, taps "Send code" and waits for the code boxes; returns the dialog. */
export async function sendStepUpCode(): Promise<HTMLElement> {
  const dialog = await screen.findByRole('dialog', { name: 'Confirm it is you' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Send code' }));
  await within(dialog).findByRole('group', { name: 'Code from the SMS' });
  return dialog;
}

/** The whole step-up: send the SMS, then type the right code. */
export async function passStepUp(): Promise<void> {
  await sendStepUpCode();
  await typeStepUpCode(STEP_UP_CODE);
}
