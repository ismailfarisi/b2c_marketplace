"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { listMemberships } from "@/lib/api";

/**
 * Branch picker.
 *
 * The console is branch-scoped everywhere below this page, not vendor-scoped,
 * because that is how the operation actually runs: a chain's three kitchens have
 * independent capacity, independent drivers and independent days, and a combined
 * view is a screen nobody can act on. `vendor_users.branch_id` already encodes
 * the same idea — a null there means vendor-wide.
 */
export default function ConsoleHome({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const isAr = locale === "ar";

  const { data, isPending, error } = useQuery({
    queryKey: ["memberships"],
    queryFn: listMemberships,
  });

  if (isPending) return <main className="page">{isAr ? "جارٍ التحميل…" : "Loading…"}</main>;

  if (error) {
    return (
      <main className="page">
        <h1>{isAr ? "تعذّر التحميل" : "Could not load"}</h1>
        <p className="muted">
          {isAr
            ? "سجّل الدخول بحساب موظّف لدى بائع."
            : "Sign in with an account that belongs to a vendor."}
        </p>
      </main>
    );
  }

  const items = data?.items ?? [];

  return (
    <main className="page">
      <h1>{isAr ? "لوحة التحكّم" : "Console"}</h1>

      {items.length === 0 && (
        <p className="muted">
          {isAr
            ? "هذا الحساب غير مرتبط بأي بائع."
            : "This account is not attached to any vendor."}
        </p>
      )}

      <ul className="stack">
        {items.map((m) => (
          <li key={`${m.vendor_id}:${m.branch_id ?? "all"}`} className="card">
            <div className="row">
              <span className="mono">{m.branch_id ?? m.vendor_id}</span>
              <span className="pill">{m.role}</span>
            </div>
            {m.branch_id ? (
              <Link href={`/${locale}/console/${m.branch_id}`}>
                {isAr ? "افتح الفرع" : "Open branch"}
              </Link>
            ) : (
              <p className="muted">
                {/* A vendor-wide membership has no single branch to open. Saying
                    so beats rendering a dead link. */}
                {isAr
                  ? "صلاحية على مستوى البائع — افتح فرعًا من الرابط المباشر."
                  : "Vendor-wide access — open a specific branch by its link."}
              </p>
            )}
          </li>
        ))}
      </ul>
    </main>
  );
}
