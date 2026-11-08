// 予定の変更案を確認する画面。OK を押すまで予定は変わらない。
import type { Draft } from '@dopagaki/contract';
import { planReasonLabels } from '@dopagaki/messages';
import { copy, timeLabel } from '../lib/copy';
import { fromLocalInput, moveUp, retime, toLocalInput } from '../lib/draft-edit';
import type { ExtensionTask } from '../lib/schemas';
import { runAction, showError } from './action';
import { button, el } from './dom';
import { overlayClass, type OverlayHost } from './overlay';

let fieldId = 0;

export async function showDraft(
  host: OverlayHost,
  task: ExtensionTask,
  mode: 'snooze' | 'adjust',
  candidate: Draft | undefined,
  back: () => void,
): Promise<void> {
  const dialog = host.open('draft', {
    label: copy.draftLabel,
    className: `${overlayClass(host.context.state?.settings ?? null, 'simple')} draft`,
    panelClassName: 'panel plan-panel',
  });
  const { panel } = dialog;
  const heading = el('h2', {
    text: mode === 'adjust' ? copy.draftAdjustHeading : copy.draftSnoozeHeading,
  });
  panel.append(heading, el('p', { className: 'muted', text: copy.draftNotice }));
  heading.tabIndex = -1;
  dialog.focusFirst(heading);

  let draft = candidate ? structuredClone(candidate) : undefined;
  if (!draft) {
    const reply = await runAction(panel, { type: 'GET_DRAFT', taskId: task.id, mode });
    draft = reply?.ok ? reply.draft : undefined;
  }
  if (host.current !== 'draft' || !dialog.element.isConnected) return;
  if (!draft) {
    const backButton = button(copy.back, back);
    panel.append(el('div', { className: 'actions' }, backButton));
    backButton.focus();
    return;
  }

  const render = (current: Draft) => {
    panel.querySelector(':scope > .draft-content')?.remove();
    const content = el('div', { className: 'draft-content' });
    const reason = current.changes[0]?.after.reason;
    if (reason) content.append(el('p', { className: 'info', text: planReasonLabels[reason] }));
    if (current.shortened)
      content.append(el('p', { className: 'info', text: copy.draftShortened }));

    current.changes.forEach((change, index) => {
      const section = el('section', { className: 'change' });
      const title = el('div', { className: 'change-title' }, el('h3', { text: change.title }));
      if (index > 0) {
        title.append(
          button(
            copy.moveUp,
            () => {
              render({ ...current, changes: moveUp(current.changes, index) });
            },
            'text',
            copy.moveUpLabel(change.title),
          ),
        );
      }
      section.append(
        title,
        el('p', {
          className: 'muted',
          text: copy.before(
            timeLabel(change.before.start),
            timeLabel(change.before.end),
            change.originalMinutes,
          ),
        }),
      );
      const row = el('div', { className: 'time-row' });
      for (const field of ['start', 'end'] as const) {
        const id = `dpg-field-${String(++fieldId)}`;
        const input = el('input', { attrs: { id, type: 'datetime-local' } });
        input.value = toLocalInput(change.after[field]);
        input.addEventListener('change', () => {
          const value = fromLocalInput(input.value);
          const next = value ? retime(change.after, field, value) : undefined;
          if (!next) {
            showError(section, copy.invalidTime);
            input.value = toLocalInput(change.after[field]);
            return;
          }
          section.querySelector(':scope > .error')?.remove();
          change.after = next;
        });
        row.append(
          el(
            'div',
            { className: 'field' },
            el('label', { text: field === 'start' ? copy.start : copy.end, attrs: { for: id } }),
            input,
          ),
        );
      }
      section.append(row);
      const steps = el('ul', { className: 'steps' });
      for (const step of change.after.steps) steps.append(el('li', { text: step.title }));
      section.append(steps);
      content.append(section);
    });

    const instructionId = `dpg-field-${String(++fieldId)}`;
    const instruction = el('textarea', {
      attrs: { id: instructionId, placeholder: copy.instructionPlaceholder, maxlength: '2000' },
    });
    content.append(
      el('label', {
        className: 'field-label',
        text: copy.instructionLabel,
        attrs: { for: instructionId },
      }),
      instruction,
      button(
        copy.regenerate,
        () => {
          const text = instruction.value.trim();
          if (!text) {
            instruction.focus();
            return;
          }
          void runAction(panel, { type: 'ADJUST_DRAFT', draft: current, instruction: text }).then(
            (reply) => {
              if (reply?.ok && reply.draft) {
                draft = reply.draft;
                render(reply.draft);
              }
            },
          );
        },
        'tonal',
      ),
    );

    const actions = el('div', { className: 'actions' });
    actions.append(
      button(
        copy.commit,
        () => {
          void runAction(panel, { type: 'COMMIT_DRAFT', draft: current }).then(async (reply) => {
            if (!reply?.ok) return;
            host.context.apply(reply);
            host.close();
            if (mode === 'snooze') host.context.toast(await host.context.line('snooze', task.id));
          });
        },
        'filled',
      ),
      button(copy.cancel, back, 'text'),
    );
    content.append(actions);
    panel.append(content);
  };
  render(draft);
}
