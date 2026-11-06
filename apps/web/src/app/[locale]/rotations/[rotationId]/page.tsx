"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";

import { LoadError } from "@/components/load-error";
import { rotationProblem } from "@/lib/rotation-problem";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { use, useState } from "react";

import {
  CustomerShell,
  STATUS,
  asLocale,
  type Locale,
  formatDay,
  formatRange,
} from "@/components/meals/shell";
import { problemMessage } from "@/lib/problem";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";
import {
  cancelRotation,
  getMyRotation,
  listRotationSwapOptions,
  payRotation,
  renewRotation,
  setRotationRenewal,
  swapRotationWeek,
  type Rotation,
  type RotationSwapOption,
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
    cancelling: "Cancelling…",
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
    toggleFailed: "Could not change automatic renewal. Try again.",
    loading: "Loading…",
    notFound: "Rotation not found.",
    failed: "Could not load this rotation.",
    changeKitchen: "Change kitchen",
    changeUntil: (d: string) => `Can be changed until ${d}`,
    kitchenChanged: "Kitchen changed",
    pickTitle: (n: number) => `Change the kitchen for week ${n}`,
    pickNote:
      "Kitchens that deliver to your address on your days and meals, and aren't already in this rotation. A week's kitchen can be changed once.",
    noOptions: "No other kitchen can cook this week right now.",
    samePrice: "Same price",
    payMore: (m: string) => `+${m} to pay now`,
    creditBack: (m: string) => `${m} back to your wallet`,
    currentPrice: (m: string) => `Current week: ${m}`,
    rated: (r: string, n: number) => `${r} ★ · ${n}`,
    unrated: "New kitchen",
    confirmSwap: "Confirm change",
    swapping: "Changing…",
    swapConfirm: (k: string) => `Change this week's kitchen to ${k}? This can't be undone.`,
    swapFailed: "Could not change the kitchen. Try again.",
    swapPaid: "Kitchen changed. Finish the payment to confirm the week.",
    close: "Close",
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
    cancelling: "جارٍ الإلغاء…",
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
    toggleFailed: "تعذر تغيير التجديد التلقائي. حاول مرة أخرى.",
    loading: "جارٍ التحميل…",
    notFound: "التناوب غير موجود.",
    failed: "تعذر تحميل هذا التناوب.",
    changeKitchen: "تغيير المطبخ",
    changeUntil: (d: string) => `يمكن التغيير حتى ${d}`,
    kitchenChanged: "تم تغيير المطبخ",
    pickTitle: (n: number) => `تغيير مطبخ الأسبوع ${n}`,
    pickNote:
      "مطابخ توصل إلى عنوانك في أيامك ووجباتك وليست ضمن هذا التناوب بعد. يمكن تغيير مطبخ الأسبوع مرة واحدة.",
    noOptions: "لا يوجد مطبخ آخر يمكنه الطبخ لهذا الأسبوع حاليًا.",
    samePrice: "نفس السعر",
    payMore: (m: string) => `+${m} تُدفع الآن`,
    creditBack: (m: string) => `${m} تعود إلى محفظتك`,
    currentPrice: (m: string) => `الأسبوع الحالي: ${m}`,
    rated: (r: string, n: number) => `${r} ★ · ${n}`,
    unrated: "مطبخ جديد",
    confirmSwap: "تأكيد التغيير",
    swapping: "جارٍ التغيير…",
    swapConfirm: (k: string) => `تغيير مطبخ هذا الأسبوع إلى ${k}؟ لا يمكن التراجع عن ذلك.`,
    swapFailed: "تعذر تغيير المطبخ. حاول مرة أخرى.",
    swapPaid: "تم تغيير المطبخ. أكمل الدفع لتأكيد الأسبوع.",
    close: "إغلاق",
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
        q.error instanceof ApiError && q.error.status === 404 ? (
          <p className="ms-empty">{t.notFound}</p>
        ) : (
          <LoadError
            error={q.error}
            onRetry={() => void q.refetch()}
            locale={locale}
          />
        )
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
  // The builder lands here with ?pay=failed when the rotation was created but
  // the payment did not go through, so the first view says why.
  const payFailedOnArrival = useSearchParams().get("pay") === "failed";
  const pay = useMutation({
    mutationFn: () => payRotation(r.id),
    onSuccess: refresh,
  });
  const [reason, setReason] = useState("");
  const cancelKey = useIdempotencyKey();
  const cancel = useMutation({
    mutationFn: () =>
      cancelRotation(r.id, reason.trim(), { headers: cancelKey.headers() }),
    onSuccess: async () => {
      cancelKey.reset();
      await refresh();
    },
  });
  const money = (n: number) =>
    formatMoney({ amount: n, currency: r.currency }, locale);
  const [picking, setPicking] = useState<number | null>(null);

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
          {(pay.isError || (payFailedOnArrival && pay.isIdle)) && (
            <p className="ms-bad" role="alert">
              {pay.isError
                ? rotationProblem(pay.error, locale, t.payFailed)
                : t.payFailed}
            </p>
          )}
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
                {w.swappable && (
                  <button
                    type="button"
                    className="ms-cta ms-cta-quiet"
                    aria-expanded={picking === w.week}
                    onClick={() =>
                      setPicking(picking === w.week ? null : w.week)
                    }
                  >
                    {t.changeKitchen}
                  </button>
                )}
                {w.swapped && <span className="ms-sub">{t.kitchenChanged}</span>}
              </span>
            </li>
          ))}
        </ol>
      </section>

      {picking !== null && (
        <SwapPicker
          r={r}
          week={picking}
          locale={locale}
          t={t}
          onClose={() => setPicking(null)}
          onDone={async () => {
            setPicking(null);
            await refresh();
          }}
        />
      )}

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
          {cancel.isError && (
            <p className="ms-bad" role="alert">
              {rotationProblem(cancel.error, locale, t.cancelFailed)}
            </p>
          )}
          <button
            type="button"
            className="ms-cta ms-cta-danger"
            disabled={cancel.isPending}
            onClick={() => {
              if (window.confirm(t.cancelConfirm)) cancel.mutate();
            }}
          >
            {cancel.isPending ? t.cancelling : t.cancel}
          </button>
        </details>
      )}
    </div>
  );
}

/**
 * The eligible kitchens for one future week and what changing costs or
 * credits. The API decides eligibility (address, days, meals, not already in
 * the rotation, the 3-day cutoff) so the page only shows what it is given.
 */
function SwapPicker({
  r,
  week,
  locale,
  t,
  onClose,
  onDone,
}: {
  r: Rotation;
  week: number;
  locale: Locale;
  t: Copy;
  onClose: () => void;
  onDone: () => Promise<void>;
}) {
  const q = useQuery({
    queryKey: ["me", "rotation", r.id, "swap-options", week],
    queryFn: () => listRotationSwapOptions(r.id, week),
  });
  const [planId, setPlanId] = useState<string | null>(null);
  const key = useIdempotencyKey();
  const swap = useMutation({
    mutationFn: (id: string) =>
      swapRotationWeek(r.id, week, id, { headers: key.headers() }),
    onSuccess: async (res) => {
      key.reset();
      const redirect = res.payment?.redirect_url;
      if (redirect) {
        window.location.assign(redirect);
        return;
      }
      await onDone();
    },
  });
  const money = (n: number) =>
    formatMoney({ amount: n, currency: r.currency }, locale);
  const name = (o: RotationSwapOption) =>
    (locale === "ar" && o.kitchen_name_ar) || o.kitchen_name;
  const difference = (n: number) =>
    n === 0
      ? t.samePrice
      : n > 0
        ? t.payMore(money(n))
        : t.creditBack(money(-n));
  const current = r.weeks.find((w) => w.week === week);
  const chosen = q.data?.items.find((o) => o.plan_id === planId) ?? null;

  return (
    <section className="ms-callout" aria-live="polite">
      <h2>{t.pickTitle(week)}</h2>
      <p className="ms-sub">{t.pickNote}</p>
      {current && (
        <p className="ms-sub">
          {t.currentPrice(money(current.price_minor))}
          {current.swap_until &&
            ` · ${t.changeUntil(formatDay(locale, current.swap_until))}`}
        </p>
      )}
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        <LoadError
          error={q.error}
          onRetry={() => void q.refetch()}
          locale={locale}
        />
      ) : q.data.items.length === 0 ? (
        <p className="ms-empty">{t.noOptions}</p>
      ) : (
        <div className="ms-list" role="radiogroup" aria-label={t.pickTitle(week)}>
          {q.data.items.map((o) => (
            <label
              key={o.plan_id}
              className={`ms-option${planId === o.plan_id ? " on" : ""}`}
            >
              <input
                type="radio"
                name="swap-plan"
                checked={planId === o.plan_id}
                onChange={() => setPlanId(o.plan_id)}
                disabled={swap.isPending}
              />
              <span className="ms-week-body">
                <strong dir="auto">{name(o)}</strong>
                <span className="ms-sub">
                  {o.rating !== null
                    ? t.rated(o.rating.toFixed(1), o.rating_count)
                    : t.unrated}
                  {o.cuisines.length > 0 && ` · ${o.cuisines.join(", ")}`}
                </span>
              </span>
              <span className="ms-sub-side">
                <span>{money(o.price_minor)}</span>
                <span className={o.difference_minor > 0 ? "ms-bad" : "ms-ok"}>
                  {difference(o.difference_minor)}
                </span>
              </span>
            </label>
          ))}
        </div>
      )}
      {swap.isError && (
        <p className="ms-bad" role="alert">
          {rotationProblem(swap.error, locale, t.swapFailed)}
        </p>
      )}
      <div className="ms-actions">
        <button
          type="button"
          className="ms-cta"
          disabled={!chosen || swap.isPending}
          onClick={() => {
            if (chosen && window.confirm(t.swapConfirm(name(chosen))))
              swap.mutate(chosen.plan_id);
          }}
        >
          {swap.isPending
            ? t.swapping
            : chosen
              ? `${t.confirmSwap} · ${difference(chosen.difference_minor)}`
              : t.confirmSwap}
        </button>
        <button
          type="button"
          className="ms-cta ms-cta-quiet"
          disabled={swap.isPending}
          onClick={onClose}
        >
          {t.close}
        </button>
      </div>
    </section>
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
  const renewKey = useIdempotencyKey();
  const toggle = useMutation({
    mutationFn: (auto: boolean) => setRotationRenewal(r.id, auto),
    onSuccess: onDone,
    // The checkbox shows the server's value again once this reloads.
    onError: () => onDone(),
  });
  const renew = useMutation({
    mutationFn: async () => {
      const next = await renewRotation(r.id, { headers: renewKey.headers() });
      try {
        await payRotation(next.id);
      } catch {
        // The new rotation shows its own pay button.
      }
      return next;
    },
    onSuccess: async (next) => {
      renewKey.reset();
      await onDone();
      router.push(`/${locale}/rotations/${next.id}`);
    },
  });
  const running = r.status === "active" || r.status === "pending_payment";
  const canRenew =
    !r.renewed_by_id && (r.status === "active" || r.status === "completed");
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
              onChange={(e) => {
                renew.reset();
                toggle.mutate(e.target.checked);
              }}
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
            onClick={() => {
              toggle.reset();
              renew.mutate();
            }}
          >
            {renew.isPending ? t.renewing : t.renew}
          </button>
          <p className="ms-sub">{t.renewNote}</p>
        </div>
      )}
      {toggle.isError && (
        <p className="ms-bad" role="alert">
          {rotationProblem(toggle.error, locale, t.toggleFailed)}
        </p>
      )}
      {renew.isError && (
        <p className="ms-bad" role="alert">
          {rotationProblem(renew.error, locale, t.renewFailed)}
        </p>
      )}
    </section>
  );
}
