"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { CustomerShell, STATUS, asLocale } from "@/components/meals/shell";
import { listMySubscriptions } from "@/lib/customer-api";

const COPY = {
  en: {
    title: "My subscriptions",
    empty: "You have no meal subscriptions yet.",
    browse: "Find a kitchen",
    cycle: (n: number, end: string | null) =>
      end ? `Cycle ${n}, ends ${end}` : `Cycle ${n}`,
    loading: "Loading…",
    failed: "Could not load your subscriptions.",
  },
  ar: {
    title: "اشتراكاتي",
    empty: "لا توجد لديك اشتراكات وجبات بعد.",
    browse: "ابحث عن مطبخ",
    cycle: (n: number, end: string | null) =>
      end ? `الدورة ${n}، تنتهي ${end}` : `الدورة ${n}`,
    loading: "جارٍ التحميل…",
    failed: "تعذر تحميل اشتراكاتك.",
  },
};

export default function MySubscriptionsPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "subscriptions"],
    queryFn: listMySubscriptions,
  });
  const items = q.data?.items ?? [];

  return (
    <CustomerShell locale={locale}>
      <h1>{t.title}</h1>
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        <p className="ms-empty">{t.failed}</p>
      ) : items.length === 0 ? (
        <div className="ms-empty">
          <p>{t.empty}</p>
          <Link className="ms-cta" href={`/${locale}`}>
            {t.browse}
          </Link>
        </div>
      ) : (
        <ul className="ms-subs">
          {items.map((s) => (
            <li key={s.id}>
              <Link
                href={`/${locale}/subscriptions/${s.id}`}
                className="ms-sub-card"
              >
                <span>
                  <strong dir="auto">
                    {(locale === "ar" && s.kitchen_name_ar) || s.kitchen_name}
                  </strong>
                  <br />
                  <span className="ms-sub" dir="auto">
                    {(locale === "ar" && s.plan_name_ar) || s.plan_name} ·{" "}
                    {t.cycle(s.cycle_no, s.cycle_ends_on)}
                  </span>
                </span>
                <span className="ms-sub-side">
                  <span className={`ms-status ms-status-${s.status}`}>
                    {STATUS[locale][s.status] ?? s.status}
                  </span>
                  <span>
                    {formatMoney(
                      { amount: s.amount_minor, currency: s.currency },
                      locale,
                    )}
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </CustomerShell>
  );
}
