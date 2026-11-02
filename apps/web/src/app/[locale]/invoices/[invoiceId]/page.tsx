"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { asLocale } from "@/components/meals/shell";
import { getMyInvoice } from "@/lib/customer-api";

export default function InvoicePage({
  params,
}: {
  params: Promise<{ locale: string; invoiceId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const q = useQuery({
    queryKey: ["me", "invoices", p.invoiceId],
    queryFn: () => getMyInvoice(p.invoiceId),
    retry: false,
  });

  if (q.data) return <InvoiceView invoice={q.data} locale={locale} />;
  return (
    <p style={{ padding: "2rem", textAlign: "center" }}>
      {q.isError ? (
        <Link href={`/${locale}/invoices`}>
          {locale === "ar"
            ? "تعذّر تحميل الفاتورة"
            : "Could not load this invoice"}
        </Link>
      ) : locale === "ar" ? (
        "جارٍ التحميل…"
      ) : (
        "Loading…"
      )}
    </p>
  );
}
