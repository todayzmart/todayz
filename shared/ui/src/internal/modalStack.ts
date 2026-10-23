/**
 * The open modal dialogs, topmost last. A modal `<dialog>` makes everything outside it inert,
 * including other top-layer elements such as a `popover` shown after it (checked in Chromium), so
 * anything that must stay usable while a modal is open (the toast region) renders inside the
 * topmost one. Modal registers here after showModal() and unregisters when it closes.
 */
const stack: HTMLDialogElement[] = [];
const listeners = new Set<() => void>();

const emit = () => {
  for (const l of listeners) l();
};

/** Registers an open modal; returns the unregister function. */
export function pushModal(el: HTMLDialogElement): () => void {
  stack.push(el);
  emit();
  return () => {
    const at = stack.lastIndexOf(el);
    if (at >= 0) stack.splice(at, 1);
    emit();
  };
}

/** The topmost open modal, or null. */
export function topModal(): HTMLDialogElement | null {
  return stack.at(-1) ?? null;
}

export function subscribeModals(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
