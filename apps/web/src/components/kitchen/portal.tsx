"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

import { logout } from "@/lib/auth-api";
import { problemFields, problemMessage } from "@/lib/problem";
import { listMyBranches, type BranchAccess } from "@/lib/vendor-api";

import { consoleCopy, consoleLocale, type ConsoleCopy } from "./copy";

/**
 * The vendor portal's frame: the sign-in gate, the sidebar and the branch
 * context every page below reads.
 *
 * It reuses the staff console's stylesheet and layout primitives rather than
 * growing a second design system. A kitchen owner and an ops agent are looking
 * at the same kind of screen — tables, forms, a queue — and one set of
 * components means a fix to one is a fix to both.
 *
 * Like the staff gate, this is routing, not security. Every endpoint checks
 * branch membership and role on the server; the gate exists so a signed-out
 * visitor sees one sign-in screen rather than a page of 401s.
 */

export const BRANCHES_KEY = ["vendor", "branches"] as const;

export function useBranches() {
  return useQuery({
    queryKey: BRANCHES_KEY,
    queryFn: listMyBranches,
    staleTime: 5 * 60_000,
    refetchOnReconnect: true,
    retry: (count, err) =>
      !(err instanceof ApiError && err.status === 401) && count < 2,
  });
}

export function canEdit(role: BranchAccess["role"]): boolean {
  return role === "owner" || role === "manager";
}

/**
 * One sentence for a failed call. English readers get the server's own
 * sentence (written for the kitchen); Arabic readers get the localized one for
 * its code. A call that never got a response (fetch's TypeError) is a
 * connection problem, said as such — never the browser's "Failed to fetch".
 */
export function errorText(err: unknown, locale: string = "en"): string {
  const t = consoleCopy(locale);
  if (err instanceof ApiError) {
    if (consoleLocale(locale) === "ar") return problemMessage(err, "ar");
    const fields = err.problem?.errors ?? [];
    if (fields.length > 0) {
      return fields.map((f) => f.message ?? `${f.field}: ${f.code}`).join("; ");
    }
    return err.problem?.detail ?? err.problem?.title ?? err.message;
  }
  if (err instanceof TypeError || (typeof navigator !== "undefined" && navigator.onLine === false)) {
    return t.errors.offline;
  }
  return t.errors.failed;
}

/** Field-level errors from a 422, keyed by field name, in the reader's language. */
export function fieldErrors(err: unknown, locale: string = "en"): Record<string, string> {
  if (!(err instanceof ApiError)) return {};
  return consoleLocale(locale) === "ar" ? problemFields(err, "ar") : err.fieldErrors;
}

/** A failed load: the reason, the request id for a server failure, and Try again. */
export function ConsoleError({
  error,
  onRetry,
  locale,
}: {
  error: unknown;
  onRetry?: (() => void) | undefined;
  locale: string;
}) {
  const t = consoleCopy(locale);
  const requestId = error instanceof ApiError && error.status >= 500 ? error.requestId : null;
  const final = error instanceof ApiError && (error.status === 403 || error.status === 404);
  return (
    <div className="state error vp-error" role="alert">
      <span>{errorText(error, locale)}</span>
      {requestId ? (
        <span className="muted mono">
          {" "}
          {t.errors.request} {requestId}
        </span>
      ) : null}
      {onRetry && !final ? (
        <button type="button" className="btn" onClick={onRetry}>
          {t.retry}
        </button>
      ) : null}
    </div>
  );
}

function Waiting({ children }: { children: ReactNode }) {
  return (
    <div className="console-gate">
      <div className="console-gate-card">{children}</div>
    </div>
  );
}

export function SignOut({ locale }: { locale: string }) {
  const t = consoleCopy(locale);
  const router = useRouter();
  const queryClient = useQueryClient();
  const signOut = useMutation({
    mutationFn: () => logout(),
    onSettled: () => {
      queryClient.clear();
      router.replace(`/${locale}/console/login`);
    },
  });
  return (
    <button
      type="button"
      className="btn link"
      onClick={() => signOut.mutate()}
      disabled={signOut.isPending}
    >
      {signOut.isPending ? t.signingOut : t.signOut}
    </button>
  );
}

export function VendorGate({
  locale,
  children,
}: {
  locale: string;
  children: ReactNode;
}) {
  const pathname = usePathname();
  if (pathname === `/${locale}/console/login`) return <>{children}</>;
  return <Gate locale={locale}>{children}</Gate>;
}

function Gate({ locale, children }: { locale: string; children: ReactNode }) {
  const t = consoleCopy(locale);
  const router = useRouter();
  const pathname = usePathname();
  const { data, error, isPending, refetch, isRefetching } = useBranches();
  const unauthenticated = error instanceof ApiError && error.status === 401;

  useEffect(() => {
    if (!unauthenticated) return;
    router.replace(
      `/${locale}/console/login?next=${encodeURIComponent(pathname)}`,
    );
  }, [unauthenticated, router, locale, pathname]);

  if (isPending) return <Waiting>{t.gate.checking}</Waiting>;
  if (unauthenticated) return <Waiting>{t.gate.toSignIn}</Waiting>;
  if (error) {
    return (
      <Waiting>
        <div className="state error" role="alert">
          <div className="headline">
            {error instanceof TypeError ? t.errors.offline : t.gate.unreachable}
          </div>
          {error instanceof ApiError && error.requestId ? (
            <p className="muted mono">
              {t.errors.request} {error.requestId}
            </p>
          ) : null}
        </div>
        <div className="console-gate-actions">
          <button
            type="button"
            className="btn primary"
            onClick={() => void refetch()}
            disabled={isRefetching}
          >
            {isRefetching ? t.loading : t.retry}
          </button>
          <SignOut locale={locale} />
        </div>
      </Waiting>
    );
  }
  if (data.items.length === 0) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">{t.gate.noKitchen}</div>
        </div>
        <p className="muted">{t.gate.noKitchenBody}</p>
        <div className="console-gate-actions">
          <SignOut locale={locale} />
        </div>
      </Waiting>
    );
  }
  return <>{children}</>;
}

// ── the frame ───────────────────────────────────────────────────────────────

type BranchContext = BranchAccess & {
  editable: boolean;
  base: string;
  locale: string;
  t: ConsoleCopy;
};

const Ctx = createContext<BranchContext | null>(null);

export function useBranch(): BranchContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useBranch outside VendorShell");
  return ctx;
}

type NavKey = keyof Omit<ConsoleCopy["nav"], "sections" | "branch" | "menuToggle" | "groups">;

const NAV: Array<{
  group: keyof ConsoleCopy["nav"]["groups"] | null;
  items: Array<{ href: string; key: NavKey }>;
}> = [
  { group: null, items: [{ href: "", key: "overview" }] },
  {
    group: "kitchen",
    items: [
      { href: "/menu", key: "menu" },
      { href: "/dishes", key: "dishes" },
      { href: "/plans", key: "plans" },
    ],
  },
  {
    group: "operations",
    items: [
      { href: "/prep", key: "prep" },
      { href: "/deliveries", key: "deliveries" },
      { href: "/reviews", key: "reviews" },
    ],
  },
  { group: "money", items: [{ href: "/payouts", key: "payouts" }] },
  { group: "settings", items: [{ href: "/profile", key: "profile" }] },
];

const TITLES: Record<string, NavKey> = Object.fromEntries(
  NAV.flatMap((g) => g.items.map((i) => [i.href, i.key] as const)),
);

export function VendorShell({
  locale,
  branchId,
  children,
}: {
  locale: string;
  branchId: string;
  children: ReactNode;
}) {
  const t = consoleCopy(locale);
  const router = useRouter();
  const pathname = usePathname();
  const { data } = useBranches();
  const [navOpen, setNavOpen] = useState(false);
  useEffect(() => setNavOpen(false), [pathname]);
  const branches = data?.items ?? [];
  const branch = branches.find((b) => b.branch_id === branchId);
  const base = `/${locale}/console/${branchId}`;
  const section = pathname.startsWith(base) ? pathname.slice(base.length) : "";
  const current =
    Object.keys(TITLES)
      .filter((k) => k !== "" && section.startsWith(k))
      .sort((a, b) => b.length - a.length)[0] ?? "";

  if (!branch) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">{t.gate.notYours}</div>
        </div>
        <Link className="btn" href={`/${locale}/console`}>
          {t.gate.chooseBranch}
        </Link>
      </Waiting>
    );
  }

  const ctx: BranchContext = {
    ...branch,
    editable: canEdit(branch.role),
    base,
    locale,
    t,
  };

  return (
    <Ctx.Provider value={ctx}>
      <div className={`console vp-shell ${navOpen ? "vp-nav-open" : ""}`}>
        <nav className="console-nav" aria-label={t.nav.sections}>
          <div className="console-brand">
            <span>{t.brand}</span>
            <span className="env">{process.env.NEXT_PUBLIC_ENV ?? "dev"}</span>
            <button
              type="button"
              className="vp-nav-toggle"
              aria-expanded={navOpen}
              aria-controls="vp-nav-links"
              onClick={() => setNavOpen((o) => !o)}
            >
              {t.nav.menuToggle}
            </button>
          </div>
          <div id="vp-nav-links" className="vp-nav-links">

          <div className="vp-branch">
            <span className="vendor" dir="auto">
              {branch.vendor_name}
            </span>
            {branches.length > 1 ? (
              <select
                aria-label={t.nav.branch}
                value={branchId}
                onChange={(e) =>
                  router.push(`/${locale}/console/${e.target.value}${current}`)
                }
              >
                {branches.map((b) => (
                  <option key={b.branch_id} value={b.branch_id}>
                    {b.vendor_name === branch.vendor_name
                      ? b.branch_name
                      : `${b.vendor_name} · ${b.branch_name}`}
                  </option>
                ))}
              </select>
            ) : (
              <span dir="auto">{branch.branch_name}</span>
            )}
          </div>

          {NAV.map((group) => (
            <div key={group.group ?? "top"}>
              {group.group ? (
                <div className="console-group">{t.nav.groups[group.group]}</div>
              ) : null}
              {group.items.map((item) => {
                const text = t.nav[item.key];
                return (
                  <Link
                    key={item.href}
                    href={`${base}${item.href}`}
                    className={`console-link ${current === item.href ? "active" : ""}`}
                    title={text}
                    aria-label={text}
                    aria-current={current === item.href ? "page" : undefined}
                  >
                    <span className="abbr" aria-hidden="true">
                      {Array.from(text).slice(0, 2).join("")}
                    </span>
                    <span className="label">{text}</span>
                  </Link>
                );
              })}
            </div>
          ))}
          </div>
        </nav>

        <div className="console-main">
          <header className="console-appbar">
            <h1>{t.nav[TITLES[current] ?? "overview"]}</h1>
            {branch.vendor_status !== "active" ? (
              <span className="tag warn">{t.vendorStatus(branch.vendor_status)}</span>
            ) : null}
            <div className="spacer" />
            <div className="console-session">
              <span className="who">{t.roles[branch.role] ?? branch.role}</span>
              <SignOut locale={locale} />
            </div>
          </header>
          <div className="console-content">
            {!ctx.editable ? (
              <div className="vp-readonly">{t.readOnly}</div>
            ) : null}
            {children}
          </div>
        </div>
      </div>
    </Ctx.Provider>
  );
}
