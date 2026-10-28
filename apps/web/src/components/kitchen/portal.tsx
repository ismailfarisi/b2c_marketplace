"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, type ReactNode } from "react";

import { logout } from "@/lib/auth-api";
import { listMyBranches, type BranchAccess } from "@/lib/vendor-api";

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
    retry: (count, err) =>
      !(err instanceof ApiError && err.status === 401) && count < 2,
  });
}

export function canEdit(role: BranchAccess["role"]): boolean {
  return role === "owner" || role === "manager";
}

/** The server's sentence for a failed call, not a generic one. */
export function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.problem?.errors ?? [];
    if (fields.length > 0) {
      return fields.map((f) => f.message ?? `${f.field}: ${f.code}`).join("; ");
    }
    return err.problem?.detail ?? err.problem?.title ?? err.message;
  }
  return err instanceof Error ? err.message : "Request failed.";
}

/** Field-level errors from a 422, keyed by field name. */
export function fieldErrors(err: unknown): Record<string, string> {
  return err instanceof ApiError ? err.fieldErrors : {};
}

function Waiting({ children }: { children: ReactNode }) {
  return (
    <div className="console-gate">
      <div className="console-gate-card">{children}</div>
    </div>
  );
}

export function SignOut({ locale }: { locale: string }) {
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
      {signOut.isPending ? "Signing out…" : "Sign out"}
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
  const router = useRouter();
  const pathname = usePathname();
  const { data, error, isPending } = useBranches();
  const unauthenticated = error instanceof ApiError && error.status === 401;

  useEffect(() => {
    if (!unauthenticated) return;
    router.replace(
      `/${locale}/console/login?next=${encodeURIComponent(pathname)}`,
    );
  }, [unauthenticated, router, locale, pathname]);

  if (isPending) return <Waiting>Checking your access…</Waiting>;
  if (unauthenticated) return <Waiting>Taking you to sign in…</Waiting>;
  if (error) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">The portal could not reach the API.</div>
          {error instanceof ApiError && error.requestId ? (
            <p className="muted mono">request {error.requestId}</p>
          ) : null}
        </div>
      </Waiting>
    );
  }
  if (data.items.length === 0) {
    return (
      <Waiting>
        <div className="state error">
          <div className="headline">
            This account is not attached to a kitchen.
          </div>
        </div>
        <p className="muted">
          Signing in worked. Ask the kitchen&apos;s owner to add this number to
          their team, or contact the marketplace to onboard a new kitchen.
        </p>
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
};

const Ctx = createContext<BranchContext | null>(null);

export function useBranch(): BranchContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useBranch outside VendorShell");
  return ctx;
}

const NAV: Array<{
  title: string | null;
  items: Array<{ href: string; label: string }>;
}> = [
  { title: null, items: [{ href: "", label: "Overview" }] },
  {
    title: "Kitchen",
    items: [
      { href: "/menu", label: "Weekly menu" },
      { href: "/dishes", label: "Dishes" },
      { href: "/plans", label: "Plans & pricing" },
    ],
  },
  {
    title: "Operations",
    items: [
      { href: "/prep", label: "Prep sheet" },
      { href: "/deliveries", label: "Deliveries & dispatch" },
      { href: "/reviews", label: "Reviews" },
    ],
  },
  { title: "Money", items: [{ href: "/payouts", label: "Payouts" }] },
  {
    title: "Settings",
    items: [{ href: "/profile", label: "Kitchen profile" }],
  },
];

const TITLES: Record<string, string> = {
  "": "Overview",
  "/menu": "Weekly menu",
  "/dishes": "Dishes",
  "/plans": "Plans & pricing",
  "/prep": "Prep sheet",
  "/deliveries": "Deliveries & dispatch",
  "/reviews": "Reviews",
  "/payouts": "Payouts",
  "/profile": "Kitchen profile",
};

export function VendorShell({
  locale,
  branchId,
  children,
}: {
  locale: string;
  branchId: string;
  children: ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const { data } = useBranches();
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
          <div className="headline">This branch is not one of yours.</div>
        </div>
        <Link className="btn" href={`/${locale}/console`}>
          Choose a branch
        </Link>
      </Waiting>
    );
  }

  const ctx: BranchContext = {
    ...branch,
    editable: canEdit(branch.role),
    base,
    locale,
  };

  return (
    <Ctx.Provider value={ctx}>
      <div className="console">
        <nav className="console-nav" aria-label="Sections">
          <div className="console-brand">
            <span>Kitchen</span>
            <span className="env">{process.env.NEXT_PUBLIC_ENV ?? "dev"}</span>
          </div>

          <div className="vp-branch">
            <span className="vendor" dir="auto">
              {branch.vendor_name}
            </span>
            {branches.length > 1 ? (
              <select
                aria-label="Branch"
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
            <div key={group.title ?? "top"}>
              {group.title ? (
                <div className="console-group">{group.title}</div>
              ) : null}
              {group.items.map((item) => (
                <Link
                  key={item.href}
                  href={`${base}${item.href}`}
                  className={`console-link ${current === item.href ? "active" : ""}`}
                  title={item.label}
                  aria-label={item.label}
                >
                  <span className="abbr" aria-hidden="true">
                    {item.label.slice(0, 2)}
                  </span>
                  <span className="label">{item.label}</span>
                </Link>
              ))}
            </div>
          ))}
        </nav>

        <div className="console-main">
          <header className="console-appbar">
            <h1>{TITLES[current] ?? "Kitchen"}</h1>
            {branch.vendor_status !== "active" ? (
              <span className="tag warn">vendor {branch.vendor_status}</span>
            ) : null}
            <div className="spacer" />
            <div className="console-session">
              <span className="who">{branch.role}</span>
              <SignOut locale={locale} />
            </div>
          </header>
          <div className="console-content">
            {!ctx.editable ? (
              <div className="vp-readonly">
                You are signed in as staff, so this portal is read-only. An
                owner or manager can change the menu, plans and profile.
              </div>
            ) : null}
            {children}
          </div>
        </div>
      </div>
    </Ctx.Provider>
  );
}
