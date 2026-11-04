"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useInfiniteQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale, formatDay } from "@/components/meals/shell";
import { listMyInvoices } from "@/lib/customer-api";

const COPY = {
  en: {
    title: "Invoices",
    empty:
      "No invoices yet. An invoice is issued once a paid order is delivered.",
    failed: "Could not load older invoices.",
    signIn: "Sign in to see your invoices",
    credit: "Credit note",
    loading: "Loading…",
    older: "Show older",
    loadingOlder: "Loading older…",
  },
  ar: {
    title: "الفواتير",
    empty: "لا توجد فواتير بعد. تصدر الفاتورة بعد توصيل الطلب المدفوع.",
    failed: "تعذّر تحميل الفواتير الأقدم.",
    signIn: "سجّل الدخول لعرض فواتيرك",
    credit: "إشعار دائن",
    loading: "جارٍ التحميل…",
    older: "عرض الأقدم",
    loadingOlder: "جارٍ تحميل الأقدم…",
  },
} as const;

export default function InvoicesPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const q = useInfiniteQuery({
    queryKey: ["me", "invoices"],
    queryFn: ({ pageParam }) => listMyInvoices(pageParam || undefined),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    retry: false,
  });
  const signedOut = q.error instanceof ApiError && q.error.status === 401;
  const items = q.data?.pages.flatMap((p) => p.items ?? []) ?? [];

  return (
    <CustomerShell locale={locale}>
      <div className="ms-support">
        <h1>{t.title}</h1>
        {signedOut ? (
          <Link
            className="ms-btn"
            href={`/${locale}/signin?next=/${locale}/invoices`}
          >
            {t.signIn}
          </Link>
        ) : q.isError ? (
          <LoadError
            error={q.error}
            onRetry={() => void q.refetch()}
            locale={locale}
          />
        ) : q.isPending ? (
          <p className="ms-note" aria-busy="true">
            {t.loading}
          </p>
        ) : null}
        {q.isSuccess && items.length === 0 ? <p>{t.empty}</p> : null}
        <ul className="ms-support-list">
          {items.map((inv) => (
            <li key={inv.id}>
              <Link href={`/${locale}/invoices/${inv.id}`}>
                <strong className="inv-num">{inv.number}</strong>
                <span>{formatMoney(inv.total, locale)}</span>
                <small>
                  {formatDay(locale, inv.supply_date)}
                  {inv.kind === "credit_note" ? ` · ${t.credit}` : ""}
                </small>
              </Link>
            </li>
          ))}
        </ul>
        {q.hasNextPage ? (
          <button
            type="button"
            className="ms-link"
            onClick={() => void q.fetchNextPage()}
            disabled={q.isFetchingNextPage}
          >
            {q.isFetchingNextPage ? t.loadingOlder : t.older}
          </button>
        ) : null}
        {q.isFetchNextPageError ? <p className="ms-error">{t.failed}</p> : null}
      </div>
    </CustomerShell>
  );
}
