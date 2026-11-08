// Shorts の検出（DOM を読む部分）。判定そのものは lib/shorts の純粋な関数で行う。
import { canMeasure, isShortsUrl, pickVideo } from '../lib/shorts';

export function onShorts(): boolean {
  return isShortsUrl(location.href);
}

/** 計測する動画。表示中の Shorts の動画を優先する。 */
export function currentVideo(): HTMLVideoElement | undefined {
  const active =
    document.querySelector<HTMLVideoElement>('ytd-reel-video-renderer[is-active] video') ??
    undefined;
  return pickVideo(active, [...document.querySelectorAll('video')]);
}

/** 今、視聴時間として数えてよいか。 */
export function measurable(video: HTMLVideoElement | undefined, overlayOpen: boolean): boolean {
  return canMeasure({
    url: location.href,
    visible: document.visibilityState === 'visible',
    focused: document.hasFocus(),
    video,
    overlayOpen,
  });
}
