"use client";

import { ApiError } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { messageOf } from "@/components/admin/detail";
import { InvoiceView } from "@/components/invoice/invoice-document";
import "@/components/invoice/invoice.css";
import { getInvoiceDocument } from "@/lib/admin-api";

/**
 * Staff reprint of an issued invoice (money.read). English by default; the
 * Arabic copy is one click away because customers ask for it.
 */
export default function AdminInvoicePage({
  params,
}: {
  params: Promise<{ invoiceId: string }>;
}) {
  const { invoiceId } = use(params);
  const [lang, setLang] = useState<"en" | "ar">("en");
  const q = useQuery({
    queryKey: ["admin", "invoice", invoiceId],
    queryFn: () => getInvoiceDocument(invoiceId),
  });
  const back = { href: "/en/admin/refunds", label: "Refunds & invoices" };
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
  const status = q.error instanceof ApiError ? q.error.status : 0;
  return (
    <div className="console-lede">
      {q.isPending ? (
        <p>Loading…</p>
      ) : (
        <div className="state error" role="alert">
          <div className="headline">
            {status === 404
              ? "No invoice with this id."
              : status === 403
                ? "This account cannot read invoices (needs money.read)."
                : "Could not load this invoice."}
          </div>
          {status !== 404 && status !== 403 ? (
            <>
              {messageOf(q.error)}{" "}
              <button type="button" onClick={() => void q.refetch()}>
                Retry
              </button>
            </>
          ) : null}
        </div>
      )}
      <p>
        <a href={back.href}>← {back.label}</a>
      </p>
    </div>
  );
}
