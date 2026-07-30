import { apiFetch } from "@marketplace/api-client";

import { tokenStore } from "@/lib/token-store";

/**
 * Sign-in bindings.
 *
 * Hand-written like the rest of `lib/` — see the note at the top of `api.ts`.
 *
 * **There is no password anywhere in this system.** The phone number is the
 * credential and a one-time code is the proof, for staff exactly as for
 * customers: `platform_admins` grants *authority*, never a second way to
 * authenticate. That is worth stating because "add a login page" usually means
 * an email and a password field, and adding one here would mean inventing a
 * credential the server has no column for and no way to reset.
 *
 * Staff-ness is therefore not decided here at all. This module gets a token for
 * whoever holds the phone; `/admin/whoami` decides what that token can do, on
 * the server, on every call.
 */

export interface OtpChallenge {
  challenge_id: string;
  expires_at: string;
  /** Seconds before a second code may be requested. The server counts codes per
   *  number per hour and refuses the sixth, so a UI that lets someone tap
   *  "resend" freely is a UI that locks them out of their own account. */
  resend_after_seconds: number;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: {
    id: string;
    phone: string;
    email: string | null;
    display_name: string | null;
    locale: string;
    status: string;
    created_at: string;
  };
}

/**
 * Ask for a code.
 *
 * The response is identical whether or not the number belongs to anyone — the
 * endpoint is public, and one that answered differently would be a free
 * membership oracle for any list of phone numbers.
 */
export const requestOtp = (phone: string, locale: string) =>
  apiFetch<OtpChallenge>({
    url: "/auth/otp/request",
    method: "POST",
    data: { phone, purpose: "login", locale },
  });

/**
 * Exchange the code for a session, and store it.
 *
 * Storing here rather than at the call site is deliberate: a caller that
 * verifies and forgets to persist gets a screen that works until the first
 * refresh and then silently signs the user out, which is a bug nobody
 * reproduces on their own machine.
 */
export const verifyOtp = async (challengeId: string, code: string): Promise<TokenPair> => {
  const pair = await apiFetch<TokenPair>({
    url: "/auth/otp/verify",
    method: "POST",
    data: {
      challenge_id: challengeId,
      code,
      device: { platform: "web", name: "Ops console" },
    },
  });
  await tokenStore.setTokens(pair.access_token, pair.refresh_token);
  return pair;
};

/**
 * Revoke the session server-side, then drop the local copy.
 *
 * In that order, and the local clear runs even when the call fails. A sign-out
 * that leaves a usable refresh token in `sessionStorage` because the network
 * blipped is the failure mode this whole function exists to prevent — the
 * person clicking it is often walking away from a shared machine.
 *
 * The access token remains valid until it expires (ACCESS_TOKEN_TTL, 15 minutes
 * by default); the JWT is verified locally and nothing consults a revocation
 * list per request. What this stops is the session outliving the sign-out.
 */
export const logout = async (allDevices = false): Promise<void> => {
  try {
    await apiFetch<void>({
      url: "/auth/logout",
      method: "POST",
      data: { all_devices: allDevices },
    });
  } finally {
    await tokenStore.clear();
  }
};
