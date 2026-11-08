// 拡張IDを固定するための公開鍵。
//
// manifest に `key` が無いと、「パッケージ化されていない拡張機能を読み込む」で読み込んだ拡張のIDは
// 読み込んだフォルダーのパスから決まり、端末ごとに変わる。APIの live モードは EXTENSION_IDS に
// 書いた `chrome-extension://<id>` からの状態を変える要求しか受け付けないので、IDをどの端末でも
// 同じにする必要がある。Chrome は `key`（公開鍵）の SHA-256 からIDを決めるため、公開鍵を manifest に
// 入れればIDは一定になる。
//
// ここにあるのは公開鍵だけで、秘密ではない。対になる秘密鍵は CRX の署名にしか使わず、ZIPを展開して
// 読み込む配布方法（`wxt zip` も CRX は作らない）では不要なので、リポジトリには置かない。
//
// 公開鍵から導かれる拡張ID: egongmmfglcolkgiappacmmbiiiogpmb
// （`extensionIdFromPublicKey` で再計算でき、extension-key.test.ts で一致を確かめる）
import { createHash } from 'node:crypto';

/** manifest の `key`。RSA 2048 の公開鍵（DER の SubjectPublicKeyInfo を base64 にしたもの）。 */
export const EXTENSION_PUBLIC_KEY =
  'MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxZ6a8E7rxxQuNlT8aRUP7Ln0JPCi+cSo250395B09po/J6yt9OB8tzyedlQrKSOEoDwEJANQYSW6v3EQF2j9l0Ayrn4Il6bk1JYnPlsb+eSqNeP2TOSl+wiCU9pvJiVycPFHVbpY3QKDnpkRlbcqrS1vLnSIcDHsl28orWFSAS8WLQxsaeWlvy3o/sfcNs8KpxGCG3khQIQeC0tBaznE8rtJHreaKaL6YaeTk8p1xeelMFGsbtwTyRwP9wpGgCMUnQTOdEKQl2BP8w3Scd/bNmOqcmRZn5SrxcjIcXvLjzL8VDnp3pT07d+ub+nR33W8kXefeanTSrqapQxTzEGZYwIDAQAB';

/** `EXTENSION_PUBLIC_KEY` から決まる拡張ID。APIの `EXTENSION_IDS` に設定する値。 */
export const EXTENSION_ID = 'egongmmfglcolkgiappacmmbiiiogpmb';

/**
 * Chrome と同じ手順で、公開鍵から拡張IDを計算する。
 * DER の公開鍵の SHA-256 の先頭16バイト（16進で32文字）を、0-9a-f → a-p に置き換えたもの。
 */
export function extensionIdFromPublicKey(publicKeyBase64: string): string {
  const digest = createHash('sha256').update(Buffer.from(publicKeyBase64, 'base64')).digest('hex');
  return digest
    .slice(0, 32)
    .replace(/[0-9a-f]/g, (hex) => String.fromCharCode('a'.charCodeAt(0) + parseInt(hex, 16)));
}
