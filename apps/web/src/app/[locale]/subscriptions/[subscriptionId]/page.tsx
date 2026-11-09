"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useState, type FormEvent } from "react";

import { LoadError } from "@/components/load-error";
import { CancelFlow } from "@/components/meals/cancel-reasons";
import {
  DeliveryStatus,
  RecentDeliveries,
} from "@/components/meals/delivery-status";
import {
  GUEST_WEEK_ANCHOR,
  GuestWeekCard,
} from "@/components/meals/guest-week";
import { RateKitchen } from "@/components/meals/rate-kitchen";
import { DeliveryAddresses } from "@/components/meals/meal-addresses";
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
  clearMyTaste,
  listMyTastes,
  setMyTaste,
  getCheaperPlans,
  changeSubscriptionAddresses,
  getMySubscription,
  pauseSubscription,
  payOrder,
  renewSubscription,
  resumeSubscription,
  setRenewal,
  skipMeal,
  unskipMeal,
  type CancellationReasonCode,
  type MySubscriptionDetail,
  type UpcomingMeal,
} from "@/lib/customer-api";
import { useBusyGuard } from "@/lib/use-busy-guard";
import { problemMessage } from "@/lib/problem";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

const COPY = {
  en: {
    back: "My subscriptions",
    pay: "Pay now",
    walletApplied: (amount: string) => `${amount} paid from your wallet credit`,
    paying: "Processing…",
    payNote: "Deliveries are scheduled once payment goes through.",
    payFailed: "The payment did not go through. Try again.",
    paidBanner: (d: string | null) =>
      d
        ? `Payment received. Your first delivery is on ${d}.`
        : "Payment received. Your meals are scheduled.",
    payPending:
      "Your payment is being confirmed. This page updates once it goes through.",
    cycle: (n: number, s: string, e: string) => `Cycle ${n}: ${s} – ${e}`,
    upcoming: "Next two weeks",
    noUpcoming: "No deliveries in the next two weeks.",
    choose: "Choose your dish",
    locked: "Locked: the cutoff has passed",
    until: (d: string) => `Change until ${d}`,
    skipUntil: (d: string) => `Skip until ${d}`,
    yourPick: "Your pick",
    notAgain: "Not again",
    avoided: "Not again — we'll pick the other dish when there is one",
    loveIt: "Loved it",
    loved: "Loved it — remembered when your rotation renews",
    undoAvoid: "Undo",
    tasteFailed: "Could not save. Try again.",
    skip: "Skip this meal",
    skipConfirm:
      "Skip this meal? Skipped meals are not credited. To move days to the end of your cycle, pause instead.",
    skipped: "Skipped",
    skippedNote:
      "Meal skipped. Skips are not credited; pause instead to move days to the end of your cycle.",
    unskip: "Put it back",
    unskipped: "Meal back on your schedule.",
    pause: "Pause deliveries",
    pauseNote: (left: number) =>
      `You can pause up to ${left} more day${left === 1 ? "" : "s"} this cycle. Paused days move to the end of the cycle.`,
    resume: "Resume deliveries",
    resuming: "Resuming…",
    resumeNote:
      "Deliveries restart from the first day still before its cutoff.",
    resumed: (d: string) => `Deliveries resume ${d}.`,
    cancelling: "Cancelling…",
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
    companyUnpaid: (c: string) =>
      `${c} couldn't cover this renewal (the company's lunch credit or policy doesn't allow it). You don't need to pay: ask your company's lunch admin.`,
    companyFirstUnpaid: (c: string) =>
      `${c} hasn't covered this office lunch yet. Ask your company's lunch admin.`,
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
    walletApplied: (amount: string) => `${amount} مدفوعة من رصيد محفظتك`,
    paying: "جارٍ المعالجة…",
    payNote: "تُجدول التوصيلات بعد إتمام الدفع.",
    payFailed: "لم يتم الدفع. حاول مرة أخرى.",
    paidBanner: (d: string | null) =>
      d
        ? `تم استلام الدفع. أول توصيل لك يوم ${d}.`
        : "تم استلام الدفع. تمت جدولة وجباتك.",
    payPending: "جارٍ تأكيد الدفع. ستتحدث هذه الصفحة عند إتمامه.",
    cycle: (n: number, s: string, e: string) => `الدورة ${n}: ${s} – ${e}`,
    upcoming: "الأسبوعان القادمان",
    noUpcoming: "لا توجد توصيلات خلال الأسبوعين القادمين.",
    choose: "اختر طبقك",
    locked: "مغلق: انتهى الموعد النهائي",
    until: (d: string) => `يمكن التغيير حتى ${d}`,
    skipUntil: (d: string) => `يمكن التخطي حتى ${d}`,
    yourPick: "اختيارك",
    notAgain: "ليس مرة أخرى",
    avoided: "ليس مرة أخرى — سنختار الطبق الآخر عندما يتوفر",
    loveIt: "أحببته",
    loved: "أحببته — سنتذكره عند تجديد تناوبك",
    undoAvoid: "تراجع",
    tasteFailed: "تعذر الحفظ. حاول مرة أخرى.",
    skip: "تخطي هذه الوجبة",
    skipConfirm:
      "تخطي هذه الوجبة؟ لا يُعوَّض عن الوجبات المتخطاة. لنقل الأيام إلى نهاية دورتك، استخدم الإيقاف المؤقت.",
    skipped: "متخطاة",
    skippedNote:
      "تم تخطي الوجبة. لا يُعوَّض عن التخطي؛ استخدم الإيقاف المؤقت لنقل الأيام إلى نهاية دورتك.",
    unskip: "أعدها",
    unskipped: "عادت الوجبة إلى جدولك.",
    pause: "إيقاف التوصيل مؤقتًا",
    pauseNote: (left: number) =>
      `${arPauseLeft(left)} تنتقل الأيام الموقوفة إلى نهاية الدورة.`,
    resume: "استئناف التوصيل",
    resuming: "جارٍ الاستئناف…",
    resumeNote: "يُستأنف التوصيل من أول يوم لم يفت موعد تغييره.",
    resumed: (d: string) => `يُستأنف التوصيل في ${d}.`,
    cancelling: "جارٍ الإلغاء…",
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
    companyUnpaid: (c: string) =>
      `لم تتمكن ${c} من تغطية هذا التجديد (رصيد الغداء أو سياسة الشركة لا يسمح بذلك). لا داعي للدفع: تواصل مع مسؤول الغداء في شركتك.`,
    companyFirstUnpaid: (c: string) =>
      `لم تغطِّ ${c} غداء المكتب هذا بعد. تواصل مع مسؤول الغداء في شركتك.`,
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

/** Arabic counted nouns change form with the number (يومان، ٣ أيام، ١١ يومًا). */
function arPauseLeft(n: number): string {
  switch (new Intl.PluralRules("ar").select(n)) {
    case "zero":
      return "لم يتبقَّ لك أي يوم إيقاف في هذه الدورة.";
    case "one":
      return "يمكنك الإيقاف يومًا واحدًا إضافيًا في هذه الدورة.";
    case "two":
      return "يمكنك الإيقاف يومين إضافيين في هذه الدورة.";
    case "few":
      return `يمكنك الإيقاف ${n} أيام إضافية في هذه الدورة.`;
    case "many":
      return `يمكنك الإيقاف ${n} يومًا إضافيًا في هذه الدورة.`;
    default:
      return `يمكنك الإيقاف ${n} يوم إضافي في هذه الدورة.`;
  }
}

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
    body =
      q.error instanceof ApiError && q.error.status === 404 ? (
        <p className="ms-empty">{t.notFound}</p>
      ) : (
        <LoadError
          error={q.error}
          onRetry={() => void q.refetch()}
          locale={locale}
        />
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
  const [notice, setNotice] = useState<string | null>(null);
  const pay = useMutation({
    mutationFn: () => payOrder(s.pending_order_id!),
    onSuccess: refresh,
  });
  // Checkout lands here with ?paid=1 or ?payment=failed|pending.
  const [outcome, setOutcome] = useState<"paid" | "failed" | "pending" | null>(
    null,
  );
  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    if (q.get("paid") === "1") setOutcome("paid");
    else if (q.get("payment") === "failed") setOutcome("failed");
    else if (q.get("payment") === "pending") setOutcome("pending");
  }, []);
  const firstDelivery = s.upcoming[0]?.date ?? null;
  const meals = [...s.upcoming, ...(s.skipped ?? [])].sort(
    (a, b) =>
      a.date.localeCompare(b.date) || a.cutoff_at.localeCompare(b.cutoff_at),
  );
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

      {outcome === "paid" && !s.pending_order_id && (
        <p className="ms-callout ms-ok" role="status">
          {t.paidBanner(
            firstDelivery ? formatDay(locale, firstDelivery) : null,
          )}
        </p>
      )}

      {s.rotation_id && s.rotation_week != null && (
        <section className="ms-callout">
          <p>{t.rotation(s.rotation_week)}</p>
          <Link href={`/${locale}/rotations/${s.rotation_id}`}>
            → {t.manageRotation}
          </Link>
        </section>
      )}

      {s.pending_order_id && s.company_name && (
        <section className="ms-callout" role="status">
          <p>
            {s.cycle_no > 1
              ? t.companyUnpaid(s.company_name)
              : t.companyFirstUnpaid(s.company_name)}
          </p>
        </section>
      )}

      {s.pending_order_id && !s.rotation_id && !s.company_name && (
        <section className="ms-callout">
          <p>
            {s.cycle_no > 1 && cycle
              ? t.renewalDue(formatDay(locale, cycle.starts_on))
              : t.payNote}
          </p>
          {pay.isError && (
            <p className="ms-bad" role="alert">
              {pay.error instanceof ApiError
                ? problemMessage(pay.error, locale)
                : t.payFailed}
            </p>
          )}
          {outcome === "failed" && !pay.isError && !pay.isSuccess && (
            <p className="ms-bad" role="alert">
              {t.payFailed}
            </p>
          )}
          {outcome === "pending" && !pay.isError && !pay.isSuccess && (
            <p className="ms-note" role="status">
              {t.payPending}
            </p>
          )}
          {s.wallet_applied_minor > 0 && (
            <p className="ms-note">
              {t.walletApplied(money(s.wallet_applied_minor))}
            </p>
          )}
          <button
            type="button"
            className="ms-cta"
            disabled={pay.isPending}
            onClick={() => pay.mutate()}
          >
            {pay.isPending ? t.paying : `${t.pay} · ${money(s.due_minor)}`}
          </button>
        </section>
      )}

      <DeliveryAddresses
        locale={locale}
        items={s.delivery_addresses ?? []}
        // A rotation's week moves with the rotation (its page changes all
        // weeks at once); a company lunch's office is the company's to set.
        canChange={(s.can_change_address ?? false) && !s.rotation_id}
        isCompany={!!s.company_name}
        companyName={s.company_name}
        kitchens={[
          {
            branch_id: s.branch_id,
            name: (locale === "ar" && s.kitchen_name_ar) || s.kitchen_name,
          },
        ]}
        kitchenName={(name) =>
          name === s.kitchen_name
            ? (locale === "ar" && s.kitchen_name_ar) || s.kitchen_name
            : null
        }
        save={(addresses, headers) =>
          changeSubscriptionAddresses(s.id, addresses, { headers })
        }
        onSaved={(out) => {
          const { changed: _changed, ...detail } = out;
          qc.setQueryData(["me", "subscription", s.id], detail);
          return refresh();
        }}
      />

      <section>
        <h2>{t.upcoming}</h2>
        {notice && (
          <p className="ms-ok" role="status">
            {notice}
          </p>
        )}
        {meals.length === 0 ? (
          <p className="ms-note">{t.noUpcoming}</p>
        ) : (
          <ul className="ms-meals">
            {meals.map((m) => (
              <Meal
                key={`${m.date}-${m.meal_slot}`}
                m={m}
                sub={s}
                locale={locale}
                t={t}
                onChange={refresh}
                onNotice={setNotice}
                editable={live}
              />
            ))}
          </ul>
        )}
      </section>

      <RecentDeliveries
        items={s.recent ?? []}
        locale={locale}
        slotLabel={(slot) => t.slot[slot] ?? slot}
      />

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
          {!s.rotation_id && s.pauses.some((p) => p.can_resume !== false) && (
            <Resume s={s} locale={locale} t={t} onDone={refresh} />
          )}
        </section>
      )}

      {!s.rotation_id && (
        <GuestWeekCard s={s} locale={locale} onDone={refresh} />
      )}

      {!s.rotation_id && (
        <Renewal s={s} locale={locale} t={t} onDone={refresh} />
      )}

      {s.status !== "pending_payment" && (
        <RateKitchen subscriptionId={s.id} locale={locale} />
      )}

      {s.status === "active" && !s.rotation_id && (
        <div id="pause">
          <Pause s={s} locale={locale} t={t} onDone={refresh} />
        </div>
      )}
      {live && !s.rotation_id && (
        <Cancel s={s} locale={locale} t={t} onDone={refresh} />
      )}
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
  const renewKey = useIdempotencyKey();
  const toggle = useMutation({
    mutationFn: (auto: boolean) => setRenewal(s.id, auto),
    onSuccess: onDone,
    onError: async (err) => {
      setError(problemMessage(err, locale));
      await onDone();
    },
  });
  const renew = useMutation({
    mutationFn: async () => {
      const next = await renewSubscription(s.id, {
        headers: renewKey.headers(),
      });
      try {
        await payOrder(next.order_id);
      } catch {
        // The new subscription shows its own pay button.
      }
      return next;
    },
    onSuccess: async (next) => {
      renewKey.reset();
      await onDone();
      router.push(`/${locale}/subscriptions/${next.id}`);
    },
    onError: (err) => setError(problemMessage(err, locale)),
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
  onNotice,
  editable,
}: {
  m: UpcomingMeal;
  sub: MySubscriptionDetail;
  locale: Locale;
  t: Copy;
  onChange: () => Promise<unknown>;
  onNotice: (text: string | null) => void;
  editable: boolean;
}) {
  const [error, setError] = useState<string | null>(null);
  const choose = useMutation({
    mutationFn: (dishId: string) =>
      chooseMeal(sub.id, m.date, m.meal_slot, dishId),
    onSuccess: onChange,
    onError: (err) => setError(problemMessage(err, locale)),
  });
  const skip = useMutation({
    mutationFn: () => skipMeal(sub.id, m.date, m.meal_slot),
    onSuccess: async () => {
      onNotice(t.skippedNote);
      await onChange();
    },
    onError: (err) => setError(problemMessage(err, locale)),
  });
  const unskip = useMutation({
    mutationFn: () => unskipMeal(sub.id, m.date, m.meal_slot),
    onSuccess: async () => {
      onNotice(t.unskipped);
      await onChange();
    },
    onError: (err) => setError(problemMessage(err, locale)),
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
    <li className={`ms-meal${m.locked || m.skipped ? " locked" : ""}`}>
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
        {m.options.length > 1 && canEdit && !m.skipped ? (
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
        {m.skipped && (
          <span className="ms-status ms-status-cancelled">{t.skipped}</span>
        )}
        {m.delivery_status && (
          <DeliveryStatus status={m.delivery_status} locale={locale} />
        )}
        {m.delivery_status === "completed" && chosen && (
          <DishMarks dishId={chosen} locale={locale} t={t} />
        )}
        <small className="ms-sub">
          {m.locked
            ? t.locked
            : m.options.length > 1 && !m.skipped
              ? t.until(cutoff)
              : t.skipUntil(cutoff)}
        </small>
        {error && (
          <small className="ms-bad" role="alert">
            {error}
          </small>
        )}
      </div>
      {canEdit && m.skipped && (
        <button
          type="button"
          className="ms-link"
          disabled={unskip.isPending}
          onClick={() => {
            setError(null);
            onNotice(null);
            unskip.mutate();
          }}
        >
          {t.unskip}
        </button>
      )}
      {canEdit && !m.skipped && (
        <button
          type="button"
          className="ms-link"
          disabled={skip.isPending}
          onClick={() => {
            if (window.confirm(t.skipConfirm)) {
              setError(null);
              onNotice(null);
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
  locale,
  t,
  onDone,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
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
    onError: (err) => setError(problemMessage(err, locale)),
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

function Resume({
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
  const key = useIdempotencyKey();
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => resumeSubscription(s.id, { headers: key.headers() }),
    onSuccess: async (out) => {
      key.reset();
      setDone(t.resumed(formatDay(locale, out.resumes_on)));
      await onDone();
    },
    onError: (err) => setError(problemMessage(err, locale)),
  });
  return (
    <div className="ms-renew">
      <button
        type="button"
        className="ms-cta ms-cta-quiet"
        disabled={m.isPending}
        onClick={() => {
          setError(null);
          setDone(null);
          m.mutate();
        }}
      >
        {m.isPending ? t.resuming : t.resume}
      </button>
      <p className="ms-sub">{t.resumeNote}</p>
      {done && (
        <p className="ms-ok" role="status">
          {done}
        </p>
      )}
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

function Cancel({
  s,
  locale,
  onDone,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
  t: Copy;
  onDone: () => Promise<unknown>;
}) {
  const [error, setError] = useState<string | null>(null);
  const key = useIdempotencyKey();
  const m = useMutation({
    mutationFn: (v: { code: CancellationReasonCode | null; note: string }) =>
      cancelSubscription(
        s.id,
        {
          ...(v.code ? { reason_code: v.code } : {}),
          ...(v.note ? { reason: v.note } : {}),
        },
        { headers: key.headers() },
      ),
    onSuccess: async () => {
      key.reset();
      await onDone();
    },
    onError: (err) => setError(problemMessage(err, locale)),
  });
  const reveal = (id: string) => {
    const el = document.getElementById(id);
    el?.querySelector("details")?.setAttribute("open", "");
    el?.scrollIntoView({ behavior: "smooth", block: "start" });
  };
  return (
    <CancelFlow
      kind="subscription"
      locale={locale}
      pending={m.isPending}
      error={error}
      onCancel={(code, note) => {
        setError(null);
        m.mutate({ code, note });
      }}
      saves={{
        pause: s.status === "active" ? () => reveal("pause") : undefined,
        rotationBuilderHref: `/${locale}/rotation`,
        guestWeek: s.guest_week_offer?.eligible
          ? () => reveal(GUEST_WEEK_ANCHOR)
          : undefined,
        cheaper: () => getCheaperPlans(s.id),
        kitchenHref: (b) => `/${locale}/kitchens/${b}`,
      }}
    />
  );
}

/**
 * "Loved it" / "Not again" on a dish that was delivered. One mark at a time:
 * the API keeps a single sentiment per dish, so hearting an avoided dish
 * replaces the avoid (and the other way round); tapping the active mark again
 * clears it.
 */
function DishMarks({
  dishId,
  locale,
  t,
}: {
  dishId: string;
  locale: Locale;
  t: Copy;
}) {
  const qc = useQueryClient();
  const tastes = useQuery({
    queryKey: ["me", "tastes"],
    queryFn: listMyTastes,
  });
  const mark =
    tastes.data?.items.find(
      (x) => x.subject_kind === "dish" && x.subject_id === dishId,
    )?.sentiment ?? null;
  const [error, setError] = useState<string | null>(null);
  const tap = useBusyGuard();
  const flip = useMutation({
    mutationFn: async (next: "favourite" | "avoid" | null) => {
      if (next)
        await setMyTaste({
          subject_kind: "dish",
          subject_id: dishId,
          sentiment: next,
        });
      else await clearMyTaste("dish", dishId);
    },
    onSuccess: async () => {
      setError(null);
      await qc.invalidateQueries({ queryKey: ["me", "tastes"] });
      await qc.invalidateQueries({ queryKey: ["me", "subscription"] });
    },
    onError: (err) => setError(problemMessage(err, locale) || t.tasteFailed),
    onSettled: tap.release,
  });
  if (tastes.isPending || tastes.isError) return null;
  const busy = flip.isPending;
  return (
    <span className="ms-taste">
      {mark === "favourite" ? (
        <>
          <button
            type="button"
            className="ms-link ms-taste-on"
            disabled={busy}
            aria-pressed={true}
            onClick={tap.guard(() => flip.mutate(null))}
          >
            ♥ {t.loved}
          </button>{" "}
          <button
            type="button"
            className="ms-link"
            disabled={busy}
            aria-pressed={true}
            onClick={tap.guard(() => flip.mutate(null))}
          >
            {t.undoAvoid}
          </button>
        </>
      ) : mark === "avoid" ? (
        <>
          <small className="ms-sub">{t.avoided}</small>{" "}
          <button
            type="button"
            className="ms-link"
            disabled={busy}
            onClick={tap.guard(() => flip.mutate(null))}
          >
            {t.undoAvoid}
          </button>
        </>
      ) : (
        <>
          <button
            type="button"
            className="ms-link ms-taste-love"
            disabled={busy}
            aria-pressed={false}
            onClick={tap.guard(() => flip.mutate("favourite"))}
          >
            ♡ {t.loveIt}
          </button>
          <button
            type="button"
            className="ms-link ms-taste-avoid"
            disabled={busy}
            aria-pressed={false}
            onClick={tap.guard(() => flip.mutate("avoid"))}
          >
            {t.notAgain}
          </button>
        </>
      )}
      {error && (
        <small className="ms-bad" role="alert">
          {error}
        </small>
      )}
    </span>
  );
}
