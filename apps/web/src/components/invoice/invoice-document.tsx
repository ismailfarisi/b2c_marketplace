"use client";

import { formatMoney } from "@marketplace/api-client";

import type { InvoiceDocument } from "@/lib/customer-api";

const COPY = {
  en: {
    tax_invoice: "Tax invoice",
    simplified: "Simplified tax invoice",
    credit_note: "Credit note",
    number: "Invoice no.",
    issued: "Date of issue",
    supply: "Date of supply",
    corrects: "Corrects invoice",
    supplier: "Supplier",
    buyer: "Customer",
    trn: "TRN",
    item: "Description",
    qty: "Qty",
    unit: "Unit price",
    net: "Net",
    vat: "VAT",
    total: "Total",
    subtotal: "Total excluding VAT",
    vatTotal: "VAT",
    grand: "Total including VAT",
    print: "Print or save as PDF",
    void: { cancelled: "Cancelled", superseded: "Superseded" },
  },
  ar: {
    tax_invoice: "فاتورة ضريبية",
    simplified: "فاتورة ضريبية مبسطة",
    credit_note: "إشعار دائن",
    number: "رقم الفاتورة",
    issued: "تاريخ الإصدار",
    supply: "تاريخ التوريد",
    corrects: "تصحيح للفاتورة",
    supplier: "المورد",
    buyer: "العميل",
    trn: "الرقم الضريبي",
    item: "الوصف",
    qty: "الكمية",
    unit: "سعر الوحدة",
    net: "الصافي",
    vat: "ضريبة القيمة المضافة",
    total: "الإجمالي",
    subtotal: "الإجمالي غير شامل الضريبة",
    vatTotal: "ضريبة القيمة المضافة",
    grand: "الإجمالي شامل الضريبة",
    print: "طباعة أو حفظ كملف PDF",
    void: { cancelled: "ملغاة", superseded: "مستبدلة" },
  },
} as const;

/**
 * The printable copy of an issued invoice. Renders the stored snapshot only;
 * the browser's print dialog produces the PDF ("Save as PDF").
 */
export function InvoiceView({
  invoice,
  locale,
}: {
  invoice: InvoiceDocument;
  locale: "en" | "ar";
}) {
  const t = COPY[locale];
  const m = (amount: number) =>
    formatMoney({ amount, currency: invoice.currency }, locale);
  const day = (iso: string) =>
    new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-AE", {
      dateStyle: "long",
    }).format(new Date(iso));

  return (
    <article
      className="inv"
      dir={locale === "ar" ? "rtl" : "ltr"}
      lang={locale}
    >
      <div className="inv-actions">
        <button type="button" onClick={() => window.print()}>
          {t.print}
        </button>
      </div>
      <header className="inv-head">
        <h1>{t[invoice.kind]}</h1>
        {invoice.status !== "issued" ? (
          <p className="inv-void">{t.void[invoice.status]}</p>
        ) : null}
        <dl>
          <dt>{t.number}</dt>
          <dd className="inv-num">{invoice.number}</dd>
          <dt>{t.issued}</dt>
          <dd>{day(invoice.issued_at)}</dd>
          <dt>{t.supply}</dt>
          <dd>{day(invoice.supply_date)}</dd>
          {invoice.corrects_number ? (
            <>
              <dt>{t.corrects}</dt>
              <dd className="inv-num">{invoice.corrects_number}</dd>
            </>
          ) : null}
        </dl>
      </header>
      <section className="inv-parties">
        {(
          [
            [t.supplier, invoice.supplier],
            [t.buyer, invoice.buyer],
          ] as const
        ).map(([label, p]) => (
          <div key={label}>
            <h2>{label}</h2>
            <p dir="auto">{p.name || "—"}</p>
            {p.address ? <p dir="auto">{p.address}</p> : null}
            {p.trn ? (
              <p>
                {t.trn}: <span className="inv-num">{p.trn}</span>
              </p>
            ) : null}
          </div>
        ))}
      </section>
      <div className="inv-scroll">
        <table className="inv-lines">
          <thead>
            <tr>
              <th>{t.item}</th>
              <th>{t.qty}</th>
              <th>{t.unit}</th>
              <th>{t.net}</th>
              <th>{t.vat}</th>
              <th>{t.total}</th>
            </tr>
          </thead>
          <tbody>
            {invoice.lines.map((l, i) => (
              <tr key={i}>
                <td dir="auto">
                  {locale === "ar" && l.description_ar
                    ? l.description_ar
                    : l.description}
                </td>
                <td>{l.quantity}</td>
                <td>{m(l.unit_net_minor)}</td>
                <td>{m(l.net_minor)}</td>
                <td>
                  {m(l.tax_minor)} <small>({l.tax_rate_bp / 100}%)</small>
                </td>
                <td>{m(l.gross_minor)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <dl className="inv-totals">
        <dt>{t.subtotal}</dt>
        <dd>{m(invoice.net_minor)}</dd>
        <dt>{t.vatTotal}</dt>
        <dd>{m(invoice.tax_minor)}</dd>
        <dt className="inv-grand">{t.grand}</dt>
        <dd className="inv-grand">{m(invoice.gross_minor)}</dd>
      </dl>
    </article>
  );
}
