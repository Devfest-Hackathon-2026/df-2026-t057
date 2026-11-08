const output = document.querySelector<HTMLElement>('#status')!;
const retry = document.querySelector<HTMLButtonElement>('#retry')!;
let completed = false;
let timer: ReturnType<typeof setTimeout> | undefined;
async function check() {
  clearTimeout(timer);
  const receipt = sessionStorage.getItem('dopagaki-deletion-receipt');
  if (!receipt) {
    output.textContent = 'このブラウザーに削除の受付情報がありません。';
    retry.disabled = true;
    return;
  }
  retry.disabled = true;
  try {
    const response = await fetch('/api/account/deletion-status', {
      headers: { Authorization: `Deletion ${receipt}` },
    });
    if (!response.ok) throw new Error('確認できませんでした。時間をおいて再試行してください。');
    const value = await response.json();
    if (value.status === 'completed') {
      completed = true;
      output.textContent = 'アプリに保存したデータとアカウントの削除が完了しました。';
      sessionStorage.removeItem('dopagaki-deletion-receipt');
      return;
    }
    output.textContent = value.retrying
      ? '削除処理は再試行待ちです。運営による確認が必要な場合があります。'
      : '削除を受け付けています。完了までお待ちください。';
    timer = setTimeout(check, 15000);
  } catch {
    output.textContent = '状況を確認できませんでした。時間をおいて再試行してください。';
  } finally {
    retry.disabled = completed;
  }
}
retry.onclick = () => void check();
void check();
