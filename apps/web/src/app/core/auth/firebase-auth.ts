import { InjectionToken } from '@angular/core';

/** Firebase Auth のうち、このアプリが使う部分。テストでは偽物に差し替える。 */
export interface FirebaseUser {
  readonly uid: string;
  readonly email: string | null;
  readonly displayName: string | null;
  readonly photoURL: string | null;
  getIdToken(): Promise<string>;
}

export interface FirebaseAuthPort {
  onIdTokenChanged(listener: (user: FirebaseUser | null) => void): void;
  currentUser(): FirebaseUser | null;
  signInWithGoogle(): Promise<void>;
  signOut(): Promise<void>;
}

export type FirebaseAuthLoader = (config: Record<string, string>) => Promise<FirebaseAuthPort>;

/** Firebase の SDK は大きいので、live のときだけ読み込む。 */
export const loadFirebaseAuth: FirebaseAuthLoader = async (config) => {
  const [{ initializeApp }, auth] = await Promise.all([
    import('firebase/app'),
    import('firebase/auth'),
  ]);
  const app = initializeApp(config);
  const instance = auth.getAuth(app);
  return {
    onIdTokenChanged: (listener) => {
      auth.onIdTokenChanged(instance, listener);
    },
    currentUser: () => instance.currentUser,
    signInWithGoogle: async () => {
      const provider = new auth.GoogleAuthProvider();
      provider.setCustomParameters({ prompt: 'select_account' });
      await auth.signInWithPopup(instance, provider);
    },
    signOut: () => auth.signOut(instance),
  };
};

export const FIREBASE_AUTH_LOADER = new InjectionToken<FirebaseAuthLoader>('FirebaseAuthLoader', {
  providedIn: 'root',
  factory: () => loadFirebaseAuth,
});
