import type { TokenStore } from "@marketplace/api-client";

/**
 * Tokens in memory, with the refresh token in sessionStorage.
 *
 * Not localStorage. A refresh token in localStorage survives the tab closing,
 * is readable by any script that ever gets injected, and persists on a shared
 * machine until someone clears site data — which for a warehouse terminal or a
 * branch office PC is never. sessionStorage is scoped to the tab and dies with
 * it, which matches how these consoles are actually used: someone signs in at
 * the start of a shift on a machine several people touch.
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

let accessToken: string | null = null;

export const tokenStore: TokenStore = {
  getAccessToken() {
    return accessToken;
  },

  getRefreshToken() {
    if (typeof window === "undefined") return null;
    return window.sessionStorage.getItem(REFRESH_KEY);
  },

  setTokens(access: string, refresh: string) {
    accessToken = access;
    if (typeof window !== "undefined") {
      window.sessionStorage.setItem(REFRESH_KEY, refresh);
    }
  },

  clear() {
    accessToken = null;
    if (typeof window !== "undefined") {
      window.sessionStorage.removeItem(REFRESH_KEY);
    }
  },
};
