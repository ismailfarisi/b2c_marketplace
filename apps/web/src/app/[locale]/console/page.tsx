"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect } from "react";

import { SignOut, useBranches } from "@/components/vendor/portal";

/**
 * Branch picker.
 *
 * The portal is branch-scoped everywhere below this page, because that is how
 * a kitchen runs: a chain's branches have independent menus, capacity and
 * drivers. Someone with a single branch never sees this page — they are sent
 * straight to it.
 */
export default function ConsoleHome({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const router = useRouter();
  const { data } = useBranches();
  const items = data?.items ?? [];
  const only = items.length === 1 ? items[0] : undefined;

  useEffect(() => {
    if (only) router.replace(`/${locale}/console/${only.branch_id}`);
  }, [only, router, locale]);

  return (
    <div className="console-gate">
      <div className="console-gate-card">
        <div className="console-login-brand">
          <span>Kitchen portal</span>
          <span className="env">{process.env.NEXT_PUBLIC_ENV ?? "dev"}</span>
        </div>
        {only ? (
          <p className="muted">Opening {only.branch_name}…</p>
        ) : (
          <>
            <p className="muted">Choose a branch.</p>
            <div className="vp-picker">
              {items.map((b) => (
                <Link key={b.branch_id} href={`/${locale}/console/${b.branch_id}`}>
                  <span>
                    <strong dir="auto">{b.vendor_name}</strong>
                    <br />
                    <span className="muted" dir="auto">
                      {b.branch_name}
                      {b.city ? ` · ${b.city}` : ""}
                    </span>
                  </span>
                  <span className="tag">{b.role}</span>
                </Link>
              ))}
            </div>
          </>
        )}
        <div className="console-gate-actions">
          <SignOut locale={locale} />
        </div>
      </div>
    </div>
  );
}
