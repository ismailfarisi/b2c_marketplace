"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useState, type FormEvent } from "react";

import { RateKitchen } from "@/components/meals/rate-kitchen";
import {
  CustomerShell,
  STATUS,
  asLocale,
  formatDay,
  formatRange,
  type Locale,
} from "@/components/meals/shell";
import {
  cancelSubscription,
  chooseMeal,
  getMySubscription,
  pauseSubscription,
  payOrder,
  renewSubscription,
  setRenewal,
  skipMeal,
  type MySubscriptionDetail,
  type UpcomingMeal,
} from "@/lib/customer-api";

const COPY = {
  en: {
    back: "My subscriptions",
    pay: "Pay now",
    paying: "Processing…",
    payNote: "Deliveries are scheduled once payment goes through.",
    payFailed: "The payment did not go through. Try again.",
    cycle: (n: number, s: string, e: string) => `Cycle ${n}: ${s} – ${e}`,
    upcoming: "Next two weeks",
    noUpcoming: "No deliveries in the next two weeks.",
    choose: "Choose your dish",
    locked: "Locked: the cutoff has passed",
    until: (d: string) => `Change until ${d}`,
    yourPick: "Your pick",
    skip: "Skip this meal",
    skipConfirm: "Skip this meal? It is credited to the end of your cycle.",
    pause: "Pause deliveries",
    pauseNote: (left: number) =>
      `You can pause up to ${left} more day${left === 1 ? "" : "s"} this cycle. Paused days move to the end of the cycle.`,
    from: "From",
    to: "To",
    pauseBtn: "Pause",
    paused: "Paused",
    cancel: "Cancel subscription",
    cancelNote: "Stops all future deliveries. This cannot be undone.",
    reason: "Reason (optional)",
    cancelBtn: "Cancel subscription",
    cancelConfirm: "Cancel this subscription? All future deliveries stop.",
    rotation: (n: number) =>
      `Week ${n} of a 4-kitchen rotation. Payment, pausing and cancelling are managed on the rotation.`,
    manageRotation: "Manage rotation",
    renewalDue: (d: string) =>
      `Your next cycle starts ${d}. Pay now to keep your meals coming.`,
    renewal: "Renewal",
    autoRenew: "Renew automatically",
    autoOn: (d: string) =>
      `Your next cycle is billed 3 days before ${d} at the plan's current price. Pay it with one tap and your meals continue.`,
    autoOff: (d: string) =>
      `This subscription ends after ${d}. Turn on automatic renewal or renew below to keep your meals coming.`,
    companyOn: (c: string, d: string) =>
      `${c} pays for your next cycle, which starts after ${d}. Your meals continue automatically.`,
    companyOff: (c: string, d: string) =>
      `This office lunch paid by ${c} ends after ${d}. Turn on automatic renewal to keep it going.`,
    renew: "Renew with this kitchen",
    continueTrial: "Continue with this kitchen",
    renewNote:
      "Same plan, address and delivery days, at the plan's current price.",
    renewing: "Renewing…",
    renewed: "You've renewed this subscription.",
    viewRenewal: "View the new subscription",
    loading: "Loading…",
    notFound: "Subscription not found.",
    failed: "Something went wrong. Try again.",
    kcal: "kcal",
    weekdays: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
    slot: {
      breakfast: "Breakfast",
      lunch: "Lunch",
      dinner: "Dinner",
      snack: "Snack",
      suhoor: "Suhoor",
      iftar: "Iftar",
    } as Record<string, string>,
  },
  ar: {
    back: "اشتراكاتي",
    pay: "ادفع الآن",
    paying: "جارٍ المعالجة…",
    payNote: "تُجدول التوصيلات بعد إتمام الدفع.",
    payFailed: "لم يتم الدفع. حاول مرة أخرى.",
    cycle: (n: number, s: string, e: string) => `الدورة ${n}: ${s} – ${e}`,
    upcoming: "الأسبوعان القادمان",
    noUpcoming: "لا توجد توصيلات خلال الأسبوعين القادمين.",
    choose: "اختر طبقك",
    locked: "مغلق: انتهى الموعد النهائي",
    until: (d: string) => `يمكن التغيير حتى ${d}`,
    yourPick: "اختيارك",
    skip: "تخطي هذه الوجبة",
    skipConfirm: "تخطي هذه الوجبة؟ تُضاف إلى نهاية دورتك.",
    pause: "إيقاف التوصيل مؤقتًا",
    pauseNote: (left: number) =>
      `يمكنك الإيقاف ${left} يومًا إضافيًا في هذه الدورة. تنتقل الأيام الموقوفة إلى نهاية الدورة.`,
    from: "من",
    to: "إلى",
    pauseBtn: "إيقاف مؤقت",
    paused: "موقوف",
    cancel: "إلغاء الاشتراك",
    cancelNote: "يوقف كل التوصيلات القادمة. لا يمكن التراجع عن ذلك.",
    reason: "السبب (اختياري)",
    cancelBtn: "إلغاء الاشتراك",
    cancelConfirm: "إلغاء هذا الاشتراك؟ ستتوقف كل التوصيلات القادمة.",
    rotation: (n: number) =>
      `الأسبوع ${n} من تناوب ٤ مطابخ. يتم الدفع والإلغاء من صفحة التناوب.`,
    manageRotation: "إدارة التناوب",
    renewalDue: (d: string) =>
      `تبدأ دورتك التالية في ${d}. ادفع الآن لتستمر وجباتك.`,
    renewal: "التجديد",
    autoRenew: "التجديد التلقائي",
    autoOn: (d: string) =>
      `تُصدر فاتورة دورتك التالية قبل ${d} بثلاثة أيام بالسعر الحالي للخطة. ادفعها بلمسة واحدة وتستمر وجباتك.`,
    autoOff: (d: string) =>
      `ينتهي هذا الاشتراك بعد ${d}. فعّل التجديد التلقائي أو جدّد أدناه لتستمر وجباتك.`,
    companyOn: (c: string, d: string) =>
      `تدفع ${c} دورتك التالية التي تبدأ بعد ${d}. تستمر وجباتك تلقائيًا.`,
    companyOff: (c: string, d: string) =>
      `ينتهي غداء المكتب المدفوع من ${c} بعد ${d}. فعّل التجديد التلقائي ليستمر.`,
    renew: "جدّد مع هذا المطبخ",
    continueTrial: "استمر مع هذا المطبخ",
    renewNote: "نفس الخطة والعنوان وأيام التوصيل، بالسعر الحالي للخطة.",
    renewing: "جارٍ التجديد…",
    renewed: "لقد جدّدت هذا الاشتراك.",
    viewRenewal: "عرض الاشتراك الجديد",
    loading: "جارٍ التحميل…",
    notFound: "الاشتراك غير موجود.",
    failed: "حدث خطأ. حاول مرة أخرى.",
    kcal: "سعرة",
    weekdays: [
      "الأحد",
      "الاثنين",
      "الثلاثاء",
      "الأربعاء",
      "الخميس",
      "الجمعة",
      "السبت",
    ],
    slot: {
      breakfast: "فطور",
      lunch: "غداء",
      dinner: "عشاء",
      snack: "وجبة خفيفة",
      suhoor: "سحور",
      iftar: "إفطار",
    } as Record<string, string>,
  },
};
type Copy = (typeof COPY)["en"];

const problemText = (err: unknown, fallback: string) =>
  err instanceof ApiError
    ? (err.problem?.errors?.[0]?.message ?? err.problem?.detail ?? fallback)
    : fallback;

export default function SubscriptionPage({
  params,
}: {
  params: Promise<{ locale: string; subscriptionId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "subscription", p.subscriptionId],
    queryFn: () => getMySubscription(p.subscriptionId),
  });

  let body;
  if (q.isPending) body = <p className="ms-note">{t.loading}</p>;
  else if (q.isError)
    body = (
      <p className="ms-empty">
        {q.error instanceof ApiError && q.error.status === 404
          ? t.notFound
          : t.failed}
      </p>
    );
  else body = <Detail s={q.data} locale={locale} t={t} />;

  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/subscriptions`, label: t.back }}
    >
      {body}
    </CustomerShell>
  );
}

function Detail({
  s,
  locale,
  t,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
  t: Copy;
}) {
  const qc = useQueryClient();
  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ["me", "subscription", s.id] }),
      qc.invalidateQueries({ queryKey: ["me", "subscriptions"] }),
    ]);
  const pay = useMutation({
    mutationFn: () => payOrder(s.pending_order_id!),
    onSuccess: refresh,
  });
  const money = (n: number) =>
    formatMoney({ amount: n, currency: s.currency }, locale);
  const live =
    s.status === "active" ||
    s.status === "pending_payment" ||
    s.status === "paused";
  const cycle = s.cycles[0];

  return (
    <div className="ms-detail">
      <header>
        <h1 dir="auto">
          {(locale === "ar" && s.kitchen_name_ar) || s.kitchen_name}
        </h1>
        <p className="ms-sub" dir="auto">
          {(locale === "ar" && s.plan_name_ar) || s.plan_name} ·{" "}
          <span className="mono">{s.subscription_no}</span>
        </p>
        <p>
          <span className={`ms-status ms-status-${s.status}`}>
            {STATUS[locale][s.status] ?? s.status}
          </span>{" "}
          {cycle && (
            <span className="ms-sub">
              {t.cycle(
                cycle.cycle_no,
                formatDay(locale, cycle.starts_on),
                formatDay(locale, cycle.ends_on),
              )}{" "}
              · {money(cycle.amount_minor)}
            </span>
          )}
        </p>
        <p>
          <Link href={`/${locale}/kitchens/${s.branch_id}`}>
            → {(locale === "ar" && s.kitchen_name_ar) || s.kitchen_name}
          </Link>
        </p>
      </header>

      {s.rotation_id && s.rotation_week != null && (
        <section className="ms-callout">
          <p>{t.rotation(s.rotation_week)}</p>
          <Link href={`/${locale}/rotations/${s.rotation_id}`}>
            → {t.manageRotation}
          </Link>
        </section>
      )}

      {s.pending_order_id && !s.rotation_id && (
        <section className="ms-callout">
          <p>
            {s.cycle_no > 1 && cycle
              ? t.renewalDue(formatDay(locale, cycle.starts_on))
              : t.payNote}
          </p>
          {pay.isError && <p className="ms-bad">{t.payFailed}</p>}
          <button
            type="button"
            className="ms-cta"
            disabled={pay.isPending}
            onClick={() => pay.mutate()}
          >
            {pay.isPending ? t.paying : `${t.pay} · ${money(s.amount_minor)}`}
          </button>
        </section>
      )}

      <section>
        <h2>{t.upcoming}</h2>
        {s.upcoming.length === 0 ? (
          <p className="ms-note">{t.noUpcoming}</p>
        ) : (
          <ul className="ms-meals">
            {s.upcoming.map((m) => (
              <Meal
                key={`${m.date}-${m.meal_slot}`}
                m={m}
                sub={s}
                locale={locale}
                t={t}
                onChange={refresh}
                editable={live}
              />
            ))}
          </ul>
        )}
      </section>

      {s.pauses.length > 0 && (
        <section>
          <h2>{t.paused}</h2>
          <ul className="ms-pauses">
            {s.pauses.map((p) => (
              <li key={p.from_date}>
                {formatRange(locale, p.from_date, p.to_date)}
              </li>
            ))}
          </ul>
        </section>
      )}

      {!s.rotation_id && (
        <Renewal s={s} locale={locale} t={t} onDone={refresh} />
      )}

      {s.status !== "pending_payment" && (
        <RateKitchen subscriptionId={s.id} locale={locale} />
      )}

      {s.status === "active" && !s.rotation_id && (
        <Pause s={s} t={t} onDone={refresh} />
      )}
      {live && !s.rotation_id && <Cancel s={s} t={t} onDone={refresh} />}
    </div>
  );
}

function Renewal({
  s,
  locale,
  t,
  onDone,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
  t: Copy;
  onDone: () => Promise<unknown>;
}) {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const toggle = useMutation({
    mutationFn: (auto: boolean) => setRenewal(s.id, auto),
    onSuccess: onDone,
    onError: (err) => setError(problemText(err, t.failed)),
  });
  const renew = useMutation({
    mutationFn: async () => {
      const next = await renewSubscription(s.id);
      try {
        await payOrder(next.order_id);
      } catch {
        // The new subscription shows its own pay button.
      }
      return next;
    },
    onSuccess: async (next) => {
      await onDone();
      router.push(`/${locale}/subscriptions/${next.id}`);
    },
    onError: (err) => setError(problemText(err, t.failed)),
  });
  const running = s.status === "active" || s.status === "paused";
  const paidEnd = s.cycles.find((c) => c.status === "paid")?.ends_on;
  if (!s.renewed_by_id && !running && !s.can_renew) return null;

  return (
    <section className="ms-renewal">
      <h2>{t.renewal}</h2>
      {s.renewed_by_id ? (
        <p>
          {t.renewed}{" "}
          <Link href={`/${locale}/subscriptions/${s.renewed_by_id}`}>
            → {t.viewRenewal}
          </Link>
        </p>
      ) : (
        <>
          {running && (
            <>
              <label className="ms-toggle">
                <input
                  type="checkbox"
                  checked={s.renewal_mode === "auto"}
                  disabled={toggle.isPending}
                  onChange={(e) => {
                    setError(null);
                    toggle.mutate(e.target.checked);
                  }}
                />
                <span>{t.autoRenew}</span>
              </label>
              {paidEnd && (
                <p className="ms-sub">
                  {s.company_name
                    ? s.renewal_mode === "auto"
                      ? t.companyOn(s.company_name, formatDay(locale, paidEnd))
                      : t.companyOff(s.company_name, formatDay(locale, paidEnd))
                    : s.renewal_mode === "auto"
                      ? t.autoOn(formatDay(locale, paidEnd))
                      : t.autoOff(formatDay(locale, paidEnd))}
                </p>
              )}
            </>
          )}
          {s.can_renew && (
            <div className="ms-renew">
              <button
                type="button"
                className="ms-cta"
                disabled={renew.isPending}
                onClick={() => {
                  setError(null);
                  renew.mutate();
                }}
              >
                {renew.isPending
                  ? t.renewing
                  : s.renewal_mode === "none" && running
                    ? t.continueTrial
                    : t.renew}
              </button>
              <p className="ms-sub">{t.renewNote}</p>
            </div>
          )}
        </>
      )}
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

function Meal({
  m,
  sub,
  locale,
  t,
  onChange,
  editable,
}: {
  m: UpcomingMeal;
  sub: MySubscriptionDetail;
  locale: Locale;
  t: Copy;
  onChange: () => Promise<unknown>;
  editable: boolean;
}) {
  const [error, setError] = useState<string | null>(null);
  const choose = useMutation({
    mutationFn: (dishId: string) =>
      chooseMeal(sub.id, m.date, m.meal_slot, dishId),
    onSuccess: onChange,
    onError: (err) => setError(problemText(err, t.failed)),
  });
  const skip = useMutation({
    mutationFn: () => skipMeal(sub.id, m.date, m.meal_slot),
    onSuccess: onChange,
    onError: (err) => setError(problemText(err, t.failed)),
  });
  const chosen =
    m.chosen_dish_id ?? m.options.find((o) => o.is_default)?.id ?? null;
  const cutoff = new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-AE", {
    weekday: "short",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(m.cutoff_at));
  const canEdit = editable && !m.locked;
  const day = new Date(`${m.date}T00:00:00`);

  return (
    <li className={`ms-meal${m.locked ? " locked" : ""}`}>
      <div className="ms-meal-when">
        <strong>{t.weekdays[m.weekday]}</strong>
        <span>
          {day.getDate()}/{day.getMonth() + 1}
        </span>
        <small className="ms-sub">
          {t.slot[m.meal_slot] ?? m.meal_slot}
          {m.window_label && ` · ${m.window_label}`}
        </small>
      </div>
      <div className="ms-meal-what">
        {m.options.length > 1 && canEdit ? (
          <fieldset>
            <legend className="ms-sub">{t.choose}</legend>
            {m.options.map((o) => (
              <label
                key={o.id}
                className={`ms-option${o.id === chosen ? " on" : ""}`}
              >
                <input
                  type="radio"
                  name={`${m.date}-${m.meal_slot}`}
                  checked={o.id === chosen}
                  disabled={choose.isPending}
                  onChange={() => {
                    setError(null);
                    choose.mutate(o.id);
                  }}
                />
                <span dir="auto">
                  {(locale === "ar" && o.name_ar) || o.name}
                </span>
                {(o.calories != null || o.allergens.length > 0) && (
                  <small className="ms-sub">
                    {o.calories != null && `${o.calories} ${t.kcal}`}
                    {o.calories != null && o.allergens.length > 0 && " · "}
                    {o.allergens.join(", ")}
                  </small>
                )}
              </label>
            ))}
          </fieldset>
        ) : (
          <span dir="auto">
            {(() => {
              const d = m.options.find((o) => o.id === chosen) ?? m.options[0];
              return d ? (locale === "ar" && d.name_ar) || d.name : "—";
            })()}
            {m.chosen_by === "customer" && (
              <small className="ms-sub"> · {t.yourPick}</small>
            )}
          </span>
        )}
        <small className="ms-sub">
          {m.locked ? t.locked : t.until(cutoff)}
        </small>
        {error && (
          <small className="ms-bad" role="alert">
            {error}
          </small>
        )}
      </div>
      {canEdit && (
        <button
          type="button"
          className="ms-link"
          disabled={skip.isPending}
          onClick={() => {
            if (window.confirm(t.skipConfirm)) {
              setError(null);
              skip.mutate();
            }
          }}
        >
          {t.skip}
        </button>
      )}
    </li>
  );
}

function Pause({
  s,
  t,
  onDone,
}: {
  s: MySubscriptionDetail;
  t: Copy;
  onDone: () => Promise<unknown>;
}) {
  const left = Math.max(0, s.pause_allowance_days - s.pause_days_used);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [error, setError] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => pauseSubscription(s.id, from, to),
    onSuccess: async () => {
      setFrom("");
      setTo("");
      await onDone();
    },
    onError: (err) => setError(problemText(err, t.failed)),
  });
  if (s.pause_allowance_days === 0) return null;

  function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    m.mutate();
  }

  return (
    <section>
      <h2>{t.pause}</h2>
      <p className="ms-sub">{t.pauseNote(left)}</p>
      <form className="ms-row ms-inline" onSubmit={submit}>
        <label className="ms-field">
          <span>{t.from}</span>
          <input
            type="date"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
            required
          />
        </label>
        <label className="ms-field">
          <span>{t.to}</span>
          <input
            type="date"
            value={to}
            min={from}
            onChange={(e) => setTo(e.target.value)}
            required
          />
        </label>
        <button
          type="submit"
          className="ms-cta ms-cta-quiet"
          disabled={m.isPending || left === 0 || !from || !to}
        >
          {t.pauseBtn}
        </button>
      </form>
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

function Cancel({
  s,
  t,
  onDone,
}: {
  s: MySubscriptionDetail;
  t: Copy;
  onDone: () => Promise<unknown>;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => cancelSubscription(s.id, reason.trim()),
    onSuccess: onDone,
    onError: (err) => setError(problemText(err, t.failed)),
  });
  return (
    <details className="ms-danger">
      <summary>{t.cancel}</summary>
      <p className="ms-sub">{t.cancelNote}</p>
      <label className="ms-field">
        <span>{t.reason}</span>
        <textarea
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          rows={2}
          maxLength={500}
          dir="auto"
        />
      </label>
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
      <button
        type="button"
        className="ms-cta ms-cta-danger"
        disabled={m.isPending}
        onClick={() => {
          if (window.confirm(t.cancelConfirm)) {
            setError(null);
            m.mutate();
          }
        }}
      >
        {t.cancelBtn}
      </button>
    </details>
  );
}
