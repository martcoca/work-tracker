import { initializeApp } from "firebase/app";
import {
  GoogleAuthProvider,
  browserSessionPersistence,
  getAuth,
  getRedirectResult,
  onAuthStateChanged,
  setPersistence,
  signInWithRedirect,
  signOut,
  type User,
} from "firebase/auth";

export const AUTH_DOMAIN = "tracker.martcoca.com";
export const CALLBACK_URL = `https://${AUTH_DOMAIN}/__/auth/handler`;
export const LOGOUT_URL = `https://${AUTH_DOMAIN}/signed-out`;

export interface AuthUser {
  getToken(): Promise<string>;
}

export interface AuthPort {
  observe(callback: (user: AuthUser | null) => void): () => void;
  signIn(): Promise<void>;
  signOut(): Promise<void>;
}

interface FirebaseRuntimeConfig {
  apiKey: string;
  projectId: string;
  appId: string;
  [key: string]: unknown;
}

export async function createFirebaseAuth(): Promise<AuthPort> {
  const response = await fetch("/__/firebase/init.json", { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error("Firebase runtime configuration is unavailable");
  }
  const runtime = (await response.json()) as FirebaseRuntimeConfig;
  const app = initializeApp({ ...runtime, authDomain: AUTH_DOMAIN });
  const auth = getAuth(app);
  await setPersistence(auth, browserSessionPersistence);
  await getRedirectResult(auth);

  const adapt = (user: User | null): AuthUser | null =>
    user === null ? null : { getToken: () => user.getIdToken() };

  return {
    observe(callback) {
      return onAuthStateChanged(auth, (user) => callback(adapt(user)));
    },
    async signIn() {
      await signInWithRedirect(auth, new GoogleAuthProvider());
    },
    async signOut() {
      await signOut(auth);
      window.location.assign(LOGOUT_URL);
    },
  };
}
