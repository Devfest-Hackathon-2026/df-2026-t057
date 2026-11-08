import {
  Draft,
  displayTime,
  elapsedSeconds,
  nightWindow,
  plannedSeconds,
  workLimitSeconds,
  rewardDescription,
  workRewardMessage,
  viewingAvailable,
} from '../../shared/models';
import {
  ExtensionState,
  ExtensionTask,
  MANAGEMENT_ORIGIN,
  Moment,
  Reply,
  Request,
  send,
} from './protocol';

const runtimeWindow = window as Window & { dopagakiLoaded?: boolean };
if (!runtimeWindow.dopagakiLoaded) {
  runtimeWindow.dopagakiLoaded = true;
  initialize();
}
function initialize() {
  let state: ExtensionState | undefined;
  let overlay: HTMLDialogElement | undefined;
  let root: ShadowRoot | undefined;
  let timer: HTMLElement | undefined;
  let requestPending = false;
  let consumed = 0;
  let receipt:
    { id: string; seconds: number; sessionId?: string; consumedBefore?: number } | undefined;
  let lastTick = performance.now();
  let previousPlaying = false;
  let retryAfter = 0;
  let ownsViewing = false;
  let viewingBar: HTMLElement | undefined;
  let stopRequested: string | undefined;
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
        void send({ type: 'DISCONNECT', accountId: event.data.accountId });
        return;
      }
      if (event.data?.type === 'DOPAGAKI_DIAGNOSE' && typeof event.data.requestId === 'string') {
        const requestId = event.data.requestId;
        void send({ type: 'DIAGNOSTICS' }).then((reply) =>
          window.postMessage(
            { type: 'DOPAGAKI_DIAGNOSTICS', requestId, ...reply },
            MANAGEMENT_ORIGIN,
          ),
        );
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
        if (reply.tabId !== undefined) ownsViewing = reply.tabId === reply.viewingOwnerTabId;
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
  function showViewingChoice(offline = false) {
    if (!state || overlay) return;
    shortsVideo()?.pause();
    const current = state.viewingSession;
    const active = current && !current.endedAt;
    overlay = element('dialog', '', 'overlay simple reduced');
    overlay.setAttribute('aria-label', offline ? '視聴時間の接続を確認' : '視聴の区切り');
    const panel = element('div', '', 'panel');
    panel.append(
      element(
        'h2',
        offline
          ? '接続を確認してから再開しましょう'
          : active
            ? '同じ区切りを引き継ぎますか'
            : current
              ? '決めた区切りになりました'
              : '今回は、何分見ますか',
      ),
    );
    const available = viewingAvailable(state);
    panel.append(
      element(
        'p',
        state.timePolicy?.mode === 'reminder'
          ? '見る長さを決めてから再生します。'
          : `今日の残り枠 ${displayTime(available)}。再生中の時間だけを数えます。`,
      ),
    );
    const actions = element('div', '', 'actions');
    const resume = (request: Request) => {
      void action(request, panel).then((reply) => {
        if (!reply) return;
        dismiss();
        previousPlaying = false;
        void shortsVideo()
          ?.play()
          .catch(() => showToast('動画の再生ボタンから再開できます。'));
      });
    };
    if (offline) {
      actions.append(button('再接続して続ける', () => resume({ type: 'STATE' }), 'primary'));
    } else if (active && available > 0) {
      actions.append(
        button(
          `このタブで続ける（残り${displayTime(current.plannedSeconds - current.consumedSeconds)}）`,
          () => resume({ type: 'CLAIM_VIEWING', sessionId: current.id }),
          'primary',
        ),
      );
    } else if (available > 0) {
      for (const seconds of current ? [300] : [300, 600]) {
        const id = crypto.randomUUID();
        actions.append(
          button(
            current
              ? '残り枠からもう5分'
              : `${seconds / 60}分にする${available < seconds ? `（残り${displayTime(available)}まで）` : ''}`,
            () => resume({ type: 'START_VIEWING', id, seconds, extendFrom: current?.id }),
            seconds === 300 ? 'primary' : '',
          ),
        );
      }
    } else
      panel.append(element('p', '今日の視聴枠を使い切りました。休憩や次の作業へ移りましょう。'));
    actions.append(
      button(current && !active ? 'ここで終える' : '今は見ない', () => {
        if (active && ownsViewing) void stopViewing(panel);
        else dismiss();
      }),
    );
    actions.append(
      button('次の作業へ', () => {
        void send({ type: 'OPEN_APP' });
        dismiss();
      }),
    );
    panel.append(actions);
    overlay.append(panel);
    mount().append(overlay);
    overlay.oncancel = dismiss;
    overlay.showModal();
  }
  function showNotice(task: ExtensionTask, snapshot: ExtensionState, line = '') {
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
    if (line) panel.append(element('p', line, 'companion'));
    const card = element('div', '', 'task-card');
    card.append(element('h2', task.title));
    if (task.why) card.append(element('p', `なぜ：${task.why}`, 'why'));
    const first = task.plan.steps[0]?.title;
    if (first && first !== task.title) {
      card.append(element('p', `最初の一手：${first}`, 'first-step'));
    }
    card.append(
      element('p', `${Math.ceil(plannedSeconds(task) / 60)}分の作業`),
      element('strong', rewardDescription(snapshot)),
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
  function showNight(snapshot: ExtensionState, line = '') {
    if (!shorts() || document.visibilityState !== 'visible' || overlay) return false;
    state = snapshot;
    shortsVideo()?.pause();
    overlay = element('dialog', '', 'overlay simple night');
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', '寝る時間が近づいています');
    const panel = element('div', '', 'panel');
    panel.append(element('p', 'GOOD NIGHT', 'eyebrow'), element('h1', 'そろそろ、寝る準備。'));
    if (line) panel.append(element('p', line, 'companion'));
    panel.append(
      element(
        'p',
        `就寝 ${snapshot.sleep?.bedtime} ・ 起床 ${snapshot.sleep?.wake}。今夜の課題の報酬は、朝に受け取れます。`,
        'muted',
      ),
      element('strong', 'このまま寝ると、朝に視聴時間 最大15分'),
    );
    const actions = element('div', '', 'actions');
    actions.append(
      button('Shortsを閉じて寝る', () => void send({ type: 'CLOSE_TAB' }), 'primary'),
      button('あと少しだけ', dismiss),
    );
    panel.append(actions);
    overlay.append(panel);
    mount().append(overlay);
    overlay.oncancel = dismiss;
    overlay.showModal();
    actions.querySelector('button')?.focus();
    return true;
  }
  function local(value: string) {
    const date = new Date(value);
    return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }
  async function showDraft(task: ExtensionTask, adjust: boolean, candidate?: Draft) {
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
            void action(
              { type: 'COMMIT', draft: { ...current, intent: adjust ? 'adjust' : 'snooze' } },
              panel,
            ).then((reply) => {
              if (!reply) return;
              dismiss();
              if (!adjust)
                void say(
                  'snooze',
                  '了解、少し後ろにずらそう。次の時間に、また声かけるね。',
                  task.id,
                ).then(showToast);
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
      '課題を完了',
      () => {
        const before = state!;
        void action({ type: 'COMPLETE', taskId: task.id }, timer!).then((reply) => {
          if (reply?.state) {
            const reward = workRewardMessage(
              Math.max(0, reply.state.workRewardSeconds - before.workRewardSeconds),
              !!state?.sleep && !!nightWindow(state.sleep, state.timeZone),
            );
            void say('complete', '', task.id).then((line) =>
              showToast(line ? `${line}（${reward}）` : reward),
            );
          }
        });
      },
      'primary',
    );
    const progress = element('progress');
    progress.max = workLimitSeconds(state, task);
    progress.setAttribute('aria-label', '作業の進捗');
    timer.append(title, time, pause);
    if (state.timePolicy)
      timer.append(
        button('ここまで取り組んだ', () => {
          const before = state!;
          void action({ type: 'CONFIRM_WORK', id: state!.session!.id! }, timer!).then((reply) => {
            if (reply?.state)
              showToast(
                workRewardMessage(
                  Math.max(0, reply.state.workRewardSeconds - before.workRewardSeconds),
                  !!state?.sleep && !!nightWindow(state.sleep, state.timeZone),
                ),
              );
          });
        }),
      );
    timer.append(complete, progress);
    mount().append(timer);
    updateTimer();
  }
  function updateViewingBar() {
    const current = state?.viewingSession;
    if (
      !shorts() ||
      !current ||
      current.endedAt ||
      !ownsViewing ||
      !state?.timePolicy ||
      state.timePolicy.mode === 'self'
    ) {
      viewingBar?.remove();
      viewingBar = undefined;
      return;
    }
    if (!viewingBar) {
      viewingBar = element('section', '', 'viewing-bar');
      viewingBar.setAttribute('aria-label', '視聴タイマー');
      viewingBar.append(
        element('span', '', 'remaining'),
        button('ここで終える', () => {
          void stopViewing(viewingBar!);
        }),
      );
      mount().append(viewingBar);
    }
    viewingBar.querySelector('.remaining')!.textContent =
      `今回あと ${displayTime(current.plannedSeconds - current.consumedSeconds - consumed)}`;
    viewingBar.querySelector('button')!.disabled = !!stopRequested || requestPending;
  }
  async function saveViewing(): Promise<boolean> {
    if (requestPending) return false;
    receipt ??= {
      id: crypto.randomUUID(),
      seconds: Math.min(60, Math.floor(consumed)),
      sessionId: state?.viewingSession?.id,
      consumedBefore: state?.viewingSession?.consumedSeconds,
    };
    const current = receipt;
    requestPending = true;
    try {
      const reply = await send({
        type: 'VIEWING',
        id: current.id,
        seconds: current.seconds,
        sessionId: current.sessionId,
      });
      if (reply.error || !reply.state) throw new Error(reply.error);
      const saved = reply.state.viewingSession;
      const charged =
        current.consumedBefore !== undefined &&
        saved &&
        saved.id === current.sessionId &&
        reply.state.timePolicy?.mode !== 'self'
          ? Math.max(0, Math.min(current.seconds, saved.consumedSeconds - current.consumedBefore))
          : current.seconds;
      consumed = Math.max(0, consumed - charged);
      receipt = undefined;
      state = reply.state;
      if (state.timePolicy && state.viewingSession?.endedAt) {
        consumed = 0;
        if (!overlay && !stopRequested) showViewingChoice();
      }
      return true;
    } catch {
      retryAfter = Date.now() + 10000;
      shortsVideo()?.pause();
      showViewingChoice(true);
      return false;
    } finally {
      requestPending = false;
    }
  }
  async function stopViewing(container: HTMLElement) {
    if (requestPending || stopRequested || !state?.viewingSession) return;
    stopRequested = state.viewingSession.id;
    shortsVideo()?.pause();
    previousPlaying = false;
    updateViewingBar();
    try {
      while (receipt || consumed >= 1) {
        if (!(await saveViewing())) return;
        if (consumed >= 1) await new Promise((resolve) => setTimeout(resolve, 1000));
      }
      if (
        await action({ type: 'END_VIEWING', sessionId: stopRequested, reason: 'stop' }, container)
      ) {
        dismiss();
        showToast('決めたところで、視聴を終えました。');
      }
    } finally {
      stopRequested = undefined;
      updateViewingBar();
    }
  }
  function updateTimer() {
    if (!timer || !state?.session) return;
    const elapsed = elapsedSeconds(state.session);
    timer.querySelector('.elapsed')!.textContent =
      `${displayTime(Math.min(elapsed, timer.querySelector('progress')!.max))} / ${displayTime(timer.querySelector('progress')!.max)}`;
    timer.querySelector('progress')!.value = elapsed;
  }
  let balanceLine: Promise<string> | undefined;
  async function say(moment: Moment, fallback: string, taskId?: string) {
    try {
      return (await send({ type: 'LINE', moment, taskId })).line || fallback;
    } catch {
      return fallback;
    }
  }
  function showToast(text: string) {
    const toast = element('div', text, 'toast');
    toast.setAttribute('role', 'status');
    mount().append(toast);
    setTimeout(() => toast.remove(), 4500);
  }
  chrome.runtime.onMessage.addListener((message, _sender, respond) => {
    if (message.type === 'SHOW_NOTICE') {
      respond({ shown: showNotice(message.task, message.state, message.line) });
    }
    if (message.type === 'SHOW_NIGHT') {
      respond({ shown: showNight(message.state, message.line) });
    }
    if (message.type === 'STATE_UPDATED') {
      if (!message.state) {
        dismiss();
        consumed = 0;
        receipt = undefined;
        previousPlaying = false;
      }
      state = message.state;
      ownsViewing = message.tabId !== undefined && message.tabId === message.viewingOwnerTabId;
      renderTimer();
    }
  });
  void send({ type: 'STATE' })
    .then((reply) => {
      if (reply.state) {
        state = reply.state;
        ownsViewing = reply.tabId !== undefined && reply.tabId === reply.viewingOwnerTabId;
        renderTimer();
      }
    })
    .catch((error) => console.debug('DOPAGAKI connection unavailable', error));
  setInterval(() => {
    updateTimer();
    updateViewingBar();
    const now = performance.now();
    const delta = Math.min(2, (now - lastTick) / 1000);
    lastTick = now;
    const video = shortsVideo();
    let playing =
      shorts() &&
      document.visibilityState === 'visible' &&
      document.hasFocus() &&
      !!video &&
      !video.paused &&
      !video.ended &&
      video.readyState >= 2 &&
      !overlay;
    if (playing && stopRequested) {
      video?.pause();
      playing = false;
    }
    if (
      playing &&
      state?.timePolicy &&
      state.timePolicy.mode !== 'self' &&
      (!state.viewingSession ||
        state.viewingSession.endedAt ||
        !ownsViewing ||
        viewingAvailable(state) <= 0)
    ) {
      video?.pause();
      playing = false;
      showViewingChoice();
    }
    if (playing && previousPlaying) consumed += delta;
    previousPlaying = playing;
    const sessionRemaining = state?.viewingSession
      ? state.viewingSession.plannedSeconds - state.viewingSession.consumedSeconds
      : Infinity;
    if (
      state &&
      playing &&
      state.timePolicy?.mode !== 'self' &&
      Math.min(viewingAvailable(state), state.timePolicy ? sessionRemaining : Infinity) -
        Math.floor(consumed) <=
        0
    ) {
      video?.pause();
      playing = false;
      previousPlaying = false;
      balanceLine ??= say(
        'balance_zero',
        '今日の視聴枠を使い切りました。休憩や次の作業へ移りましょう。',
      );
      void balanceLine.then(showToast);
    }
    if (
      !requestPending &&
      !stopRequested &&
      Date.now() >= retryAfter &&
      (receipt || consumed >= 10 || (!playing && consumed >= 1))
    ) {
      void saveViewing();
    }
  }, 1000);
}
