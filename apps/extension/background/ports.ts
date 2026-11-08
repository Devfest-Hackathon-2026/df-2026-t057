import { connectionVersion, getConnection } from './connection';
// content script とつないだ runtime.Port。
// つながっている間は service worker を止めないよう、content script が定期的に PING を送る。
// 1つ以上つながっている間だけ変更通知（SSE）を購読し、状態が変わったら各タブへ送る。
import { PORT_NAME, portPingSchema, type PortPush } from '../lib/protocol';
import { getState } from './store';
import { getOwner } from './viewing';

const ports = new Set<chrome.runtime.Port>();
const deliveries = new WeakMap<chrome.runtime.Port, number>();
let onCountChange: (count: number) => void = () => undefined;

function trusted(port: chrome.runtime.Port): boolean {
  const sender = port.sender;
  if (port.name !== PORT_NAME || sender?.id !== chrome.runtime.id || !sender.url || !sender.tab) {
    return false;
  }
  // YouTube・管理画面・作業サイトなど、http(s) のページの content script だけを受け付ける。
  const protocol = new URL(sender.url).protocol;
  return protocol === 'https:' || protocol === 'http:';
}

async function pushTo(port: chrome.runtime.Port): Promise<void> {
  const ticket = (deliveries.get(port) ?? 0) + 1;
  deliveries.set(port, ticket);
  const version = connectionVersion();
  const connection = await getConnection().catch(() => undefined);
  const owner = await getOwner();
  const state = connection ? await getState() : undefined;
  if (ticket !== deliveries.get(port) || version !== connectionVersion()) return;
  const message: PortPush = {
    type: 'STATE',
    state: state ?? null,
    ownsViewing: owner !== undefined && owner.tabId === port.sender?.tab?.id,
  };
  try {
    port.postMessage(message);
  } catch {
    // 切れたポートは onDisconnect で取り除く。
  }
}

export function listenPorts(countChanged: (count: number) => void): void {
  onCountChange = countChanged;
  chrome.runtime.onConnect.addListener((port) => {
    if (!trusted(port)) {
      port.disconnect();
      return;
    }
    ports.add(port);
    port.onMessage.addListener((message: unknown) => {
      // PING は受け取るだけでよい（service worker の待機時間がリセットされる）。それ以外は無視する。
      portPingSchema.safeParse(message);
    });
    port.onDisconnect.addListener(() => {
      ports.delete(port);
      onCountChange(ports.size);
    });
    onCountChange(ports.size);
    void pushTo(port);
  });
}

export function portCount(): number {
  return ports.size;
}

export async function broadcast(): Promise<void> {
  await Promise.all([...ports].map((port) => pushTo(port)));
}
