/**
 * The single fetch mutator every generated endpoint routes through.
 *
 * Web and mobile share this file, which is why token storage is injected rather
 * than imported: the browser uses an httpOnly-cookie-backed store, Expo uses
 * SecureStore, and neither should be reachable from the other's bundle.
 */

export interface ApiProblem {
  type: string;
  title: string;
  status: number;
  detail?: string;
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
  /** Called when refresh fails and the user must sign in again. */
  onUnauthenticated?: () => void;
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
    if (!refresh) return false;

    const res = await fetch(`${config.baseUrl}/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refresh }),
    });

    if (!res.ok) {
      await config.tokens?.clear();
      config.onUnauthenticated?.();
      return false;
    }

    const body = (await res.json()) as { access_token: string; refresh_token: string };
    await config.tokens?.setTokens(body.access_token, body.refresh_token);
    return true;
  })().finally(() => {
    refreshInFlight = null;
  });

  return refreshInFlight;
}

export const apiFetch = async <T>(
  { url, method, params, data, headers, signal }: {
    url: string;
    method: string;
    params?: Record<string, unknown>;
    data?: unknown;
    headers?: HeadersInit;
    signal?: AbortSignal;
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

    const res = await fetch(`${config.baseUrl}${url}${query}`, {
      method,
      headers: requestHeaders,
      body: data !== undefined ? JSON.stringify(data) : undefined,
      signal,
    });

    if (res.status === 401 && retryOn401 && (await refreshTokens())) {
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
