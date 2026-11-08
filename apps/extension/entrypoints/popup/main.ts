// ポップアップ。接続の状態、視聴できる時間、作業中の課題か次の課題、課題の登録、作業サイトでのタイマーの許可。
import '@dopagaki/tokens/tokens.css';
import './style.css';
import { newIdempotencyKey, type TaskCreateInput } from '@dopagaki/contract';
import { clockLabel } from '@dopagaki/messages';
import { send } from '../../content/messaging';
import { MANAGEMENT_ORIGIN } from '../../lib/config';
import { fromLocalInput, toLocalInput } from '../../lib/draft-edit';
import { currentLocale, currentMessages, followLocale } from '../../lib/locale';
import type { OkReply, Request } from '../../lib/protocol';
import type { ExtensionState } from '../../lib/schemas';
import { workSitePattern, workSiteScriptId } from '../../lib/urls';
import { workElapsedSeconds } from '../../lib/work-timer';
import { runAction } from '../../ui/action';
import { button } from '../../ui/dom';
import { YOUTUBE_MATCH } from '../../buildconfig/manifest';

function byId<T extends HTMLElement>(id: string, type: new () => T): T {
  const element = document.getElementById(id);
  if (!(element instanceof type)) throw new Error(`#${id} is missing`);
  return element;
}

const balance = byId('balance', HTMLElement);
const focus = byId('focus', HTMLElement);
const workActions = byId('work-actions', HTMLElement);
const status = byId('status', HTMLElement);
const openButton = byId('open', HTMLButtonElement);
const permissionButton = byId('permission', HTMLButtonElement);
const workSection = byId('work', HTMLElement);
const links = byId('links', HTMLElement);
const addTaskButton = byId('add-task', HTMLButtonElement);
const taskForm = byId('task-form', HTMLFormElement);
const titleInput = byId('task-title', HTMLInputElement);
const deadlineInput = byId('task-deadline', HTMLInputElement);
const minutesInput = byId('task-minutes', HTMLInputElement);
const presets = byId('task-presets', HTMLElement);
const whyInput = byId('task-why', HTMLInputElement);
const urlInput = byId('task-url', HTMLInputElement);
const notesInput = byId('task-notes', HTMLTextAreaElement);
const cancelTaskButton = byId('task-cancel', HTMLButtonElement);
const submitTaskButton = byId('task-submit', HTMLButtonElement);

const connectButton = button('', () => {
  void runAction(workActions, { type: 'SIGN_IN' }).then((reply) => {
    if (reply?.ok && reply.state) render(reply.state);
  });
});
const testButton = button('', () => {
  void runAction(workActions, { type: 'TEST_NOTICE' });
});
document.body.append(connectButton, testButton);

const presetButtons = [15, 30, 60, 90, 120].map((minutes) => {
  const element = button('', () => {
    minutesInput.value = String(minutes);
    markPreset();
  });
  presets.append(element);
  return { minutes, element };
});

/** 必要時間の欄と同じ値のボタンを、押された状態で示す。 */
function markPreset(): void {
  for (const { minutes, element } of presetButtons) {
    const selected = Number(minutesInput.value) === minutes;
    element.className = `button ${selected ? 'tonal' : 'outlined'}`;
    element.setAttribute('aria-pressed', String(selected));
  }
}

/** 状態によらない文言。プロフィールの言語を受け取ったら書き直す。 */
function renderLabels(): void {
  const copy = currentMessages().extension;
  document.documentElement.lang = currentLocale();
  byId('balance-label', HTMLElement).textContent = copy.popupAvailable;
  openButton.textContent = copy.popupOpenApp;
  permissionButton.textContent = copy.popupWorkSite;
  connectButton.textContent = copy.popupSignIn;
  testButton.textContent = copy.popupTestNotice;
  addTaskButton.textContent = copy.popupAddTask;
  byId('task-heading', HTMLElement).textContent = copy.popupAddTask;
  byId('task-title-label', HTMLElement).textContent = copy.popupTaskTitle;
  byId('task-deadline-label', HTMLElement).textContent = copy.popupTaskDeadline;
  byId('task-minutes-label', HTMLElement).textContent = copy.popupTaskMinutes;
  byId('task-details', HTMLElement).textContent = copy.popupTaskDetails;
  byId('task-why-label', HTMLElement).textContent = copy.popupTaskWhy;
  byId('task-url-label', HTMLElement).textContent = copy.popupTaskUrl;
  byId('task-notes-label', HTMLElement).textContent = copy.popupTaskNotes;
  presets.setAttribute('aria-label', copy.popupTaskPresets);
  for (const { minutes, element } of presetButtons)
    element.textContent = copy.popupTaskPreset(minutes);
  cancelTaskButton.textContent = copy.cancel;
  submitTaskButton.textContent = copy.popupTaskRegister;
}

renderLabels();
focus.textContent = currentMessages().extension.popupChecking;

let state: ExtensionState | undefined;

function renderBalance(current: ExtensionState): void {
  const mode = current.wallet?.policy.mode;
  const available = current.viewing?.availableSeconds ?? current.wallet?.availableSeconds;
  balance.textContent =
    mode === 'self' || mode === 'reminder'
      ? currentMessages().extension.popupUnlimited
      : available === undefined
        ? '—'
        : currentMessages().shared.durationLabel(Math.max(0, available));
}

function act(request: Request, after?: (reply: OkReply) => void): void {
  void runAction(workActions, request).then((reply) => {
    if (!reply?.ok) return;
    if (reply.state) render(reply.state);
    after?.(reply);
  });
}

function renderWork(current: ExtensionState): void {
  const copy = currentMessages().extension;
  workActions.replaceChildren();
  const session = current.workSession;
  if (session) {
    const task = current.tasks.find((item) => item.id === session.taskId);
    focus.textContent = copy.popupWorking(
      task?.title ?? '',
      clockLabel(workElapsedSeconds(session, current.workSyncedAt, Date.now())),
    );
    workActions.append(
      button(session.running ? copy.pause : copy.resume, () => {
        act({ type: session.running ? 'PAUSE_WORK' : 'RESUME_WORK' });
      }),
      button(copy.confirmWork, () => {
        act({ type: 'CONFIRM_WORK', sessionId: session.id }, (reply) => {
          if (reply.work)
            status.textContent = copy.workReward(reply.work.rewardSeconds, reply.work.deferred);
        });
      }),
      button(
        copy.complete,
        () => {
          act({ type: 'COMPLETE_TASK', taskId: session.taskId }, (reply) => {
            status.textContent = copy.workReward(
              reply.work?.rewardSeconds ?? 0,
              reply.work?.deferred ?? false,
            );
          });
        },
        'tonal',
      ),
    );
    return;
  }
  const next = current.tasks
    .filter((task) => task.status === 'pending')
    .sort((a, b) => Date.parse(a.plan.start) - Date.parse(b.plan.start))[0];
  if (!next) {
    focus.textContent = copy.popupIdle;
    return;
  }
  focus.textContent = copy.popupNext(next.title, copy.timeLabel(next.plan.start));
  workActions.append(
    button(
      copy.popupStartNext,
      () => {
        act({ type: 'START_TASK', taskId: next.id });
      },
      'tonal',
    ),
  );
}

function render(current: ExtensionState): void {
  state = current;
  followLocale(current);
  renderLabels();
  renderBalance(current);
  renderWork(current);
  addTaskButton.hidden = false;
}

setInterval(() => {
  const session = state?.workSession;
  if (!state || !session) return;
  const task = state.tasks.find((item) => item.id === session.taskId);
  focus.textContent = currentMessages().extension.popupWorking(
    task?.title ?? '',
    clockLabel(workElapsedSeconds(session, state.workSyncedAt, Date.now())),
  );
}, 1000);

/** 登録する課題の ID。送り直しても二重に登録しないよう、フォームを開くたびに一度だけ作る。 */
let newTaskId = newIdempotencyKey();

function showTaskForm(shown: boolean): void {
  taskForm.hidden = !shown;
  workSection.hidden = shown;
  links.hidden = shown;
}

addTaskButton.addEventListener('click', () => {
  taskForm.reset();
  taskForm.querySelector(':scope > .error')?.remove();
  // 管理画面の登録と同じく、今より後の次の15分刻みを締切の初期値にする。
  deadlineInput.value = toLocalInput(
    new Date((Math.floor(Date.now() / 900_000) + 1) * 900_000).toISOString(),
  );
  minutesInput.value = '30';
  markPreset();
  newTaskId = newIdempotencyKey();
  status.textContent = '';
  showTaskForm(true);
  titleInput.focus();
});

cancelTaskButton.addEventListener('click', () => {
  showTaskForm(false);
  addTaskButton.focus();
});

minutesInput.addEventListener('input', markPreset);

/** 入力を確かめて、送る形にする。足りなければ欄ごとの理由を出して undefined を返す。 */
function taskInput(): TaskCreateInput | undefined {
  const copy = currentMessages().extension;
  const title = titleInput.value.trim();
  const deadline = fromLocalInput(deadlineInput.value);
  const minutes = Number(minutesInput.value);
  // ブラウザーの既定の文ではなく、プロフィールの言語の文で知らせる。
  titleInput.setCustomValidity(title === '' ? copy.popupTaskTitleRequired : '');
  deadlineInput.setCustomValidity(
    deadline !== undefined && Date.parse(deadline) > Date.now() ? '' : copy.popupTaskDeadlinePast,
  );
  minutesInput.setCustomValidity(
    Number.isInteger(minutes) && minutes >= 1 && minutes <= 720 ? '' : copy.popupTaskMinutesRange,
  );
  if (!taskForm.reportValidity() || deadline === undefined) return undefined;
  return {
    id: newTaskId,
    title,
    deadline,
    minutes,
    why: whyInput.value.trim(),
    url: urlInput.value.trim(),
    notes: notesInput.value,
  };
}

taskForm.addEventListener('submit', (event) => {
  event.preventDefault();
  const input = taskInput();
  if (!input) return;
  void runAction(taskForm, { type: 'CREATE_TASK', input }).then((reply) => {
    if (!reply?.ok) return;
    if (reply.state) render(reply.state);
    showTaskForm(false);
    status.textContent = currentMessages().extension.popupTaskCreated(input.title);
    addTaskButton.focus();
  });
});

openButton.addEventListener('click', () => {
  void send({ type: 'OPEN_APP' }).then(() => {
    window.close();
  });
});

// 許可を求めるのはクリックの直後に行う必要があるので、対象のタブは先に調べておく。
const activeTab = chrome.tabs.query({ active: true, currentWindow: true }).then(([tab]) => tab);
let tabNow: chrome.tabs.Tab | undefined;
void activeTab.then((tab) => {
  tabNow = tab;
});

/** YouTube と管理画面には、もともとタイマー（または管理画面の表示）があるので登録しない。 */
function alreadyCovered(pattern: string): boolean {
  return pattern === YOUTUBE_MATCH || pattern === `${MANAGEMENT_ORIGIN}/*`;
}

permissionButton.addEventListener('click', () => {
  const tab = tabNow;
  const pattern = workSitePattern(tab?.url);
  if (!tab || !pattern) {
    status.textContent = currentMessages().extension.popupOpenWorkPage;
    return;
  }
  if (alreadyCovered(pattern)) {
    status.textContent = currentMessages().extension.popupWorkSiteEnabled;
    return;
  }
  void chrome.permissions
    .request({ origins: [pattern] })
    .then(async (granted) => {
      if (!granted) {
        status.textContent = currentMessages().extension.popupPermissionUnchanged;
        return;
      }
      const id = workSiteScriptId(pattern);
      const file = 'content-scripts/worksite.js';
      const registered = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
      if (registered.length === 0) {
        await chrome.scripting.registerContentScripts([
          {
            id,
            matches: [pattern],
            js: [file],
            runAt: 'document_idle',
            persistAcrossSessions: true,
          },
        ]);
      }
      if (tab.id !== undefined)
        await chrome.scripting.executeScript({ target: { tabId: tab.id }, files: [file] });
      status.textContent = currentMessages().extension.popupWorkSiteEnabled;
    })
    .catch(() => {
      status.textContent = currentMessages().extension.popupWorkSiteFailed;
    });
});

void send({ type: 'GET_STATE', refresh: false }).then((reply) => {
  if (!reply.ok) {
    status.textContent = reply.retryable
      ? currentMessages().extension.popupConnectFrom(MANAGEMENT_ORIGIN)
      : reply.error;
    focus.textContent = currentMessages().extension.popupIdle;
    return;
  }
  if (!reply.connected || !reply.state) {
    status.textContent = currentMessages().extension.popupOpenToConnect;
    focus.textContent = currentMessages().extension.popupIdle;
    return;
  }
  // 先に状態を反映して、プロフィールの言語に切り替えてから書く。
  render(reply.state);
  status.textContent = currentMessages().extension.popupConnected;
});
