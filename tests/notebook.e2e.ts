import { describe, test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { keelApi, keelSend, openKeel } from './keel.ts';

// The notebook pane as a reading surface (docs/ui-redesign.md §13, boards
// Notebook and NotebookTypes): one title (the name, editable), a meta line
// naming the pane shell fences type into, fences with Copy and "Type into
// <pane>" (shell fences only) that go through the approve dialog, and
// blockquotes as callouts.

const MD = [
  'Replica **pg-2** falls behind; compare `sent_lsn` with `replay_lsn`.',
  '',
  '## Check',
  '',
  '```sh',
  'printf "nb-typed-%s\\n" ok > /tmp/keel-nb-typed',
  '```',
  '',
  '```yaml',
  'postgresql:',
  '  parameters:',
  '    tcp_keepalives_idle: 60',
  '```',
  '',
  '> If lag passes 500 MB, page the DB owner.',
].join('\n');

type Notebook = { id: string; name: string };

const approveDialog = (screen: any) => screen.getByRole('dialog').filter({ hasText: 'Type into the terminal?' });

// Closes the panes of the open tabs: "Close pane" while a tab is split,
// "Close tab" for its last pane.
async function closeTabs(screen: any) {
  for (let i = 0; i < 8; i++) {
    const pane = screen.getByRole('button', 'Close pane', { visible: true });
    const tab = screen.getByRole('button', 'Close tab', { visible: true });
    if (await pane.count()) await pane.first().tap();
    else if (await tab.count()) await tab.first().tap();
    else return;
  }
}

describe('notebook pane', { tags: ['notebook'], serial: true }, () => {
  test('reads as one document and types only after approval', async ({ app, screen, browser }) => {
    const name = `nb-read-${Date.now()}`;
    const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name, cells: [{ body: MD }] });
    try {
      await openKeel(app);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Hosts').tap();
      await vault.getByRole('button', /^Local Shell\b/).tap();
      await expect(screen.getByRole('textbox', 'Terminal input')).toBeFocused();

      // Opens beside the session.
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
      const title = screen.getByRole('textbox', 'Rename notebook');
      await expect(title).toHaveValue(name);

      // One H1, and it is the name: markdown headings sit below it.
      expect(
        await browser.evaluate(`[...document.querySelectorAll('.notebook-pane h1')].map((h) => h.querySelector('input')?.value ?? h.textContent)`),
      ).toEqual([name]);

      // The meta line and the shell fence name the pane the command goes to.
      await expect.poll(() => browser.evaluate(`document.querySelector('[data-notebook-meta] .notebook-hostchip')?.textContent || ''`)).toBe('Local Shell');
      const typeBtn = screen.getByRole('button', 'Type into Local Shell');
      await expect(typeBtn).toHaveCount(1);
      // Every fence has Copy; only the shell fence has Type.
      await expect(screen.getByRole('button', 'Copy code')).toHaveCount(2);

      // The blockquote is a callout box with an icon.
      await expect(screen.getByRole('note').filter({ hasText: 'page the DB owner' })).toBeVisible();

      // Code does not wrap; inline code does not wrap either.
      expect(
        await browser.evaluate(`[getComputedStyle(document.querySelector('.notebook-fence pre')).whiteSpace, getComputedStyle(document.querySelector('.notebook-doc p code')).whiteSpace]`),
      ).toEqual(['pre', 'nowrap']);

      // A block that changes state goes through the same approve dialog
      // as agent proposals (§6 rule 13): cancel types nothing.
      await typeBtn.tap();
      const dialog = approveDialog(screen);
      await expect(dialog).toBeVisible();
      await expect(dialog).toContainText('printf "nb-typed-%s\\n" ok > /tmp/keel-nb-typed');
      await dialog.getByRole('button', 'Cancel').tap();
      await expect(dialog).toBeHidden();
      await screen.getByRole('button', 'Cancel', { visible: true }).tap();

      // Renaming the H1 renames the notebook.
      await title.fill(`${name}-renamed`);
      await expect
        .poll(async () => (await keelApi<Notebook[]>(app.baseUrl, '/api/notebooks')).find((n) => n.id === nb.id)?.name)
        .toBe(`${name}-renamed`);
      await closeTabs(screen);
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
    }
  });

  test('without a terminal the fence says Type into pane', async ({ app, screen }) => {
    const name = `nb-alone-${Date.now()}`;
    const nb = await keelSend<Notebook>(app.baseUrl, 'POST', '/api/notebooks', { name, cells: [{ body: '```bash\nuptime\n```' }] });
    try {
      await openKeel(app);
      await closeTabs(screen);
      const vault = screen.getByRole('complementary', 'Vault');
      await vault.getByRole('button', 'Snippets').tap();
      await vault.getByRole('button', new RegExp(`^${name}$`)).tap();
      await expect(screen.getByRole('textbox', 'Rename notebook')).toHaveValue(name);
      await expect(screen.getByText('No terminal in this tab')).toBeVisible();
      await screen.getByRole('button', 'Type into pane').tap();
      await expect(screen.getByText('Split a terminal beside this notebook first')).toBeVisible();
      await expect(approveDialog(screen)).toBeHidden();
      await closeTabs(screen);
    } finally {
      await keelSend(app.baseUrl, 'DELETE', `/api/notebooks/${nb.id}`);
    }
  });
});
