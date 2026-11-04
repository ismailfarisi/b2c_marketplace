"use client";

import { ApiError } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { LoadError } from "@/components/load-error";
import { asLocale } from "@/components/meals/shell";
import { getMyInvoice } from "@/lib/customer-api";

const COPY = {
  en: {
    back: "All invoices",
    loading: "Loading…",
    signIn: "Sign in to see this invoice",
    notFound: "This invoice isn't on your account.",
  },
  ar: {
    back: "كل الفواتير",
    loading: "جارٍ التحميل…",
    signIn: "سجّل الدخول لعرض هذه الفاتورة",
    notFound: "هذه الفاتورة ليست في حسابك.",
  },
} as const;

export default function InvoicePage({
  params,
}: {
  params: Promise<{ locale: string; invoiceId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "invoices", p.invoiceId],
    queryFn: () => getMyInvoice(p.invoiceId),
    retry: false,
  });
  const list = `/${locale}/invoices`;

  if (q.data)
    return (
      <InvoiceView
        invoice={q.data}
        locale={locale}
        back={{ href: list, label: t.back }}
      />
    );

  const status = q.error instanceof ApiError ? q.error.status : 0;
  const here = `/${locale}/invoices/${p.invoiceId}`;
  return (
    <main
      className="ms"
      dir={locale === "ar" ? "rtl" : "ltr"}
      lang={locale}
      style={{ padding: "2rem 1rem", maxWidth: 560, margin: "0 auto", textAlign: "center" }}
    >
      {q.isPending ? (
        <p aria-busy="true">{t.loading}</p>
      ) : status === 401 ? (
        <Link
          className="ms-btn"
          href={`/${locale}/signin?next=${encodeURIComponent(here)}`}
        >
          {t.signIn}
        </Link>
      ) : status === 404 ? (
        <p role="alert">{t.notFound}</p>
      ) : (
        <LoadError
          error={q.error}
          onRetry={() => void q.refetch()}
          locale={locale}
        />
      )}
      <p>
        <Link href={list}>
          <span aria-hidden>{locale === "ar" ? "→" : "←"}</span> {t.back}
        </Link>
      </p>
    </main>
  );
}
