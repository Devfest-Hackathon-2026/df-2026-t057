// ビルド時に決まる設定（wxt.config.ts の `define` で埋め込む）。
declare const __MANAGEMENT_ORIGIN__: string;

/** 管理画面と API のオリジン。拡張はこのオリジンからの接続だけを受け付ける。 */
export const MANAGEMENT_ORIGIN: string = __MANAGEMENT_ORIGIN__;
