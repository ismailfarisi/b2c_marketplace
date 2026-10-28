"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { CustomerShell, STATUS, asLocale } from "@/components/meals/shell";
import { listMyRotations, listMySubscriptions } from "@/lib/customer-api";

const COPY = {
  en: {
    title: "My subscriptions",
    empty: "You have no meal subscriptions yet.",
    browse: "Find a kitchen",
    cycle: (n: number, end: string | null) =>
      end ? `Cycle ${n}, ends ${end}` : `Cycle ${n}`,
    loading: "Loading…",
    failed: "Could not load your subscriptions.",
    rotations: "4-kitchen rotations",
    rotationTitle: "4-kitchen rotation",
    rotationWeek: (n: number) => `Rotation week ${n}`,
    build: "Build a 4-kitchen rotation",
  },
  ar: {
    title: "اشتراكاتي",
    empty: "لا توجد لديك اشتراكات وجبات بعد.",
    browse: "ابحث عن مطبخ",
    cycle: (n: number, end: string | null) =>
      end ? `الدورة ${n}، تنتهي ${end}` : `الدورة ${n}`,
    loading: "جارٍ التحميل…",
    failed: "تعذر تحميل اشتراكاتك.",
    rotations: "تناوب ٤ مطابخ",
    rotationTitle: "تناوب ٤ مطابخ",
    rotationWeek: (n: number) => `الأسبوع ${n} من التناوب`,
    build: "اختر تناوب ٤ مطابخ",
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
  const rq = useQuery({
    queryKey: ["me", "rotations"],
    queryFn: listMyRotations,
  });
  const rotations = rq.data?.items ?? [];

  return (
    <CustomerShell locale={locale}>
      <h1>{t.title}</h1>
      {rotations.length > 0 && (
        <>
          <h2>{t.rotations}</h2>
          <ul className="ms-subs">
            {rotations.map((r) => (
              <li key={r.id}>
                <Link
                  href={`/${locale}/rotations/${r.id}`}
                  className="ms-sub-card"
                >
                  <span>
                    <strong>{t.rotationTitle}</strong>
                    <br />
                    <span className="ms-sub" dir="auto">
                      {r.weeks
                        .map(
                          (w) =>
                            (locale === "ar" && w.kitchen_name_ar) ||
                            w.kitchen_name,
                        )
                        .join(" → ")}
                    </span>
                    <br />
                    <span className="ms-sub">
                      {r.start_date} – {r.end_date}
                    </span>
                  </span>
                  <span className="ms-sub-side">
                    <span className={`ms-status ms-status-${r.status}`}>
                      {STATUS[locale][r.status] ?? r.status}
                    </span>
                    <span>
                      {formatMoney(
                        { amount: r.amount_minor, currency: r.currency },
                        locale,
                      )}
                    </span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        <p className="ms-empty">{t.failed}</p>
      ) : items.length === 0 ? (
        <div className="ms-empty">
          <p>{t.empty}</p>
          <Link className="ms-cta" href={`/${locale}`}>
            {t.browse}
          </Link>{" "}
          <Link className="ms-link" href={`/${locale}/rotation`}>
            {t.build}
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
                    {s.rotation_week != null
                      ? t.rotationWeek(s.rotation_week)
                      : t.cycle(s.cycle_no, s.cycle_ends_on)}
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
