import { displayTime, elapsedSeconds, viewingAvailable } from '../../shared/models';
import { ExtensionState, MANAGEMENT_ORIGIN, send } from './protocol';

const status = document.querySelector<HTMLElement>('#status')!;
const balance = document.querySelector<HTMLElement>('#balance')!;
const focus = document.querySelector<HTMLElement>('#focus')!;
document.querySelector<HTMLButtonElement>('#open')!.onclick = () => {
  void send({ type: 'OPEN_APP' });
};
document.querySelector<HTMLButtonElement>('#permission')!.onclick = async () => {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url || !/^https?:/.test(tab.url)) {
    status.textContent = '作業するページを開いてから押してください。';
    return;
  }
  const origin = new URL(tab.url).origin + '/*';
  try {
    if (!(await chrome.permissions.request({ origins: [origin] }))) {
      status.textContent = '許可を変更していません。';
      return;
    }
    const id = 'work-' + origin.replace(/\W/g, '_');
    const scripts = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
    if (!scripts.length)
      await chrome.scripting.registerContentScripts([
        { id, matches: [origin], js: ['content.js'], persistAcrossSessions: true },
      ]);
    if (tab.id)
      await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: ['content.js'] });
    status.textContent = 'このサイトで作業タイマーを表示します。';
  } catch (error) {
    status.textContent =
      error instanceof Error ? error.message : 'タイマーを表示できませんでした。';
  }
};
function render(state: ExtensionState) {
  balance.textContent =
    state.timePolicy?.mode === 'self' || state.timePolicy?.mode === 'reminder'
      ? '制限なし'
      : `${Math.floor(viewingAvailable(state) / 60)} min`;
  if (state.session) {
    const task = state.tasks.find((t) => t.id === state.session!.taskId);
    focus.textContent = `作業中 · ${task?.title} · ${displayTime(elapsedSeconds(state.session))}`;
  } else focus.textContent = '課題の開始時間になると通知します。';
}
void send({ type: 'STATE' })
  .then((reply) => {
    if (reply.error) status.textContent = reply.error;
    else if (reply.state) {
      status.textContent = '接続済み';
      render(reply.state);
    } else status.textContent = '管理画面を開くと接続されます。';
  })
  .catch(() => {
    status.textContent = `${MANAGEMENT_ORIGIN} から接続してください。`;
  });

const account = document.querySelector<HTMLElement>('#account')!;
void send({ type: 'DIAGNOSTICS' }).then((reply) => {
  account.textContent = reply.diagnostics?.email || '拡張の接続アカウントを確認してください。';
});
for (const [id, type] of [
  ['connect', 'AUTH_SIGN_IN'],
  ['disconnect', 'AUTH_SIGN_OUT'],
  ['test-notice', 'TEST_NOTICE'],
] as const) {
  const button = document.querySelector<HTMLButtonElement>('#' + id)!;
  button.onclick = async () => {
    button.disabled = true;
    try {
      const reply = await send({ type });
      status.textContent =
        reply.error ||
        (type === 'TEST_NOTICE'
          ? '通知を送信しました。OSの通知欄を確認してください。'
          : '接続状態を更新しました。');
    } finally {
      button.disabled = false;
    }
  };
}
