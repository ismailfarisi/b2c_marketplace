"use client";

import { ApiError } from "@marketplace/api-client";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import { ConsoleShell } from "@/components/admin/shell";
import { SessionMenu, useWhoami } from "@/components/admin/session";

/**
 * The console's front door.
 *
 * **This is a routing decision, not a security control**, and the distinction is
 * the whole design. Every admin endpoint checks permissions server-side on every
 * call — the gate below could be deleted and nothing would leak; the console
 * would simply render a grid of 403s instead of a sign-in form. What it buys is
 * that an unauthenticated person sees one honest screen rather than fourteen
 * panels failing in parallel.
 *
 * It cannot be done in `middleware.ts` either, which is where a reviewer will
 * look first. The refresh token lives in `sessionStorage` and the access token
 * lives in memory (see `lib/token-store.ts`), so the *server* rendering the page
 * has no idea whether the browser holds a session. Moving the check to the edge
 * means moving the token into a cookie, which is the Phase 8 BFF work that
 * `token-store.ts` already names as the destination. Until then the gate is
 * client-side and openly so.
 */

/** Rendered before the shell, so a signed-out visitor never fires queue polls. */
function Waiting({ children }: { children: ReactNode }) {
  return (
    <div className="console-gate">
      <div className="console-gate-card">{children}</div>
    </div>
  );
}

function Gate({ locale, children }: { locale: string; children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const { data, error, isPending } = useWhoami();

  // A 401 means no usable session: either nothing was ever stored, or the
  // refresh token was rejected. Anything else — a 500, a dead API — is a
  // different problem and must not be dressed up as "please sign in", which
  // would send an operator hunting for a password during an outage.
  const unauthenticated = error instanceof ApiError && error.status === 401;

  useEffect(() => {
    if (!unauthenticated) return;
    router.replace(`/${locale}/admin/login?next=${encodeURIComponent(pathname)}`);
  }, [unauthenticated, router, locale, pathname]);

  if (isPending) {
    return <Waiting>Checking your access…</Waiting>;
  }

  if (unauthenticated) {
    return <Waiting>Taking you to sign in…</Waiting>;
  }

  if (error) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">The console could not reach the API.</div>
          {error instanceof ApiError && error.requestId ? (
            <p className="muted mono">request {error.requestId}</p>
          ) : null}
          <p className="muted">
            This is not a sign-in problem — the request failed before anything
            could be decided about you.
          </p>
        </div>
      </Waiting>
    );
  }

  // Signed in, and not staff. Deliberately *not* a redirect to the login page:
  // this person holds a perfectly valid session, so bouncing them to sign in
  // would loop them through a form that succeeds and returns them here. The
  // honest screen says what is missing and offers the only two useful exits.
  if (data && !data.is_staff) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">This account has no console access.</div>
        </div>
        <p className="muted">
          Signing in worked. What is missing is a <code>platform_admins</code>{" "}
          row — staff authority is granted per account and checked on the server
          on every call, never carried in the token. Someone holding{" "}
          <code>staff.admin</code> can grant it.
        </p>
        <div className="console-gate-actions">
          <SessionMenu locale={locale} />
        </div>
      </Waiting>
    );
  }

  return <ConsoleShell locale={locale}>{children}</ConsoleShell>;
}

export function AdminAuth({ locale, children }: { locale: string; children: ReactNode }) {
  const pathname = usePathname();

  // The login page is the one route under /admin that must render without a
  // session, and without the shell — a sidebar polling queue depths behind a
  // sign-in form would fire a 401 every thirty seconds at someone who is in the
  // middle of fixing exactly that.
  if (pathname === `/${locale}/admin/login`) {
    return <>{children}</>;
  }

  return (
    <Gate locale={locale}>{children}</Gate>
  );
}
