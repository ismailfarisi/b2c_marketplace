/**
 * The single fetch mutator every generated endpoint routes through.
 *
 * **Web only.** This package was written when mobile was Expo and shared it;
 * mobile is Kotlin Multiplatform now and has its own client, so `api/openapi.yaml`
 * rather than this file is what the two platforms share.
 *
 * Token storage is still injected rather than imported, and it is worth keeping
 * that way for a reason that outlived Expo: this package then has no opinion
 * about where a token lives, which is what let the browser take the
 * weaker-but-correct answer (`sessionStorage`, tab-scoped) without that choice
 * leaking into a package other things import.
 */

export interface ApiProblem {
  type: string;
  title: string;
  status: number;
  detail?: string;
  /** Stable reason where the status alone is not enough (e.g. otp_wrong_code). */
  code?: string;
  request_id?: string;
  errors?: Array<{ field: string; code: string; message?: string }>;
}

/** Thrown for every non-2xx response. Carries the problem body when present. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: ApiProblem | null;
  readonly requestId: string | null;

  constructor(status: number, problem: ApiProblem | null, requestId: string | null) {
    super(problem?.title ?? `Request failed with status ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.problem = problem;
    this.requestId = requestId;
  }

  /** Field-level validation detail, keyed by field name. */
  get fieldErrors(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const e of this.problem?.errors ?? []) {
      out[e.field] = e.message ?? e.code;
    }
    return out;
  }
}

export interface TokenStore {
  getAccessToken(): string | null | Promise<string | null>;
  getRefreshToken(): string | null | Promise<string | null>;
  setTokens(access: string, refresh: string): void | Promise<void>;
  clear(): void | Promise<void>;
}

interface ClientConfig {
  baseUrl: string;
  tokens?: TokenStore;
  /**
   * Called when a request is unauthorised and no session can be restored:
   * `expired` when a refresh token was rejected, `signedOut` when there was
   * never one.
   */
  onUnauthenticated?: (reason: "expired" | "signedOut") => void;
  locale?: () => string;
}

let config: ClientConfig = { baseUrl: "http://localhost:8080/v1" };

export function configureApiClient(next: ClientConfig): void {
  config = { ...config, ...next };
}

/**
 * In-flight refresh, shared across callers.
 *
 * Without this, a screen firing five queries on mount against an expired token
 * triggers five concurrent refreshes — four of which use an already-rotated
 * token and are correctly rejected as replay, logging the user out mid-session.
 */
let refreshInFlight: Promise<boolean> | null = null;

async function refreshTokens(): Promise<boolean> {
  if (refreshInFlight) return refreshInFlight;

  refreshInFlight = (async () => {
    const refresh = await config.tokens?.getRefreshToken();
    if (!refresh) {
      config.onUnauthenticated?.("signedOut");
      return false;
    }

    // A network failure throws (TypeError) and keeps the tokens: being offline
    // says nothing about whether the session is still good.
    const res = await fetch(`${config.baseUrl}/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refresh }),
    });

    // Only 400/401 mean the refresh token itself is spent or revoked.
    if (res.status === 400 || res.status === 401) {
      await config.tokens?.clear();
      config.onUnauthenticated?.("expired");
      return false;
    }
    // A 5xx is the server's bad moment, not the session's: keep the tokens and
    // surface it as an ApiError so the screen can offer a retry.
    if (!res.ok) {
      let problem: ApiProblem | null = null;
      try {
        problem = (await res.json()) as ApiProblem;
      } catch {
        // Non-JSON gateway error.
      }
      throw new ApiError(res.status, problem, res.headers.get("X-Request-Id"));
    }

    const body = (await res.json()) as { access_token: string; refresh_token: string };
    await config.tokens?.setTokens(body.access_token, body.refresh_token);
    return true;
  })().finally(() => {
    refreshInFlight = null;
  });

  return refreshInFlight;
}

/** How long a request may take before it fails as a network error. */
export const REQUEST_TIMEOUT_MS = 15_000;

export const apiFetch = async <T>(
  { url, method, params, data, headers, signal }: {
    url: string;
    method: string;
    // `| undefined` because generated callers pass optional values through
    // as-is, which exactOptionalPropertyTypes otherwise rejects.
    params?: object | undefined;
    data?: unknown;
    headers?: HeadersInit | undefined;
    signal?: AbortSignal | undefined;
  },
): Promise<T> => {
  const send = async (retryOn401: boolean): Promise<T> => {
    const query = params
      ? "?" +
        new URLSearchParams(
          Object.entries(params)
            .filter(([, v]) => v !== undefined && v !== null)
            .map(([k, v]) => [k, String(v)]),
        )
      : "";

    const token = await config.tokens?.getAccessToken();

    const requestHeaders: Record<string, string> = {
      Accept: "application/json",
      "Accept-Language": config.locale?.() ?? "en",
      ...(data !== undefined ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(headers as Record<string, string> | undefined),
    };

    // Every mutating request gets an idempotency key unless the caller supplied
    // one. A retried POST on a dropped mobile connection must not place two
    // orders, and making the client remember to opt in guarantees someone won't.
    const mutating = !["GET", "HEAD", "OPTIONS"].includes(method.toUpperCase());
    if (mutating && !requestHeaders["Idempotency-Key"]) {
      requestHeaders["Idempotency-Key"] = crypto.randomUUID();
    }

    // Built up rather than declared with `undefined` values, because
    // `exactOptionalPropertyTypes` is on and `{ body: undefined }` is not the
    // same type as `{}` under it. This has never typechecked since Phase 0 —
    // the package had no consumer running tsc over it until the Phase 7
    // console arrived.
    const init: RequestInit = { method, headers: requestHeaders };
    if (data !== undefined) init.body = JSON.stringify(data);
    // A hung API must end in an error the caller can retry, not an endless
    // spinner. The timeout rejects as a TypeError, like any request that never
    // got a response, so error UIs treat it as a connection problem.
    const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
    init.signal =
      signal && typeof AbortSignal.any === "function"
        ? AbortSignal.any([signal, timeout])
        : (signal ?? timeout);

    let res: Response;
    try {
      res = await fetch(`${config.baseUrl}${url}${query}`, init);
    } catch (err) {
      if (timeout.aborted && !signal?.aborted) {
        throw new TypeError("Request timed out");
      }
      throw err;
    }

    // Auth endpoints answer 401 for a wrong code or a dead refresh token, not
    // for an expired access token. Refreshing and replaying a wrong OTP would
    // rotate the session for nothing and spend a second attempt on the code.
    if (
      res.status === 401 &&
      retryOn401 &&
      !url.startsWith("/auth/") &&
      (await refreshTokens())
    ) {
      return send(false);
    }

    if (!res.ok) {
      let problem: ApiProblem | null = null;
      try {
        problem = (await res.json()) as ApiProblem;
      } catch {
        // A non-JSON error body (proxy 502, gateway timeout) is still an error;
        // it just has no structured detail to report.
      }
      throw new ApiError(res.status, problem, res.headers.get("X-Request-Id"));
    }

    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  };

  return send(true);
};
