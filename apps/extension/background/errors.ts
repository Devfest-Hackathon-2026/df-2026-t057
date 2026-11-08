// background で起きた失敗を、content script・ポップアップへ返す形にする。
import { ApiError, NetworkError } from '@dopagaki/contract';
import { errorMessage } from '@dopagaki/messages';
import { messagesFor } from '../lib/locale';
import type { Reply } from '../lib/protocol';
import { failureAction } from '../lib/report-queue';
import { getState } from './store';

/** API 以外の理由で断るときの、利用者に見せる文言（packages/messages の拡張の文言）のキー。 */
type ExtensionErrorReason =
  | 'connectFromApp'
  | 'accountChanged'
  | 'untrustedSender'
  | 'operationUnknown'
  | 'fromShorts'
  | 'claimFirst'
  | 'sessionEnded';

/** API 以外の理由で断るときの失敗。文言は返すとき（errorReply）に利用者の言語で引く。 */
export class ExtensionError extends Error {
  constructor(readonly reason: ExtensionErrorReason) {
    super(reason);
    this.name = 'ExtensionError';
  }
}

export async function errorReply(error: unknown): Promise<Extract<Reply, { ok: false }>> {
  const { shared, extension } = messagesFor(await getState());
  const retryable = failureAction(failureOf(error)) === 'retry';
  if (error instanceof ExtensionError) {
    return { ok: false, error: extension[error.reason], retryable: false };
  }
  if (error instanceof ApiError) {
    return { ok: false, error: errorMessage(error, shared), code: error.code, retryable };
  }
  if (error instanceof NetworkError) {
    return { ok: false, error: errorMessage(error, shared), retryable };
  }
  return { ok: false, error: shared.problems.internal, retryable: false };
}

/** 送り直すかどうかの判定（lib/report-queue）に渡す形。 */
export function failureOf(error: unknown): {
  status: number | undefined;
  code: string | undefined;
} {
  if (error instanceof ExtensionError) return { status: 400, code: 'extension' };
  if (error instanceof ApiError) return { status: error.status, code: error.code };
  if (error instanceof NetworkError) return { status: undefined, code: undefined };
  // 想定外の失敗（レスポンスの形が違うなど）は、送り直しても直らないので 4xx と同じに扱う。
  return { status: 400, code: 'internal' };
}
