// 通知の短い音（設定で有効にしたときだけ）。660 Hz を 0.25 秒。
export function playChime(): void {
  try {
    const audio = new AudioContext();
    const oscillator = audio.createOscillator();
    const gain = audio.createGain();
    gain.gain.setValueAtTime(0.035, audio.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.25);
    oscillator.frequency.value = 660;
    oscillator.connect(gain).connect(audio.destination);
    oscillator.start();
    oscillator.stop(audio.currentTime + 0.25);
    oscillator.addEventListener('ended', () => {
      void audio.close();
    });
  } catch {
    // 音を出せない環境では鳴らさない。
  }
}
