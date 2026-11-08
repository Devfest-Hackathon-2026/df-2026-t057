// background で起きた失敗を、content script・ポップアップへ返す形にする。
import { ApiError, NetworkError } from '@dopagaki/contract';
import { errorMessage, problemMessages } from '@dopagaki/messages';
import type { Reply } from '../lib/protocol';
import { failureAction } from '../lib/report-queue';

/** 利用者に見せる文言をそのまま持つ失敗（API 以外の理由で断るとき）。 */
export class ExtensionError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ExtensionError';
  }
}

export function errorReply(error: unknown): Extract<Reply, { ok: false }> {
  const retryable = failureAction(failureOf(error)) === 'retry';
  if (error instanceof ExtensionError) return { ok: false, error: error.message, retryable: false };
  if (error instanceof ApiError) {
    return { ok: false, error: errorMessage(error), code: error.code, retryable };
  }
  if (error instanceof NetworkError) return { ok: false, error: errorMessage(error), retryable };
  return { ok: false, error: problemMessages.internal, retryable: false };
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
