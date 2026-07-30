"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";

import { getWhoami, type Whoami } from "@/lib/admin-api";
import { logout } from "@/lib/auth-api";

/**
 * Who is signed in, and the control that ends it.
 *
 * Its own module rather than living in `shell.tsx` because the gate in
 * `auth.tsx` needs the same query and `shell.tsx` imports nothing from the gate
 * — a cycle between the frame and the thing that decides whether to render the
 * frame is the kind that survives review and then breaks under fast refresh.
 */

export const WHOAMI_KEY = ["admin", "whoami"] as const;

/**
 * The one call that decides what this person may do.
 *
 * Staff access is not a token claim. It is a `platform_admins` row read on the
 * server on every request, so a grant revoked at 14:00 stops working at 14:00
 * rather than whenever the holder's token happens to expire. The consequence
 * here is that this answer is a *cache*, and a short one: five minutes, plus a
 * refetch whenever the tab regains focus.
 */
export function useWhoami() {
  return useQuery({
    queryKey: WHOAMI_KEY,
    queryFn: getWhoami,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: true,
  });
}

/** The most specific thing we can say about someone, for the app bar. */
export function describe(who: Whoami): string {
  if (who.roles.length > 0) return who.roles.join(", ").replace(/_/g, " ");
  return who.is_staff ? "staff" : "no access";
}

export function SessionMenu({ locale }: { locale: string }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data } = useWhoami();

  const signOut = useMutation({
    mutationFn: () => logout(),
    // `onSettled`, not `onSuccess`. The local token is already gone either way
    // — `logout()` clears it in a `finally` — so leaving a console rendered
    // against a cleared store would just show a wall of 401s.
    onSettled: () => {
      // Everything in the cache was fetched as this person. Clearing rather
      // than invalidating means the next account cannot see a stale flash of
      // the last one's queues.
      queryClient.clear();
      router.replace(`/${locale}/admin/login`);
    },
  });

  if (!data) return null;

  return (
    <div className="console-session">
      <span className="who" title={`${data.permissions.length} permissions`}>
        {describe(data)}
      </span>
      <button
        type="button"
        className="btn link"
        onClick={() => signOut.mutate()}
        disabled={signOut.isPending}
      >
        {signOut.isPending ? "Signing out…" : "Sign out"}
      </button>
    </div>
  );
}
