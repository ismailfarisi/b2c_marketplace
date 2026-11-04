import type { TokenStore } from "@marketplace/api-client";

/**
 * Tokens in memory, with the refresh token in Web Storage.
 *
 * The consoles (/admin, /console) keep it in sessionStorage: it is scoped to
 * the tab and dies with it, which matches how they are used — someone signs in
 * at the start of a shift on a machine several people touch.
 *
 * The customer storefront keeps it in localStorage. A meal subscriber opens
 * links in new tabs and comes back tomorrow; a tab-scoped session made them
 * redo OTP every visit and burnt the 5-codes-an-hour budget. The cost is that
 * the token outlives the tab and is readable by injected script, so sign-out
 * clears both stores.
 *
 * The access token is memory-only and never persisted at all. It is short-lived
 * and re-derivable from the refresh token, so writing it anywhere is pure
 * downside.
 *
 * The genuinely correct answer is an httpOnly cookie set by a same-origin BFF
 * route, which no script can read. That needs a server route per call and is
 * Phase 8 work (docs/PHASE-7-CLIENTS-AND-LOGISTICS.md §8) — this is the honest
 * interim, not the destination.
 */
const REFRESH_KEY = "auth.refresh_token";

const CONSOLE_PATH = /^\/[a-z]{2}(?:-[A-Za-z]+)?\/(?:admin|console)(?:\/|$)/;

function storage(): Storage | null {
  if (typeof window === "undefined") return null;
  return CONSOLE_PATH.test(window.location.pathname)
    ? window.sessionStorage
    : window.localStorage;
}

let accessToken: string | null = null;

export const tokenStore: TokenStore = {
  getAccessToken() {
    return accessToken;
  },

  getRefreshToken() {
    return storage()?.getItem(REFRESH_KEY) ?? null;
  },

  setTokens(access: string, refresh: string) {
    accessToken = access;
    storage()?.setItem(REFRESH_KEY, refresh);
  },

  clear() {
    accessToken = null;
    if (typeof window !== "undefined") {
      window.sessionStorage.removeItem(REFRESH_KEY);
      window.localStorage.removeItem(REFRESH_KEY);
    }
  },
};
