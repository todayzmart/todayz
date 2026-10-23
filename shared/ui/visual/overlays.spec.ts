import { expect, test } from '@playwright/test';
import { guardPage } from '@todayz/config/playwright';
import { openStory } from './helpers';

// Native <dialog> behaviour that happy-dom cannot show: showModal() focus, Escape (the browser's
// own cancel/close), focus returning to the opener, and the rest of the page turning inert, which
// would leave a toast raised from inside the modal unclickable unless its region moves into it.
const cases = [
  {
    story: 'feedback-and-overlays-dialog--with-toast',
    opener: 'Edit opening hours',
    name: 'Opening hours',
    save: 'Save hours',
    message: 'Hours saved',
    undone: 'Hours restored',
  },
  {
    story: 'feedback-and-overlays-sheet--with-toast',
    opener: 'Show shop',
    name: 'Sri Lakshmi Provision Stores',
    save: 'Save shop',
    message: 'Shop saved',
    undone: 'Shop removed from saved',
  },
];

for (const c of cases) {
  test(`overlay: ${c.story}`, async ({ page }) => {
    const guard = await guardPage(page);
    await openStory(page, c.story);
    const opener = page.getByRole('button', { name: c.opener });
    const dialog = page.getByRole('dialog', { name: c.name });
    const focusInDialog = () => page.evaluate(() => Boolean(document.activeElement?.closest('dialog[open]')));

    await expect(dialog).toBeHidden();
    await opener.click();
    await expect(dialog).toBeVisible();
    await expect.poll(focusInDialog).toBe(true);

    // Escape closes it and the story's state follows: the dialog stays closed (a stale open prop
    // would reopen it), focus is back on the opener, and the opener opens it again.
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();
    await opener.click();
    await expect(dialog).toBeVisible();
    await expect.poll(focusInDialog).toBe(true);

    // A toast raised while the modal is open is visible and its action takes a real click (no
    // force: Playwright checks the action is the element that receives the pointer).
    await dialog.getByRole('button', { name: c.save }).click();
    const toast = page.getByRole('status').filter({ hasText: c.message });
    await expect(toast).toBeVisible();
    const undo = toast.getByRole('button', { name: 'Undo' });
    await undo.click();
    await expect(page.getByText(c.undone)).toBeAttached();
    await expect(page.getByText(c.message)).toHaveCount(0);
    // The toast action does not close the modal.
    await expect(dialog).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();

    expect(guard.external).toEqual([]);
    expect(guard.errors).toEqual([]);
  });
}
