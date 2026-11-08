// YouTube の上に出すダイアログを1つだけ管理する。
import type { NoticeMode, Settings } from '@dopagaki/contract';
import type { ContentContext } from '../content/context';
import { defaultSettings } from '../lib/schemas';
import { openDialog, type Dialog, type DialogOptions } from './dialog';

export type OverlayKind = 'choice' | 'notice' | 'draft' | 'night';

export interface OverlayHost {
  readonly context: ContentContext;
  /** 今のダイアログを閉じて、新しいダイアログを開く。動画は止める。 */
  open(kind: OverlayKind, options: DialogOptions): Dialog;
  close(): void;
  readonly current: OverlayKind | undefined;
  /** 計測している動画。 */
  video(): HTMLVideoElement | undefined;
  /** ダイアログを閉じたあと、動画の再生を再開する（自動再生が止められたら案内を出す）。 */
  resumePlayback(): void;
}

/** 通知の見た目（モード）と、動きを減らす設定をクラスにする。 */
export function overlayClass(settings: Settings | null, mode?: NoticeMode): string {
  const current = settings ?? defaultSettings;
  return ['overlay', mode ?? current.mode, current.reducedMotion ? 'reduced' : '']
    .filter(Boolean)
    .join(' ');
}

export function createOverlayHost(
  context: ContentContext,
  video: () => HTMLVideoElement | undefined,
  autoplayBlocked: () => void,
): OverlayHost {
  let dialog: Dialog | undefined;
  let kind: OverlayKind | undefined;
  const host: OverlayHost = {
    context,
    open(next, options) {
      dialog?.close();
      video()?.pause();
      kind = next;
      const opened = openDialog(context.root, {
        ...options,
        onClose: () => {
          if (dialog === opened) {
            dialog = undefined;
            kind = undefined;
          }
          options.onClose?.();
        },
      });
      dialog = opened;
      return opened;
    },
    close() {
      dialog?.close();
      dialog = undefined;
      kind = undefined;
    },
    get current() {
      return kind;
    },
    video,
    resumePlayback() {
      void video()
        ?.play()
        .catch(() => {
          autoplayBlocked();
        });
    },
  };
  return host;
}
