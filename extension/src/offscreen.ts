import { MANAGEMENT_ORIGIN } from './protocol';
let frame: HTMLIFrameElement | undefined;
chrome.runtime.onMessage.addListener((request, sender, respond) => {
  if (
    sender.id !== chrome.runtime.id ||
    request?.target !== 'offscreen' ||
    request.type !== 'AUTH_OPEN'
  )
    return;
  frame?.remove();
  frame = document.createElement('iframe');
  frame.src = `${MANAGEMENT_ORIGIN}/extension-auth.html`;
  document.body.append(frame);
  frame.onload = () => frame?.contentWindow?.postMessage({ type: 'AUTH_START' }, MANAGEMENT_ORIGIN);
  respond({ started: true });
});
window.addEventListener('message', (event) => {
  if (
    event.origin !== MANAGEMENT_ORIGIN ||
    event.source !== frame?.contentWindow ||
    event.data?.type !== 'AUTH_RESULT'
  )
    return;
  void chrome.runtime.sendMessage({ type: 'AUTH_RESULT', credential: event.data.credential });
});
