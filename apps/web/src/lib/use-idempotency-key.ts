"use client";

import { useCallback, useMemo, useRef } from "react";

/**
 * One `Idempotency-Key` per draft.
 *
 * apiFetch mints a fresh key for every mutating call, which protects a single
 * network retry but not a person pressing "Subscribe" again after a timeout:
 * that second press is a new call with a new key, and a second order. Holding
 * the key in a ref for the life of the draft makes every attempt at the same
 * draft the same request to the server. Call `reset()` once the draft has
 * succeeded or its inputs change.
 *
 * The returned object and its functions are stable across renders, so they
 * are safe in effect dependency lists. A request the server refused (4xx/5xx)
 * releases its key there, so retrying with the same key re-executes it.
 *
 *   const idem = useIdempotencyKey();
 *   useEffect(() => idem.reset(), [draftKey, idem]);
 *   subscribe(input, { headers: idem.headers() });
 */
export function useIdempotencyKey() {
  const ref = useRef<string | null>(null);
  const key = useCallback(() => {
    if (!ref.current) ref.current = crypto.randomUUID();
    return ref.current;
  }, []);
  const reset = useCallback(() => {
    ref.current = null;
  }, []);
  const headers = useCallback(() => ({ "Idempotency-Key": key() }), [key]);
  return useMemo(() => ({ key, reset, headers }), [key, reset, headers]);
}
