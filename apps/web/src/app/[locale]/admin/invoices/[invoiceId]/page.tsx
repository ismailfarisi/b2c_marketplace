"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { DetailError } from "@/components/admin/errors";
import { getInvoiceDocument } from "@/lib/admin-api";

/** Staff reprint of an issued invoice (money.read), in English. */
export default function AdminInvoicePage({
  params,
}: {
  params: Promise<{ locale: string; invoiceId: string }>;
}) {
  const { locale, invoiceId } = use(params);
  const q = useQuery({
    queryKey: ["admin", "invoice", invoiceId],
    queryFn: () => getInvoiceDocument(invoiceId),
  });
  if (q.data) return <InvoiceView invoice={q.data} locale="en" />;
  if (q.isError)
    return (
      <DetailError
        error={q.error}
        what="this invoice"
        perm="money.read"
        onRetry={() => void q.refetch()}
        back={{ href: `/${locale}/admin/refunds`, label: "Back to refunds & invoices" }}
      />
    );
  return <p className="console-lede">Loading…</p>;
}
