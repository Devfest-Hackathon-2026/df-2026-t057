import {
  Draft,
  State,
  Task,
  displayTime,
  elapsedSeconds,
  plannedSeconds,
} from '../../shared/models';
import { MANAGEMENT_ORIGIN, Reply, Request, send } from './protocol';

const runtimeWindow = window as Window & { dopagakiLoaded?: boolean };
if (!runtimeWindow.dopagakiLoaded) {
  runtimeWindow.dopagakiLoaded = true;
  initialize();
}
function initialize() {
  let state: State | undefined;
  let overlay: HTMLDialogElement | undefined;
  let root: ShadowRoot | undefined;
  let timer: HTMLElement | undefined;
  let requestPending = false;
  let consumed = 0;
  let receipt: { id: string; seconds: number } | undefined;
  let lastTick = performance.now();
  let previousPlaying = false;
  let retryAfter = 0;
  const shorts = () =>
    location.hostname === 'www.youtube.com' && location.pathname.startsWith('/shorts/');
  function shortsVideo(): HTMLVideoElement | undefined {
    return (
      document.querySelector<HTMLVideoElement>('ytd-reel-video-renderer[is-active] video') ??
      [...document.querySelectorAll('video')].find((video) => !video.paused && !video.ended) ??
      document.querySelector<HTMLVideoElement>('video') ??
      undefined
    );
  }

  if (location.origin === MANAGEMENT_ORIGIN) {
    window.addEventListener('message', (event) => {
      if (event.source !== window || event.origin !== MANAGEMENT_ORIGIN) return;
      if (event.data?.type === 'DOPAGAKI_DISCONNECT') {
        void send({ type: 'DISCONNECT' });
        return;
      }
      if (event.data?.type !== 'DOPAGAKI_CONNECT') return;
      void send({
        type: 'CONNECT',
        connection: {
          apiBase: event.data.apiBase,
          auth: event.data.auth,
          accountId: event.data.accountId,
        },
      });
    });
  }
  function element<K extends keyof HTMLElementTagNameMap>(tag: K, text = '', className = '') {
    const el = document.createElement(tag);
    el.textContent = text;
    el.className = className;
    return el;
  }
  function button(text: string, action: () => void, className = '') {
    const el = element('button', text, className);
    el.type = 'button';
    el.onclick = action;
    return el;
  }
  function mount() {
    if (root) return root;
    const host = element('div');
    host.id = 'dopagaki-extension';
    root = host.attachShadow({ mode: 'open' });
    const style = element('link');
    style.rel = 'stylesheet';
    style.href = chrome.runtime.getURL('content.css');
    root.append(style);
    document.documentElement.append(host);
    return root;
  }
  function errorText(container: HTMLElement, text: string) {
    container.querySelector('.error')?.remove();
    const error = element('p', text, 'error');
    error.setAttribute('role', 'alert');
    container.append(error);
  }
  async function action(request: Request, container: HTMLElement): Promise<Reply | undefined> {
    if (requestPending) return;
    requestPending = true;
    const buttons = [...container.querySelectorAll('button')];
    buttons.forEach((b) => (b.disabled = true));
    const status = element('p', '処理しています…', 'loading');
    status.setAttribute('role', 'status');
    container.append(status);
    try {
      const reply = await send(request);
      if (reply.error) {
        errorText(container, reply.error);
        return;
      }
      if (reply.state) {
        state = reply.state;
        renderTimer();
      }
      return reply;
    } catch {
      errorText(container, '接続できませんでした。入力は残っています。再試行してください。');
      return;
    } finally {
      requestPending = false;
      status.remove();
      buttons.forEach((b) => (b.disabled = false));
    }
  }
  function dismiss() {
    overlay?.close();
    overlay?.remove();
    overlay = undefined;
  }
  function showNotice(task: Task, snapshot: State) {
    if (!shorts() || document.visibilityState !== 'visible') return false;
    state = snapshot;
    dismiss();
    const video = shortsVideo();
    video?.pause();
    overlay = element(
      'dialog',
      '',
      `overlay ${snapshot.settings.mode} ${snapshot.settings.reducedMotion ? 'reduced' : ''}`,
    );
    overlay.style.setProperty('--gold', `url("${chrome.runtime.getURL('gold-light.png')}")`);
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', '課題をはじめる時間です');
    const panel = element('div', '', 'panel');
    panel.append(
      element('p', "IT'S YOUR TURN", 'eyebrow'),
      element('h1', snapshot.settings.mode === 'mahjong' ? 'まず、一手。' : 'そろそろ、やるか。'),
    );
    const card = element('div', '', 'task-card');
    card.append(element('h2', task.title));
    if (task.why) card.append(element('p', `なぜ：${task.why}`, 'why'));
    const first = task.plan.steps[0]?.title;
    if (first && first !== task.title) {
      card.append(element('p', `最初の一手：${first}`, 'first-step'));
    }
    card.append(
      element('p', `${Math.ceil(plannedSeconds(task) / 60)}分の作業`),
      element('strong', '完了で視聴時間 ＋20分'),
    );
    panel.append(card);
    const actions = element('div', '', 'actions');
    actions.append(
      button(
        '作業をはじめる',
        () => {
          void action({ type: 'START', taskId: task.id }, panel).then((reply) => {
            if (reply) {
              dismiss();
              if (task.url) window.open(task.url, '_blank', 'noopener');
            }
          });
        },
        'primary',
      ),
    );
    actions.append(
      button(`${snapshot.settings.snoozeMinutes}分スヌーズ・予定を確認`, () => {
        void showDraft(
          task,
          false,
          snapshot.nextDraft?.taskId === task.id ? snapshot.nextDraft : undefined,
        );
      }),
    );
    actions.append(
      button('予定を調整する', () => {
        void showDraft(task, true);
      }),
    );
    actions.append(button('あとで', dismiss));
    panel.append(actions);
    overlay.append(panel);
    mount().append(overlay);
    overlay.oncancel = dismiss;
    overlay.showModal();
    actions.querySelector('button')?.focus();
    if (snapshot.settings.sound) {
      const audio = new AudioContext();
      const oscillator = audio.createOscillator();
      const gain = audio.createGain();
      gain.gain.setValueAtTime(0.035, audio.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.25);
      oscillator.frequency.value = 660;
      oscillator.connect(gain).connect(audio.destination);
      oscillator.start();
      oscillator.stop(audio.currentTime + 0.25);
      oscillator.onended = () => {
        void audio.close();
      };
    }
    return true;
  }
  function local(value: string) {
    const date = new Date(value);
    return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }
  async function showDraft(task: Task, adjust: boolean, candidate?: Draft) {
    dismiss();
    overlay = element('dialog', '', 'overlay simple');
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', '変更する予定を確認');
    const panel = element('div', '', 'panel plan-panel');
    panel.append(
      element('h2', adjust ? '予定を調整' : 'スヌーズ後の予定を確認'),
      element('p', 'OKを押すまで、予定は変わりません。', 'muted'),
    );
    overlay.append(panel);
    mount().append(overlay);
    overlay.oncancel = dismiss;
    overlay.showModal();
    let draft = candidate ? structuredClone(candidate) : undefined;
    if (!draft) {
      const reply = await action({ type: 'DRAFT', taskId: task.id, adjust }, panel);
      draft = reply?.draft;
    }
    if (!draft) {
      panel.append(button('戻る', () => showNotice(task, state!)));
      return;
    }
    function render() {
      panel.querySelector('.draft-content')?.remove();
      const content = element('div', '', 'draft-content');
      const current = draft!;
      content.append(element('p', current.reason, 'info'));
      current.changes.forEach((change, index) => {
        const section = element('section', '', 'change');
        const heading = element('div', '', 'change-title');
        heading.append(element('h3', change.title));
        if (index > 0)
          heading.append(
            button(
              '↑ 順序を上へ',
              () => {
                const start = Date.parse(current.changes[0].after.start);
                [current.changes[index - 1], current.changes[index]] = [
                  current.changes[index],
                  current.changes[index - 1],
                ];
                let cursor = start;
                for (const c of current.changes) {
                  const delta = cursor - Date.parse(c.after.start);
                  c.after = {
                    ...c.after,
                    start: new Date(cursor).toISOString(),
                    end: new Date(Date.parse(c.after.end) + delta).toISOString(),
                    steps: c.after.steps.map((s) => ({
                      ...s,
                      start: new Date(Date.parse(s.start) + delta).toISOString(),
                      end: new Date(Date.parse(s.end) + delta).toISOString(),
                    })),
                  };
                  cursor = Date.parse(c.after.end);
                }
                render();
              },
              'small',
            ),
          );
        section.append(
          heading,
          element(
            'p',
            `変更前 ${new Date(change.before.start).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })}–${new Date(change.before.end).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })} · 元の見積もり ${change.originalMinutes}分`,
            'muted',
          ),
        );
        const row = element('div', '', 'time-row');
        for (const field of ['start', 'end'] as const) {
          const label = element('label', field === 'start' ? '開始' : '終了');
          const input = element('input');
          input.type = 'datetime-local';
          input.value = local(change.after[field]);
          input.onchange = () => {
            if (!input.value) return;
            const previous = structuredClone(change.after);
            change.after[field] = new Date(input.value).toISOString();
            const ratio =
              (Date.parse(change.after.end) - Date.parse(change.after.start)) /
              (Date.parse(previous.end) - Date.parse(previous.start));
            let cursor = Date.parse(change.after.start);
            change.after.steps = previous.steps.map((step, i) => {
              const end =
                i === previous.steps.length - 1
                  ? Date.parse(change.after.end)
                  : cursor + Math.round((Date.parse(step.end) - Date.parse(step.start)) * ratio);
              const result = {
                title: step.title,
                start: new Date(cursor).toISOString(),
                end: new Date(end).toISOString(),
              };
              cursor = end;
              return result;
            });
          };
          label.append(input);
          row.append(label);
        }
        section.append(row);
        for (const step of change.after.steps) section.append(element('p', step.title, 'step'));
        content.append(section);
      });
      const instruction = element('textarea');
      instruction.placeholder = 'Geminiへの指示（例：見直しに5分回して）';
      instruction.setAttribute('aria-label', 'Geminiに予定の調整を指示');
      content.append(
        instruction,
        button(
          'Geminiで候補を作り直す',
          () => {
            if (instruction.value.trim())
              void action(
                { type: 'ADJUST', draft: current, instruction: instruction.value },
                panel,
              ).then((reply) => {
                if (reply?.draft) {
                  draft = reply.draft;
                  render();
                }
              });
          },
          'secondary',
        ),
      );
      const actions = element('div', '', 'actions');
      actions.append(
        button(
          'OK・この予定に変更',
          () => {
            void action({ type: 'COMMIT', draft: current }, panel).then((reply) => {
              if (reply) dismiss();
            });
          },
          'primary',
        ),
        button('キャンセル', () => showNotice(task, state!)),
      );
      content.append(actions);
      panel.append(content);
    }
    render();
  }
  function renderTimer() {
    timer?.remove();
    timer = undefined;
    if (!state?.session || location.origin === MANAGEMENT_ORIGIN) return;
    const task = state.tasks.find((t) => t.id === state!.session!.taskId);
    if (!task) return;
    timer = element('section', '', 'timer');
    timer.setAttribute('aria-label', 'DOPAGAKI 作業タイマー');
    const title = element('strong', task.title);
    const time = element('span', '', 'elapsed');
    const pause = button(state.session.pausedAt ? '再開' : '一時停止', () => {
      void action({ type: 'PAUSE' }, timer!);
    });
    const complete = button(
      '完了 ＋20分',
      () => {
        void action({ type: 'COMPLETE', taskId: task.id }, timer!).then((reply) => {
          if (reply) showToast('課題完了！視聴時間 ＋20分');
        });
      },
      'primary',
    );
    const progress = element('progress');
    progress.max = plannedSeconds(task);
    progress.setAttribute('aria-label', '作業の進捗');
    timer.append(title, time, pause, complete, progress);
    mount().append(timer);
    updateTimer();
  }
  function updateTimer() {
    if (!timer || !state?.session) return;
    const elapsed = elapsedSeconds(state.session);
    timer.querySelector('.elapsed')!.textContent = displayTime(elapsed);
    timer.querySelector('progress')!.value = elapsed;
  }
  function showToast(text: string) {
    const toast = element('div', text, 'toast');
    mount().append(toast);
    setTimeout(() => toast.remove(), 4500);
  }
  chrome.runtime.onMessage.addListener((message, _sender, respond) => {
    if (message.type === 'SHOW_NOTICE') {
      respond({ shown: showNotice(message.task, message.state) });
    }
    if (message.type === 'STATE_UPDATED') {
      if (!message.state) {
        dismiss();
        consumed = 0;
        receipt = undefined;
        previousPlaying = false;
      }
      state = message.state;
      renderTimer();
    }
  });
  void send({ type: 'STATE' })
    .then((reply) => {
      if (reply.state) {
        state = reply.state;
        renderTimer();
      }
    })
    .catch((error) => console.debug('DOPAGAKI connection unavailable', error));
  setInterval(() => {
    updateTimer();
    const now = performance.now();
    const delta = Math.min(2, (now - lastTick) / 1000);
    lastTick = now;
    const video = shortsVideo();
    const playing =
      shorts() &&
      document.visibilityState === 'visible' &&
      document.hasFocus() &&
      !!video &&
      !video.paused &&
      !video.ended &&
      video.readyState >= 2 &&
      !overlay;
    if (playing && previousPlaying) consumed += delta;
    previousPlaying = playing;
    if (state && state.balanceSeconds - Math.floor(consumed) <= 0 && playing) {
      video?.pause();
      showToast('視聴時間がなくなりました。課題を登録・完了して時間を増やしましょう。');
    }
    if (
      !requestPending &&
      Date.now() >= retryAfter &&
      (receipt || consumed >= 10 || (!playing && consumed >= 1))
    ) {
      receipt ??= { id: crypto.randomUUID(), seconds: Math.min(60, Math.floor(consumed)) };
      const current = receipt;
      requestPending = true;
      void send({ type: 'VIEWING', ...current })
        .then((reply) => {
          if (!reply.error && reply.state) {
            consumed = Math.max(0, consumed - current.seconds);
            receipt = undefined;
            state = reply.state;
          } else retryAfter = Date.now() + 10000;
        })
        .catch(() => {
          retryAfter = Date.now() + 10000;
        })
        .finally(() => {
          requestPending = false;
        });
    }
  }, 1000);
}
