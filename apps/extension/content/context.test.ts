import { expect, it, vi } from 'vitest';
import { emptyState } from '../lib/schemas';
import type { Reply } from '../lib/protocol';
const send = vi.hoisted(() => vi.fn());
vi.mock('./messaging', () => ({ send, keepPort: () => () => undefined }));
import { ContentContext } from './context';
it('an initial disconnected reply cannot overwrite newer pushed state', async () => {
  let resolve!: (value: Reply) => void;
  send.mockReturnValueOnce(
    new Promise<Reply>((done) => {
      resolve = done;
    }),
  );
  const root = { classList: { toggle: vi.fn() } } as unknown as HTMLElement;
  const context = new ContentContext(root);
  const initial = context.refresh(false);
  context.push({ type: 'STATE', state: emptyState, ownsViewing: true });
  resolve({ ok: true, connected: false });
  await initial;
  expect(context.state).toEqual(emptyState);
  expect(context.ownsViewing).toBe(true);
  send.mockResolvedValueOnce({ ok: true, connected: false });
  await context.refresh(false);
  expect(context.state).toBeNull();
});
