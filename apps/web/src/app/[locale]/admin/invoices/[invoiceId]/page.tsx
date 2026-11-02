"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { getInvoiceDocument } from "@/lib/admin-api";

/** Staff reprint of an issued invoice (money.read), in English. */
export default function AdminInvoicePage({
  params,
}: {
  params: Promise<{ invoiceId: string }>;
}) {
  const { invoiceId } = use(params);
  const q = useQuery({
    queryKey: ["admin", "invoice", invoiceId],
    queryFn: () => getInvoiceDocument(invoiceId),
  });
  if (q.data) return <InvoiceView invoice={q.data} locale="en" />;
  return (
    <p className="console-lede">
      {q.isError ? "Could not load this invoice." : "Loading…"}
    </p>
  );
}
