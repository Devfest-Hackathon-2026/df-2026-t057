import { NetworkError } from '@dopagaki/contract';
import { messages, type Locale } from '@dopagaki/messages';

/** Firebase Auth の例外を、利用者に見せる一文にする。 */
export function authErrorMessage(error: unknown, locale: Locale): string {
  const text = messages[locale];
  const firebaseMessages: Partial<Record<string, string>> = {
    'auth/popup-closed-by-user': text.web.app.auth.cancelled,
    'auth/cancelled-popup-request': text.web.app.auth.cancelled,
    'auth/popup-blocked': text.web.app.auth.popupBlocked,
    'auth/network-request-failed': text.shared.network,
  };
  const code =
    typeof error === 'object' && error !== null && 'code' in error && typeof error.code === 'string'
      ? error.code
      : undefined;
  const known = code === undefined ? undefined : firebaseMessages[code];
  if (known) return known;
  if (error instanceof NetworkError || error instanceof TypeError) return text.shared.network;
  return text.web.app.auth.failed;
}
