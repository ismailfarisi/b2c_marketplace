"use client";

import { useCallback, useMemo, useRef } from "react";

/**
 * A synchronous double-tap guard for mutations.
 *
 * `isPending` only disables a button on the next render; a second tap in the
 * same frame still fires. `guard(fn)` wraps the click handler so it runs only
 * while nothing else it guards is in flight. Pass `release` to the mutation's
 * `onSettled`.
 *
 *   const busy = useBusyGuard();
 *   const save = useMutation({ mutationFn, onSettled: busy.release });
 *   <button onClick={busy.guard(() => save.mutate(v))} />
 */
export function useBusyGuard() {
  const ref = useRef(false);
  const release = useCallback(() => {
    ref.current = false;
  }, []);
  const guard = useCallback(
    <A extends unknown[]>(fn: (...args: A) => void) =>
      (...args: A) => {
        if (ref.current) return;
        ref.current = true;
        fn(...args);
      },
    [],
  );
  return useMemo(() => ({ guard, release }), [guard, release]);
}
