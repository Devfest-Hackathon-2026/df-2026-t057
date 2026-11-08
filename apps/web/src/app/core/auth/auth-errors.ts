import { NetworkError } from '@dopagaki/contract';
import { networkMessage } from '@dopagaki/messages';

export const firebaseConfigMissingMessage = 'FirebaseのGoogleログイン設定が必要です。';

const firebaseMessages: Partial<Record<string, string>> = {
  'auth/popup-closed-by-user': 'ログインをキャンセルしました。',
  'auth/cancelled-popup-request': 'ログインをキャンセルしました。',
  'auth/popup-blocked': 'ポップアップを許可して、もう一度ログインしてください。',
  'auth/network-request-failed': networkMessage,
};

/** Firebase Auth の例外を、利用者に見せる一文にする。 */
export function authErrorMessage(error: unknown): string {
  const code =
    typeof error === 'object' && error !== null && 'code' in error && typeof error.code === 'string'
      ? error.code
      : undefined;
  const known = code === undefined ? undefined : firebaseMessages[code];
  if (known) return known;
  if (error instanceof NetworkError || error instanceof TypeError) return networkMessage;
  return 'ログインできませんでした。もう一度お試しください。';
}
