"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { CustomerShell, asLocale, formatDay } from "@/components/meals/shell";
import { listMyInvoices } from "@/lib/customer-api";

const COPY = {
  en: {
    title: "Invoices",
    empty:
      "No invoices yet. An invoice is issued once a paid order is delivered.",
    failed: "Could not load your invoices.",
    signIn: "Sign in to see your invoices",
    credit: "Credit note",
  },
  ar: {
    title: "الفواتير",
    empty: "لا توجد فواتير بعد. تصدر الفاتورة بعد توصيل الطلب المدفوع.",
    failed: "تعذّر تحميل الفواتير.",
    signIn: "سجّل الدخول لعرض فواتيرك",
    credit: "إشعار دائن",
  },
} as const;

export default function InvoicesPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "invoices"],
    queryFn: listMyInvoices,
    retry: false,
  });
  const signedOut = q.error instanceof ApiError && q.error.status === 401;

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
        ) : null}
        {q.isError && !signedOut ? (
          <p className="ms-error">{t.failed}</p>
        ) : null}
        {q.data && q.data.items.length === 0 ? <p>{t.empty}</p> : null}
        <ul className="ms-support-list">
          {q.data?.items.map((inv) => (
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
      </div>
    </CustomerShell>
  );
}
