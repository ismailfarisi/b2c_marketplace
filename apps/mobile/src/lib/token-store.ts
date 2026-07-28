import * as SecureStore from "expo-secure-store";
import type { TokenStore } from "@marketplace/api-client";

const ACCESS_KEY = "auth.access_token";
const REFRESH_KEY = "auth.refresh_token";

/**
 * Tokens live in the OS keychain, not AsyncStorage.
 *
 * AsyncStorage is plaintext on disk and readable from a rooted or jailbroken
 * device — and more practically, from any device backup. A long-lived refresh
 * token sitting there is a persistent account takeover waiting for one lost
 * phone.
 *
 * The in-memory access-token cache exists because SecureStore hits the keychain
 * on every read, and the feed fires a request per scroll page.
 */
let accessCache: string | null = null;

export const tokenStore: TokenStore = {
  async getAccessToken() {
    if (accessCache !== null) return accessCache;
    accessCache = await SecureStore.getItemAsync(ACCESS_KEY);
    return accessCache;
  },

  getRefreshToken() {
    return SecureStore.getItemAsync(REFRESH_KEY);
  },

  async setTokens(access: string, refresh: string) {
    accessCache = access;
    await Promise.all([
      SecureStore.setItemAsync(ACCESS_KEY, access),
      SecureStore.setItemAsync(REFRESH_KEY, refresh, {
        // Refresh tokens require the device to have been unlocked at least once
        // since boot. Background refresh on a cold-booted locked phone will fail
        // rather than expose the token to a pre-first-unlock attack.
        keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY,
      }),
    ]);
  },

  async clear() {
    accessCache = null;
    await Promise.all([
      SecureStore.deleteItemAsync(ACCESS_KEY),
      SecureStore.deleteItemAsync(REFRESH_KEY),
    ]);
  },
};
