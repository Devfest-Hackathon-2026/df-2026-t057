// 管理画面（Webページ）と Chrome 拡張の content script の間で、window.postMessage で送るメッセージ。
// 受け取った側は必ず parsePageMessage で検証してから使う（AGENTS.md「Chrome拡張」）。
import { z } from 'zod';

const connect = z.object({
  type: z.literal('DOPAGAKI_CONNECT'),
  /** API のオリジン（管理画面と同じ）。 */
  origin: z.url(),
  /** `Authorization` ヘッダーにそのまま入れる値（`Bearer <Firebase ID token>` か `Demo <token>`）。 */
  authorization: z.string().min(1).max(4096),
  /** アカウントが切り替わったことを拡張が検知するための ID。 */
  accountId: z.string().min(1).max(128),
});

const disconnect = z.object({ type: z.literal('DOPAGAKI_DISCONNECT') });

/** 拡張が読み込まれていることを管理画面へ知らせる（管理画面は拡張の有無で案内を変える）。 */
const extensionReady = z.object({
  type: z.literal('DOPAGAKI_EXTENSION_READY'),
  version: z.string().max(32),
});

export const pageMessage = z.discriminatedUnion('type', [connect, disconnect, extensionReady]);
export type PageMessage = z.infer<typeof pageMessage>;
export type ConnectMessage = z.infer<typeof connect>;

/** 検証に通らなければ undefined。 */
export function parsePageMessage(data: unknown): PageMessage | undefined {
  const result = pageMessage.safeParse(data);
  return result.success ? result.data : undefined;
}
