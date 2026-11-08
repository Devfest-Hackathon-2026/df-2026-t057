import { ChangeDetectionStrategy, Component } from '@angular/core';
import { detectLocale, messages } from '@dopagaki/messages';
import { initializeApp } from 'firebase/app';
import {
  getAuth,
  GoogleAuthProvider,
  inMemoryPersistence,
  setPersistence,
  signInWithPopup,
  signOut,
} from 'firebase/auth';
import { z } from 'zod';
@Component({
  selector: 'app-root',
  template: '<p>{{status}}</p>',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ExtensionAuth {
  // 単独で起動するページで LocaleStore（プロフィールの言語）を持たないので、ブラウザーの言語で表示する。
  private readonly locale = detectLocale(navigator.languages);
  protected readonly status = messages[this.locale].web.onboarding.extensionAuth.checking;
  constructor() {
    document.documentElement.lang = this.locale;
    const origin = location.ancestorOrigins[0];
    if (!origin || !/^chrome-extension:\/\/[a-p]{32}$/.test(origin)) return;
    let running = false;
    window.addEventListener('message', (event: MessageEvent<unknown>) => {
      if (
        event.source !== parent ||
        event.origin !== origin ||
        !z.object({ type: z.literal('AUTH_START') }).safeParse(event.data).success ||
        running
      )
        return;
      running = true;
      void (async () => {
        try {
          const response = await fetch('/api/config');
          if (!response.ok) throw new Error('config');
          const config = z
            .object({ firebase: z.record(z.string(), z.string()) })
            .parse(await response.json());
          const auth = getAuth(initializeApp(config.firebase, 'extension-popup'));
          await setPersistence(auth, inMemoryPersistence);
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
          await signOut(auth);
        } catch {
          parent.postMessage({ type: 'AUTH_RESULT', credential: { error: 'auth_failed' } }, origin);
        }
      })();
    });
  }
}
