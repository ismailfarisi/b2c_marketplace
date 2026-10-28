"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import {
  CustomerShell,
  STATUS,
  asLocale,
  formatDay,
  formatRange,
} from "@/components/meals/shell";
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
    singles: "Kitchen subscriptions",
    weekShort: (n: number) => `Week ${n}`,
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
    singles: "اشتراكات المطابخ",
    weekShort: (n: number) => `الأسبوع ${n}`,
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
  const singles = items.filter((s) => s.rotation_id == null);
  const money = (amount: number, currency: string) =>
    formatMoney({ amount, currency }, locale);
  const status = (st: string) => (
    <span className={`ms-status ms-status-${st}`}>
      {STATUS[locale][st] ?? st}
    </span>
  );

  return (
    <CustomerShell locale={locale}>
      <h1>{t.title}</h1>
      {rotations.length > 0 && (
        <section>
          <h2>{t.rotations}</h2>
          <ul className="ms-subs">
            {rotations.map((r) => (
              <li key={r.id} className="ms-rot-card">
                <Link
                  href={`/${locale}/rotations/${r.id}`}
                  className="ms-rot-head"
                >
                  <span>
                    <strong>{t.rotationTitle}</strong>
                    <span className="ms-sub">
                      {formatRange(locale, r.start_date, r.end_date)}
                    </span>
                  </span>
                  <span className="ms-sub-side">
                    {status(r.status)}
                    <span>{money(r.amount_minor, r.currency)}</span>
                  </span>
                </Link>
                <ol className="ms-rot-weeks">
                  {r.weeks.map((w) => {
                    const label = (
                      <>
                        <span className="ms-week-no">
                          {t.weekShort(w.week)}
                        </span>
                        <span dir="auto">
                          {(locale === "ar" && w.kitchen_name_ar) ||
                            w.kitchen_name}
                        </span>
                        <span className="ms-sub">
                          {formatRange(locale, w.starts_on, w.ends_on)}
                        </span>
                      </>
                    );
                    return (
                      <li key={w.week}>
                        {w.subscription_id ? (
                          <Link
                            href={`/${locale}/subscriptions/${w.subscription_id}`}
                          >
                            {label}
                          </Link>
                        ) : (
                          <span>{label}</span>
                        )}
                      </li>
                    );
                  })}
                </ol>
              </li>
            ))}
          </ul>
        </section>
      )}
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        <p className="ms-empty">{t.failed}</p>
      ) : items.length === 0 && rotations.length === 0 ? (
        <div className="ms-empty">
          <p>{t.empty}</p>
          <div className="ms-actions">
            <Link className="ms-cta" href={`/${locale}`}>
              {t.browse}
            </Link>
            <Link className="ms-cta ms-cta-quiet" href={`/${locale}/rotation`}>
              {t.build}
            </Link>
          </div>
        </div>
      ) : singles.length > 0 ? (
        <section>
          {rotations.length > 0 && <h2>{t.singles}</h2>}
          <ul className="ms-subs">
            {singles.map((s) => (
              <li key={s.id}>
                <Link
                  href={`/${locale}/subscriptions/${s.id}`}
                  className="ms-sub-card"
                >
                  <span>
                    <strong dir="auto">
                      {(locale === "ar" && s.kitchen_name_ar) || s.kitchen_name}
                    </strong>
                    <span className="ms-sub" dir="auto">
                      {(locale === "ar" && s.plan_name_ar) || s.plan_name} ·{" "}
                      {t.cycle(
                        s.cycle_no,
                        s.cycle_ends_on && formatDay(locale, s.cycle_ends_on),
                      )}
                    </span>
                  </span>
                  <span className="ms-sub-side">
                    {status(s.status)}
                    <span>{money(s.amount_minor, s.currency)}</span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </CustomerShell>
  );
}
