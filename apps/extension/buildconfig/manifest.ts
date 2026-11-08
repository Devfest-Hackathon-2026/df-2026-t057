// manifest.json のうち、WXT が entrypoints から作らない部分。
// 権限は最小にする（AGENTS.md「Chrome拡張」）。作業サイトのタイマーは optional_host_permissions で必要なときに求める。

export const YOUTUBE_MATCH = 'https://www.youtube.com/*';

export function managementMatches(origin: string): string[] {
  return [`${origin}/*`];
}

export function manifestFor(origin: string, version: string) {
  return {
    name: 'DOPAGAKI',
    version,
    description: '課題を終えて、次の楽しみに。カレンダー連携と作業タイマー。',
    minimum_chrome_version: '120',
    permissions: ['storage', 'alarms', 'notifications', 'scripting', 'activeTab'],
    host_permissions: [...managementMatches(origin), YOUTUBE_MATCH],
    optional_host_permissions: ['https://*/*', 'http://*/*'],
    action: { default_title: 'DOPAGAKI' },
    icons: { 16: 'icon-16.png', 48: 'icon-48.png', 128: 'icon-128.png' },
    web_accessible_resources: [{ resources: ['gold-light.png'], matches: [YOUTUBE_MATCH] }],
    content_security_policy: { extension_pages: "script-src 'self'; object-src 'self'" },
  };
}
