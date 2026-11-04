"use client";

import { useCallback, useRef } from "react";

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
 *   const idem = useIdempotencyKey();
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
  return { key, reset, headers };
}
