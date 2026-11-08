// Shorts の検出と、視聴時間を数えてよいかの判定。DOM に依存しない純粋な関数だけを置く。

/** 視聴時間を数える対象のページか。 */
export function isShortsUrl(url: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  return (
    parsed.protocol === 'https:' &&
    parsed.hostname === 'www.youtube.com' &&
    parsed.pathname.startsWith('/shorts/')
  );
}

/** YouTube のページか（Shorts から別のページへ移った直後の報告を受け付けるのに使う）。 */
export function isYouTubeUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' && parsed.hostname === 'www.youtube.com';
  } catch {
    return false;
  }
}

/** 判定に使う動画の状態（HTMLVideoElement の一部）。 */
export interface VideoLike {
  readonly paused: boolean;
  readonly ended: boolean;
  /** HTMLMediaElement.readyState。2（HAVE_CURRENT_DATA）以上で再生できる。 */
  readonly readyState: number;
}

export function isVideoPlaying(video: VideoLike | undefined): boolean {
  return !!video && !video.paused && !video.ended && video.readyState >= 2;
}

export interface MeasureConditions {
  readonly url: string;
  readonly visible: boolean;
  readonly focused: boolean;
  readonly video: VideoLike | undefined;
  readonly overlayOpen: boolean;
}

/**
 * 視聴時間として数えてよいか。
 * Shorts を開いていて、ページが見えていて、ウィンドウにフォーカスがあり、動画が実際に再生中で、
 * 拡張の画面（確認のダイアログ）を出していないときだけ数える。
 */
export function canMeasure(conditions: MeasureConditions): boolean {
  return (
    isShortsUrl(conditions.url) &&
    conditions.visible &&
    conditions.focused &&
    !conditions.overlayOpen &&
    isVideoPlaying(conditions.video)
  );
}

/**
 * 計測する動画を選ぶ。表示中の Shorts の動画を優先し、無ければ再生中の動画、最後に最初の動画。
 * `active` は `ytd-reel-video-renderer[is-active] video` の結果。
 */
export function pickVideo<T extends VideoLike>(
  active: T | undefined,
  all: readonly T[],
): T | undefined {
  return active ?? all.find((video) => !video.paused && !video.ended) ?? all[0];
}
