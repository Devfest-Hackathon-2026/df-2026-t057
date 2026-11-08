/**
 * FullCalendar の CSS（angular.json で `inject: false` にした `fullcalendar.css`）を、カレンダーを開いたときだけ読み込む。
 * 最初の画面を軽くするため、グローバルのスタイルには入れない。
 */
let loading: Promise<void> | undefined;

export function loadFullCalendarStyles(): Promise<void> {
  loading ??= new Promise<void>((resolve) => {
    const existing = document.querySelector<HTMLLinkElement>('link[data-fullcalendar]');
    if (existing?.sheet) {
      resolve();
      return;
    }
    const link = existing ?? document.createElement('link');
    link.rel = 'stylesheet';
    link.href = 'fullcalendar.css';
    link.dataset['fullcalendar'] = '';
    // 読み込めなくても、カレンダー自体は表示する。
    link.addEventListener('load', () => {
      resolve();
    });
    link.addEventListener('error', () => {
      resolve();
    });
    if (!existing) document.head.append(link);
  });
  return loading;
}
