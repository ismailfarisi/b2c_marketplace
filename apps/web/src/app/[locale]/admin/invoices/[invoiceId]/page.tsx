"use client";

import { ApiError } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { messageOf } from "@/components/admin/detail";
import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { DetailError } from "@/components/admin/errors";
import { getInvoiceDocument } from "@/lib/admin-api";

/**
 * Staff reprint of an issued invoice (money.read). English by default; the
 * Arabic copy is one click away because customers ask for it.
 */
export default function AdminInvoicePage({
  params,
}: {
  params: Promise<{ locale: string; invoiceId: string }>;
}) {
  const { locale, invoiceId } = use(params);
  const [lang, setLang] = useState<"en" | "ar">("en");
  const q = useQuery({
    queryKey: ["admin", "invoice", invoiceId],
    queryFn: () => getInvoiceDocument(invoiceId),
  });
  const back = { href: `/${locale}/admin/refunds`, label: "Refunds & invoices" };
  if (q.data)
    return (
      <InvoiceView
        invoice={q.data}
        locale={lang}
        back={lang === "en" ? back : { ...back, label: "المبالغ المستردة والفواتير" }}
        actions={
          <button
            type="button"
            onClick={() => setLang(lang === "en" ? "ar" : "en")}
          >
            {lang === "en" ? "عربي" : "English"}
          </button>
        }
      />
    );
  if (q.isError)
    return (
      <DetailError
        error={q.error}
        what="this invoice"
        perm="money.read"
        onRetry={() => void q.refetch()}
        back={{ href: back.href, label: "Back to refunds & invoices" }}
      />
    );
  return <p className="console-lede">Loading…</p>;
}
