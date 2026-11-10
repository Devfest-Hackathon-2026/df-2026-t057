import { expect, it, vi } from 'vitest';
import { emptyState } from '../lib/schemas';
import type { ContentContext } from './context';
import type { OverlayHost } from '../ui/overlay';
const mocks = vi.hoisted(() => ({ send: vi.fn(), choice: vi.fn(), pause: vi.fn() }));
vi.mock('./messaging', () => ({ send: mocks.send }));
vi.mock('./detector', () => ({
  currentVideo: () => ({ pause: mocks.pause }),
  measurable: () => true,
  onShorts: () => true,
}));
vi.mock('../ui/action', () => ({
  actionPending: () => false,
  runAction: vi.fn(),
  showError: vi.fn(),
}));
vi.mock('../ui/viewing-choice', () => ({
  availableSeconds: () => 1,
  showViewingChoice: mocks.choice,
}));
import { ViewingController } from './viewing-controller';

it('does not end the session before an unreported checkpoint survives a failed flush', async () => {
  mocks.send.mockImplementation((request: { type: string; meterId?: string; flush?: boolean }) =>
    Promise.resolve(
      request.type === 'CHECKPOINT_VIEWING' && !request.flush
        ? {
            ok: true,
            connected: true,
            checkpoint: { meterId: request.meterId, acknowledgedSeconds: 0 },
          }
        : { ok: false, error: 'network', retryable: true },
    ),
  );
  const context = {
    accountId: 'u1',
    ownsViewing: true,
    state: {
      ...emptyState,
      viewing: {
        reportCursor: 0,
        requiresSession: true,
        availableSeconds: 1,
        balanceSeconds: 1,
        session: {
          id: 's1',
          consumedSeconds: 0,
          plannedSeconds: 300,
          startedAt: '2026-11-10T00:00:00Z',
        },
      },
    },
    apply: vi.fn(),
    refresh: vi.fn(),
    toast: vi.fn(),
    line: vi.fn(),
  } as unknown as ContentContext;
  const controller = new ViewingController(context, {
    current: undefined,
  } as unknown as OverlayHost);
  controller.tick(1);
  controller.tick(1);
  for (let i = 0; i < 20; i++) await Promise.resolve();
  expect(
    mocks.send.mock.calls.some(([request]) => (request as { type: string }).type === 'END_VIEWING'),
  ).toBe(false);
  expect(mocks.choice).toHaveBeenCalledWith(expect.anything(), true, expect.anything());
});
