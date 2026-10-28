"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useState } from "react";

import {
  CustomerShell,
  STATUS,
  asLocale,
  type Locale,
  formatRange,
} from "@/components/meals/shell";
import {
  cancelRotation,
  getMyRotation,
  payRotation,
  renewRotation,
  setRotationRenewal,
  type Rotation,
} from "@/lib/customer-api";

const COPY = {
  en: {
    back: "My subscriptions",
    title: "4-kitchen rotation",
    period: (a: string, b: string) => `${a} – ${b}`,
    week: (n: number) => `Week ${n}`,
    open: "Meals and dish choices",
    total: "Total for 28 days",
    payNote: "Payment for this rotation hasn't gone through yet.",
    pay: "Pay now",
    paying: "Processing…",
    payFailed: "Payment failed. Try again.",
    cancel: "Cancel rotation",
    cancelNote:
      "Stops every week that hasn't finished. Meals already delivered stay delivered. This cannot be undone.",
    reason: "Reason (optional)",
    cancelConfirm: "Cancel this rotation? All future deliveries stop.",
    cancelFailed: "Could not cancel. Try again.",
    renewal: "Renewal",
    autoRenew: "Renew automatically",
    autoNote:
      "Your next 4 weeks, with the same kitchens, are set up 3 days before this rotation ends for you to pay with one tap. The kitchen that cooks the last week won't cook the first week of the next rotation.",
    renew: "Renew rotation",
    renewing: "Renewing…",
    renewNote:
      "Same 4 kitchens, address, days and meals, at each kitchen's current weekly price.",
    nextReady: "Your next rotation is ready. Pay to keep your meals coming.",
    nextOn: "Your next rotation is booked.",
    viewNext: "View next rotation",
    renewFailed: "Could not renew. Try again.",
    loading: "Loading…",
    notFound: "Rotation not found.",
    failed: "Could not load this rotation.",
  },
  ar: {
    back: "اشتراكاتي",
    title: "تناوب ٤ مطابخ",
    period: (a: string, b: string) => `${a} – ${b}`,
    week: (n: number) => `الأسبوع ${n}`,
    open: "الوجبات واختيار الأطباق",
    total: "المجموع لـ28 يومًا",
    payNote: "لم يتم الدفع لهذا التناوب بعد.",
    pay: "ادفع الآن",
    paying: "جارٍ المعالجة…",
    payFailed: "فشل الدفع. حاول مرة أخرى.",
    cancel: "إلغاء التناوب",
    cancelNote:
      "يوقف كل أسبوع لم ينتهِ بعد. تبقى الوجبات المسلّمة كما هي. لا يمكن التراجع عن ذلك.",
    reason: "السبب (اختياري)",
    cancelConfirm: "إلغاء هذا التناوب؟ ستتوقف كل التوصيلات القادمة.",
    cancelFailed: "تعذر الإلغاء. حاول مرة أخرى.",
    renewal: "التجديد",
    autoRenew: "التجديد التلقائي",
    autoNote:
      "تُجهَّز الأسابيع الأربعة التالية بنفس المطابخ قبل انتهاء هذا التناوب بثلاثة أيام لتدفعها بلمسة واحدة. المطبخ الذي يطبخ الأسبوع الأخير لن يطبخ الأسبوع الأول من التناوب التالي.",
    renew: "جدّد التناوب",
    renewing: "جارٍ التجديد…",
    renewNote:
      "نفس المطابخ الأربعة والعنوان والأيام والوجبات، بالسعر الأسبوعي الحالي لكل مطبخ.",
    nextReady: "تناوبك التالي جاهز. ادفع لتستمر وجباتك.",
    nextOn: "تم حجز تناوبك التالي.",
    viewNext: "عرض التناوب التالي",
    renewFailed: "تعذر التجديد. حاول مرة أخرى.",
    loading: "جارٍ التحميل…",
    notFound: "التناوب غير موجود.",
    failed: "تعذر تحميل هذا التناوب.",
  },
};
type Copy = (typeof COPY)["en"];

export default function RotationDetailPage({
  params,
}: {
  params: Promise<{ locale: string; rotationId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "rotation", p.rotationId],
    queryFn: () => getMyRotation(p.rotationId),
  });
  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/subscriptions`, label: t.back }}
    >
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        <p className="ms-empty">
          {q.error instanceof ApiError && q.error.status === 404
            ? t.notFound
            : t.failed}
        </p>
      ) : (
        <Detail r={q.data} locale={locale} t={t} />
      )}
    </CustomerShell>
  );
}

function Detail({ r, locale, t }: { r: Rotation; locale: Locale; t: Copy }) {
  const qc = useQueryClient();
  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ["me", "rotation", r.id] }),
      qc.invalidateQueries({ queryKey: ["me", "rotations"] }),
      qc.invalidateQueries({ queryKey: ["me", "subscriptions"] }),
    ]);
  const pay = useMutation({
    mutationFn: () => payRotation(r.id),
    onSuccess: refresh,
  });
  const [reason, setReason] = useState("");
  const cancel = useMutation({
    mutationFn: () => cancelRotation(r.id, reason.trim()),
    onSuccess: refresh,
  });
  const money = (n: number) =>
    formatMoney({ amount: n, currency: r.currency }, locale);

  return (
    <div className="ms-detail">
      <header>
        <h1>{t.title}</h1>
        <p className="ms-sub">
          <span className="mono">{r.rotation_no}</span> ·{" "}
          {formatRange(locale, r.start_date, r.end_date)}
        </p>
        <p>
          <span className={`ms-status ms-status-${r.status}`}>
            {STATUS[locale][r.status] ?? r.status}
          </span>{" "}
          <span className="ms-sub">
            {t.total} · {money(r.amount_minor)}
          </span>
        </p>
      </header>

      {r.status === "pending_payment" && (
        <section className="ms-callout">
          <p>{t.payNote}</p>
          {pay.isError && <p className="ms-bad">{t.payFailed}</p>}
          <button
            type="button"
            className="ms-cta"
            disabled={pay.isPending}
            onClick={() => pay.mutate()}
          >
            {pay.isPending ? t.paying : `${t.pay} · ${money(r.amount_minor)}`}
          </button>
        </section>
      )}

      <section>
        <ol className="ms-weeks ms-timeline">
          {r.weeks.map((w) => (
            <li key={w.week}>
              <span className="ms-week-no">{t.week(w.week)}</span>
              <span className="ms-week-body">
                <strong dir="auto">
                  {(locale === "ar" && w.kitchen_name_ar) || w.kitchen_name}
                </strong>
                <span className="ms-sub">
                  {formatRange(locale, w.starts_on, w.ends_on)}
                </span>
                {w.subscription_id && (
                  <Link href={`/${locale}/subscriptions/${w.subscription_id}`}>
                    {t.open}
                  </Link>
                )}
              </span>
              <span className="ms-sub-side">
                {w.status && (
                  <span className={`ms-status ms-status-${w.status}`}>
                    {STATUS[locale][w.status] ?? w.status}
                  </span>
                )}
                <span>{money(w.price_minor)}</span>
              </span>
            </li>
          ))}
        </ol>
      </section>

      <Renewal r={r} locale={locale} t={t} onDone={refresh} />

      {(r.status === "active" || r.status === "pending_payment") && (
        <details className="ms-danger">
          <summary>{t.cancel}</summary>
          <p className="ms-sub">{t.cancelNote}</p>
          <label className="ms-field">
            <span>{t.reason}</span>
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              maxLength={200}
              dir="auto"
            />
          </label>
          {cancel.isError && <p className="ms-bad">{t.cancelFailed}</p>}
          <button
            type="button"
            className="ms-cta ms-cta-danger"
            disabled={cancel.isPending}
            onClick={() => {
              if (window.confirm(t.cancelConfirm)) cancel.mutate();
            }}
          >
            {t.cancel}
          </button>
        </details>
      )}
    </div>
  );
}

function Renewal({
  r,
  locale,
  t,
  onDone,
}: {
  r: Rotation;
  locale: Locale;
  t: Copy;
  onDone: () => Promise<unknown>;
}) {
  const router = useRouter();
  const toggle = useMutation({
    mutationFn: (auto: boolean) => setRotationRenewal(r.id, auto),
    onSuccess: onDone,
  });
  const renew = useMutation({
    mutationFn: async () => {
      const next = await renewRotation(r.id);
      try {
        await payRotation(next.id);
      } catch {
        // The new rotation shows its own pay button.
      }
      return next;
    },
    onSuccess: async (next) => {
      await onDone();
      router.push(`/${locale}/rotations/${next.id}`);
    },
  });
  const running = r.status === "active" || r.status === "pending_payment";
  const canRenew = !r.renewed_by_id && r.status !== "pending_payment";
  if (!running && !canRenew && !r.renewed_by_id) return null;

  return (
    <section className="ms-renewal">
      <h2>{t.renewal}</h2>
      {r.renewed_by_id ? (
        <p>
          {r.renewed_by_status === "pending_payment" ? t.nextReady : t.nextOn}{" "}
          <Link href={`/${locale}/rotations/${r.renewed_by_id}`}>
            → {t.viewNext}
          </Link>
        </p>
      ) : null}
      {running && (
        <>
          <label className="ms-toggle">
            <input
              type="checkbox"
              checked={r.auto_renew}
              disabled={toggle.isPending}
              onChange={(e) => toggle.mutate(e.target.checked)}
            />
            <span>{t.autoRenew}</span>
          </label>
          <p className="ms-sub">{t.autoNote}</p>
        </>
      )}
      {canRenew && (
        <div className="ms-renew">
          <button
            type="button"
            className="ms-cta"
            disabled={renew.isPending}
            onClick={() => renew.mutate()}
          >
            {renew.isPending ? t.renewing : t.renew}
          </button>
          <p className="ms-sub">{t.renewNote}</p>
        </div>
      )}
      {(toggle.isError || renew.isError) && (
        <p className="ms-bad" role="alert">
          {renew.error instanceof ApiError
            ? (renew.error.problem?.errors?.[0]?.message ??
              renew.error.problem?.detail ??
              t.renewFailed)
            : t.renewFailed}
        </p>
      )}
    </section>
  );
}
