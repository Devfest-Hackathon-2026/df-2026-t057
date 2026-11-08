import { describe, expect, it } from 'vitest';
import {
  canMeasure,
  isShortsUrl,
  isVideoPlaying,
  isYouTubeUrl,
  pickVideo,
  type VideoLike,
} from './shorts';

const playing: VideoLike = { paused: false, ended: false, readyState: 4 };
const conditions = {
  url: 'https://www.youtube.com/shorts/abc',
  visible: true,
  focused: true,
  video: playing,
  overlayOpen: false,
};

describe('isShortsUrl', () => {
  it.each([
    ['https://www.youtube.com/shorts/abc', true],
    ['https://www.youtube.com/shorts/abc?feature=share', true],
    ['https://www.youtube.com/watch?v=abc', false],
    ['https://m.youtube.com/shorts/abc', false],
    ['http://www.youtube.com/shorts/abc', false],
    ['https://www.youtube.com.evil.example/shorts/abc', false],
    ['not a url', false],
  ])('%s → %s', (url, expected) => {
    expect(isShortsUrl(url)).toBe(expected);
  });
});

describe('isYouTubeUrl', () => {
  it.each([
    ['https://www.youtube.com/', true],
    ['https://www.youtube.com/shorts/abc', true],
    ['https://youtube.com.evil.example/', false],
    ['nope', false],
  ])('%s → %s', (url, expected) => {
    expect(isYouTubeUrl(url)).toBe(expected);
  });
});

describe('isVideoPlaying', () => {
  it.each([
    [undefined, false],
    [playing, true],
    [{ ...playing, paused: true }, false],
    [{ ...playing, ended: true }, false],
    [{ ...playing, readyState: 1 }, false],
    [{ ...playing, readyState: 2 }, true],
  ])('%o → %s', (video, expected) => {
    expect(isVideoPlaying(video)).toBe(expected);
  });
});

describe('canMeasure', () => {
  it('counts only when every condition holds', () => {
    expect(canMeasure(conditions)).toBe(true);
  });

  it.each([
    { url: 'https://www.youtube.com/' },
    { visible: false },
    { focused: false },
    { overlayOpen: true },
    { video: undefined },
    { video: { ...playing, paused: true } },
  ])('does not count with %o', (override) => {
    expect(canMeasure({ ...conditions, ...override })).toBe(false);
  });
});

describe('pickVideo', () => {
  const paused = { paused: true, ended: false, readyState: 4 };
  it('prefers the active Shorts video', () => {
    expect(pickVideo(paused, [playing])).toBe(paused);
  });
  it('falls back to the playing video, then the first one', () => {
    expect(pickVideo(undefined, [paused, playing])).toBe(playing);
    expect(pickVideo(undefined, [paused])).toBe(paused);
    expect(pickVideo<VideoLike>(undefined, [])).toBeUndefined();
  });
});
