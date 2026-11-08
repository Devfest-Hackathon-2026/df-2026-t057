import { initializeApp } from 'firebase/app';
import { getAuth, GoogleAuthProvider, signInWithPopup } from 'firebase/auth';
const origin = location.ancestorOrigins[0];
if (!origin || !/^chrome-extension:\/\/[a-p]{32}$/.test(origin))
  throw new Error('認証元を確認してください。');
let running = false;
window.addEventListener('message', async (event) => {
  if (
    event.source !== parent ||
    event.origin !== origin ||
    event.data?.type !== 'AUTH_START' ||
    running
  )
    return;
  running = true;
  try {
    const config = await (await fetch('/api/config')).json();
    const auth = getAuth(initializeApp(config.firebase, 'extension-popup'));
    const provider = new GoogleAuthProvider();
    provider.setCustomParameters({ prompt: 'select_account' });
    const result = await signInWithPopup(auth, provider);
    const credential = GoogleAuthProvider.credentialFromResult(result);
    parent.postMessage(
      {
        type: 'AUTH_RESULT',
        credential: { idToken: credential?.idToken, accessToken: credential?.accessToken },
      },
      origin,
    );
    await auth.signOut();
  } catch {
    parent.postMessage({ type: 'AUTH_RESULT', credential: { error: 'auth_failed' } }, origin);
  }
});
