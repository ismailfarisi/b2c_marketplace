"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useEffect, useState, type KeyboardEvent } from "react";

import { KitchenCover } from "@/components/meals/cover";
import { DishPhoto, shortMoney } from "@/components/meals/food";
import { useCustomerPoint } from "@/components/meals/location";
import { CustomerShell, signInHref, useHasSession } from "@/components/meals/shell";
import { OFFER_COPY, offerLabel, offerName } from "@/components/meals/offers";
import {
  RatingBadge,
  SafetyBadge,
  VarietyBadge,
} from "@/components/meals/trust";
import {
  getKitchenOffers,
  getKitchenReviews,
  getMealKitchen,
  type PublicKitchenDetail,
} from "@/lib/meals-api";
import { useBusyGuard } from "@/lib/use-busy-guard";
import type { MenuChoice, MenuSlot } from "@/lib/vendor-api";
import { tagLabel } from "@/components/meals/kitchen-card";
import { allergenLine } from "@/components/meals/allergens";
import { LoadError } from "@/components/load-error";
import { clearMyTaste, listMyTastes, setMyTaste, type Sentiment } from "@/lib/customer-api";
import { problemMessage } from "@/lib/problem";

const COPY = {
  en: {
    back: "All kitchens",
    deliversYes: "Delivers to your location",
    heart: "Love it",
    hearted: "Loved — kept first when your rotation renews",
    notAgain: "Not again",
    avoidedKitchen: "Not again — left out of your suggestions",
    undoTaste: "Undo",
    tasteSignIn: "Sign in to remember what you love",
    tasteFailed: "Could not save. Try again.",
    deliversNo: "Does not deliver to your location",
    windows: "Delivery windows",
    menu: "Weekly menu",
    menuNote:
      "The same dish every week on the same day. Where two dishes are listed you choose one before the cutoff.",
    or: "or",
    choiceShort: "2 dishes",
    choiceHelp: "Days with a dot have two mains; pick one before the cutoff.",
    noMenu: "This kitchen has not published its menu yet.",
    plans: "Plans",
    reviews: "Reviews from subscribers",
    subscriber: "Subscriber",
    noReviews:
      "No reviews yet. Only customers who have had a delivery can rate a kitchen.",
    kitchenReply: "Kitchen's reply",
    days: "days",
    meals: (n: number) => (n === 1 ? "1 meal a day" : `${n} meals a day`),
    cutoff: (h: number) => `Changes up to ${h}h before delivery`,
    pause: (d: number) => `Pause up to ${d} days`,
    cycle: (d: number) => `per ${d}-day cycle`,
    perMeal: (m: string, n: number) => `${m} a meal · ${n} meals`,
    rotationMeal: (m: string) => `${m} a meal`,
    mealUnit: "a meal",
    mealCount: (n: number) => `${n} meals`,
    inRotation: "In a 4-kitchen rotation?",
    rotationHelp:
      "Pick four kitchens for the month; each cooks one week and you pay its weekly price.",
    rotation: "In the 4-kitchen rotation",
    choice: "Choose 1 of 2",
    popular: "Most popular",
    seePlans: "See plans",
    fromMeal: (m: string) => `from ${m} a meal`,
    glance: "The week at a glance",
    rotationWeek: "a week",
    subscribe: "Subscribe",
    allergens: "Allergens",
    kcal: "kcal",
    notFound: "This kitchen is not taking subscriptions right now.",
    failed: "Could not load this kitchen.",
    missing: "Kitchen not found. The link may be broken.",
    backToKitchens: "Back to kitchens",
    reviewsFailed: "Couldn't load reviews.",
    retry: "Try again",
    loading: "Loading…",
    weekdays: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
    menuWeeksNote: (n: number) =>
      `A different menu each week for ${n} weeks, then it starts again. Where two dishes are listed you choose one before the cutoff.`,
    menuWeeksTabs: "Menu weeks",
    thisWeek: "This week",
    nextWeek: "Next week",
    inWeeks: (n: number) => `In ${n} weeks`,
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
    back: "كل المطابخ",
    deliversYes: "يوصل إلى موقعك",
    heart: "أحبه",
    hearted: "أحببته — يبقى أولًا عند تجديد تناوبك",
    notAgain: "ليس مرة أخرى",
    avoidedKitchen: "ليس مرة أخرى — خارج اقتراحاتك",
    undoTaste: "تراجع",
    tasteSignIn: "سجّل الدخول لنتذكر ما تحبه",
    tasteFailed: "تعذر الحفظ. حاول مرة أخرى.",
    deliversNo: "لا يوصل إلى موقعك",
    windows: "أوقات التوصيل",
    menu: "القائمة الأسبوعية",
    menuNote:
      "الطبق نفسه كل أسبوع في اليوم نفسه. عند وجود طبقين تختار أحدهما قبل الموعد النهائي.",
    or: "أو",
    choiceShort: "طبقان",
    choiceHelp:
      "الأيام التي عليها نقطة فيها طبقان رئيسيان؛ اختر أحدهما قبل الموعد النهائي.",
    noMenu: "لم ينشر هذا المطبخ قائمته بعد.",
    plans: "الباقات",
    reviews: "تقييمات المشتركين",
    subscriber: "مشترك",
    noReviews:
      "لا توجد تقييمات بعد. يمكن فقط للعملاء الذين استلموا توصيلة تقييم المطبخ.",
    kitchenReply: "رد المطبخ",
    days: "أيام",
    meals: (n: number) => (n === 1 ? "وجبة يوميًا" : `${n} وجبات يوميًا`),
    cutoff: (h: number) => `التعديل حتى ${h} ساعة قبل التوصيل`,
    pause: (d: number) => `إيقاف حتى ${d} يومًا`,
    cycle: (d: number) => `لكل دورة ${d} يومًا`,
    perMeal: (m: string, n: number) => `${m} للوجبة · ${n} وجبة`,
    rotationMeal: (m: string) => `${m} للوجبة`,
    mealUnit: "للوجبة",
    mealCount: (n: number) => `${n} وجبة`,
    inRotation: "ضمن تناوب ٤ مطابخ؟",
    rotationHelp:
      "اختر أربعة مطابخ للشهر؛ كل مطبخ يطبخ أسبوعًا وتدفع سعره الأسبوعي.",
    rotation: "ضمن تناوب المطابخ الأربعة",
    choice: "اختر طبقًا من اثنين",
    popular: "الأكثر طلبًا",
    seePlans: "عرض الخطط",
    fromMeal: (m: string) => `من ${m} للوجبة`,
    glance: "الأسبوع في لمحة",
    rotationWeek: "أسبوعيًا",
    subscribe: "اشترك",
    allergens: "مسببات الحساسية",
    kcal: "سعرة",
    notFound: "هذا المطبخ لا يقبل اشتراكات حاليًا.",
    failed: "تعذر تحميل المطبخ.",
    missing: "لم يتم العثور على المطبخ. قد يكون الرابط غير صحيح.",
    backToKitchens: "العودة إلى المطابخ",
    reviewsFailed: "تعذر تحميل التقييمات.",
    retry: "حاول مرة أخرى",
    loading: "جارٍ التحميل…",
    weekdays: [
      "الأحد",
      "الاثنين",
      "الثلاثاء",
      "الأربعاء",
      "الخميس",
      "الجمعة",
      "السبت",
    ],
    menuWeeksNote: (n: number) =>
      `قائمة مختلفة كل أسبوع لمدة ${n === 2 ? "أسبوعين" : `${n} أسابيع`}، ثم تبدأ من جديد. عند وجود طبقين تختار أحدهما قبل الموعد النهائي.`,
    menuWeeksTabs: "أسابيع القائمة",
    thisWeek: "هذا الأسبوع",
    nextWeek: "الأسبوع القادم",
    inWeeks: (n: number) => (n === 2 ? "بعد أسبوعين" : `بعد ${n} أسابيع`),
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

/** Kitchen branch ids are UUIDs; anything else is a broken link, not a request. */
const KITCHEN_ID =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Weekday (0 = Sunday) of tomorrow in Asia/Dubai. */
function dubaiTomorrowWeekday() {
  const ymd = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Dubai",
  }).format(Date.now() + 86_400_000);
  return new Date(`${ymd}T00:00:00Z`).getUTCDay();
}

function DishCard({
  d,
  alt,
  locale,
  t,
}: {
  d: MenuChoice;
  alt?: boolean;
  locale: "en" | "ar";
  t: Copy;
}) {
  const name = (locale === "ar" && d.name_ar) || d.name;
  return (
    <div className="ms-mdish">
      <span className="ms-mdish-ph">
        <DishPhoto name={name} src={d.image_url} />
        {alt && <span className="ms-chip ms-chip-hot">{t.or}</span>}
      </span>
      <span className="ms-mdish-body">
        <b dir="auto">{name}</b>
        {(d.calories != null || d.allergens.length > 0) && (
          <small>
            {d.calories != null && `${d.calories} ${t.kcal}`}
            {d.calories != null && d.allergens.length > 0 && " · "}
            {d.allergens.length > 0 && (
              <span title={t.allergens}>
                {allergenLine(d.allergens, locale)}
              </span>
            )}
          </small>
        )}
      </span>
    </div>
  );
}

/**
 * A multi-week kitchen's menu by real calendar week: "This week", "Next
 * week" and so on up to the cycle length, each with its dates, so a customer
 * sees which dishes land on which days before subscribing.
 */
function MenuByWeek({
  k,
  locale,
  t,
}: {
  k: PublicKitchenDetail;
  locale: "en" | "ar";
  t: Copy;
}) {
  const weeks = k.menu_calendar ?? [];
  const [picked, setPicked] = useState(0);
  if (k.menu_weeks <= 1 || weeks.length <= 1)
    return <Menu slots={k.menu.slots} locale={locale} t={t} />;
  const i = Math.min(picked, weeks.length - 1);
  const fmt = new Intl.DateTimeFormat(locale === "ar" ? "ar" : "en", {
    day: "numeric",
    month: "short",
    timeZone: "UTC",
  });
  const range = (startsOn: string) => {
    const a = new Date(`${startsOn}T00:00:00Z`);
    const b = new Date(a.getTime() + 6 * 86_400_000);
    return `${fmt.format(a)} – ${fmt.format(b)}`;
  };
  const name = (n: number) =>
    n === 0 ? t.thisWeek : n === 1 ? t.nextWeek : t.inWeeks(n);
  const onTabKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
    let j = i;
    if (step) j += locale === "ar" ? -step : step;
    else if (e.key === "Home") j = 0;
    else if (e.key === "End") j = weeks.length - 1;
    else return;
    e.preventDefault();
    j = (j + weeks.length) % weeks.length;
    setPicked(j);
    e.currentTarget.parentElement
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
      [j]?.focus();
  };
  return (
    <>
      <div
        className="ms-days ms-weeks"
        role="tablist"
        aria-label={t.menuWeeksTabs}
      >
        {weeks.map((w, n) => (
          <button
            key={w.starts_on}
            type="button"
            role="tab"
            aria-selected={n === i}
            tabIndex={n === i ? 0 : -1}
            className={n === i ? "ms-day on" : "ms-day"}
            onClick={() => setPicked(n)}
            onKeyDown={onTabKey}
          >
            <b>{name(n)}</b> <small>{range(w.starts_on)}</small>
          </button>
        ))}
      </div>
      <div role="tabpanel">
        <Menu
          key={weeks[i]!.starts_on}
          slots={weeks[i]!.slots}
          fromTomorrow={i === 0}
          locale={locale}
          t={t}
        />
      </div>
    </>
  );
}

function Menu({
  slots: cells,
  fromTomorrow = true,
  locale,
  t,
}: {
  slots: MenuSlot[];
  fromTomorrow?: boolean;
  locale: "en" | "ar";
  t: Copy;
}) {
  const slots = [...new Set(cells.map((s) => s.slot))];
  const days = [0, 1, 2, 3, 4, 5, 6].filter((d) =>
    cells.some((s) => s.weekday === d && s.default),
  );
  const [picked, setPicked] = useState<number | null>(null);
  if (slots.length === 0 || days.length === 0)
    return <p className="ms-note">{t.noMenu}</p>;
  const from = fromTomorrow ? dubaiTomorrowWeekday() : 0;
  const day =
    picked ??
    [...days].sort((a, b) => ((a - from + 7) % 7) - ((b - from + 7) % 7))[0]!;
  const cell = (d: number, slot: string) =>
    cells.find((s) => s.weekday === d && s.slot === slot);
  const dishName = (d: MenuChoice) => (locale === "ar" && d.name_ar) || d.name;
  const onTabKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
    let i = days.indexOf(day);
    if (step) i += locale === "ar" ? -step : step;
    else if (e.key === "Home") i = 0;
    else if (e.key === "End") i = days.length - 1;
    else return;
    e.preventDefault();
    i = (i + days.length) % days.length;
    setPicked(days[i]!);
    e.currentTarget.parentElement
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
      [i]?.focus();
  };
  return (
    <div className="ms-menu2">
      {days.some((d) => slots.some((slot) => cell(d, slot)?.alternative)) && (
        <p className="ms-sub">{t.choiceHelp}</p>
      )}
      <div className="ms-days" role="tablist" aria-label={t.menu}>
        {days.map((d) => (
          <button
            key={d}
            type="button"
            role="tab"
            aria-selected={d === day}
            tabIndex={d === day ? 0 : -1}
            className={d === day ? "ms-day on" : "ms-day"}
            onClick={() => setPicked(d)}
            onKeyDown={onTabKey}
          >
            {t.weekdays[d]}
            {slots.some((slot) => cell(d, slot)?.alternative) && (
              <span
                className="ms-day-dot"
                role="img"
                aria-label={t.choiceShort}
              />
            )}
          </button>
        ))}
      </div>
      <div role="tabpanel">
        {slots.map((slot) => {
          const c = cell(day, slot);
          if (!c?.default) return null;
          return (
            <div key={slot} className="ms-mslot">
              <h3>{t.slot[slot] ?? slot}</h3>
              {c.alternative && <p className="ms-choice-note">{t.choice}</p>}
              <div className="ms-mcards">
                <DishCard d={c.default} locale={locale} t={t} />
                {c.alternative && (
                  <DishCard d={c.alternative} alt locale={locale} t={t} />
                )}
              </div>
            </div>
          );
        })}
      </div>
      <h3 className="ms-glance-h">{t.glance}</h3>
      <ul className="ms-glance">
        {days.flatMap((d) =>
          slots.map((slot) => {
            const c = cell(d, slot);
            if (!c?.default) return null;
            return (
              <li key={`${d}-${slot}`}>
                <DishPhoto
                  name={dishName(c.default)}
                  src={c.default.image_url}
                />
                <span className="ms-glance-day">{t.weekdays[d]}</span>
                <span className="ms-glance-dish">
                  <b dir="auto">{dishName(c.default)}</b>
                  <small dir="auto">
                    {t.slot[slot] ?? slot}
                    {c.alternative && ` · ${t.or} ${dishName(c.alternative)}`}
                  </small>
                </span>
              </li>
            );
          }),
        )}
      </ul>
    </div>
  );
}

export default function KitchenPage({
  params,
}: {
  params: Promise<{ locale: string; branchId: string }>;
}) {
  const p = use(params);
  const locale = p.locale === "ar" ? "ar" : "en";
  const t = COPY[locale];
  const { point, precise } = useCustomerPoint();
  const validId = KITCHEN_ID.test(p.branchId);
  const q = useQuery({
    queryKey: [
      "meal-kitchen",
      p.branchId,
      precise ? point?.lat : null,
      precise ? point?.lng : null,
    ],
    queryFn: () => getMealKitchen(p.branchId, precise ? point : null),
    enabled: point != null && validId,
  });
  const offers = useQuery({
    queryKey: ["meal-kitchen-offers", p.branchId],
    queryFn: () => getKitchenOffers(p.branchId),
    enabled: validId,
  });
  const reviews = useQuery({
    queryKey: ["meal-kitchen-reviews", p.branchId],
    queryFn: () => getKitchenReviews(p.branchId),
    enabled: validId,
  });
  // Offers are an extra: a failure hides them (no promo links or new-customer
  // pricing off a failed query) but is still reported.
  useEffect(() => {
    if (offers.isError)
      console.warn("kitchen offers failed to load", p.branchId, offers.error);
  }, [offers.isError, offers.error, p.branchId]);
  const subOffers = offers.isSuccess
    ? offers.data.items.filter((o) => o.applies_to !== "rotations")
    : [];

  const money = (amount: number, currency: string) =>
    shortMoney(amount, currency, locale);

  const status = q.error instanceof ApiError ? q.error.status : null;
  const backLink = (
    <p className="ms-empty">
      <Link href={`/${locale}/kitchens`}>{t.backToKitchens}</Link>
    </p>
  );

  let body;
  if (!validId || status === 400 || status === 422)
    body = (
      <>
        <p className="ms-empty">{t.missing}</p>
        {backLink}
      </>
    );
  else if (!point || q.isPending) body = <p className="ms-note">{t.loading}</p>;
  else if (q.isError)
    body = (
      <>
        {status === 404 ? (
          <p className="ms-empty">{t.notFound}</p>
        ) : (
          <LoadError
            error={q.error}
            onRetry={() => void q.refetch()}
            locale={locale}
          />
        )}
        {backLink}
      </>
    );
  else {
    const k = q.data;
    const name = (locale === "ar" && k.name_ar) || k.name;
    const blurb = (locale === "ar" && k.description_ar) || k.description;
    body = (
      <div className="ms-kitchen2">
        <section className="ms-khero">
          <KitchenCover
            id={k.branch_id}
            name={name}
            src={k.cover_image_url}
            fallback={
              k.menu.slots.find((s) => s.default?.image_url)?.default?.image_url
            }
            caption={k.cuisines.map((c) => tagLabel(c, locale)).join(" · ")}
            className="ms-khero-img"
          />
          <div className="ms-khero-copy">
            {(k.rating_count > 0 ||
              k.food_safety_verified ||
              k.no_repeat_weeks > 1) && (
              <div className="ms-kc-badges ms-khero-badges">
                <RatingBadge
                  rating={k.rating}
                  count={k.rating_count}
                  locale={locale}
                />
                {k.food_safety_verified && <SafetyBadge locale={locale} />}
                <VarietyBadge weeks={k.no_repeat_weeks} locale={locale} />
              </div>
            )}
            <h1 dir="auto">{name}</h1>
            <KitchenTaste branchId={k.branch_id} locale={locale} t={t} />
            <p className="ms-khero-sub" dir="auto">
              {(locale === "ar" && k.vendor_name_ar) || k.vendor_name}
              {k.city && ` · ${k.city}`}
              {k.cuisines.length > 0 &&
                ` · ${k.cuisines.map((c) => tagLabel(c, locale)).join(" · ")}`}
              {k.dietary_tags.length > 0 &&
                ` · ${k.dietary_tags.map((c) => tagLabel(c, locale)).join(" · ")}`}
            </p>
          </div>
        </section>

        <div className="ms-kgrid">
          <div className="ms-kmain">
            {blurb && (
              <p className="ms-klede" dir="auto">
                {blurb}
              </p>
            )}
            <ul className="ms-facts">
              {k.delivers_here != null && (
                <li className={k.delivers_here ? "ms-ok" : "ms-bad"}>
                  {k.delivers_here ? t.deliversYes : t.deliversNo}
                </li>
              )}
              {k.delivery_windows.length > 0 && (
                <li>
                  {t.windows}:{" "}
                  {k.delivery_windows
                    .map((w) => `${w.starts_at}–${w.ends_at}`)
                    .join(", ")}
                </li>
              )}
            </ul>

            <section className="ms-k-menu">
              <h2>{t.menu}</h2>
              <p className="ms-sub">
                {k.menu_weeks > 1 ? t.menuWeeksNote(k.menu_weeks) : t.menuNote}
              </p>
              <MenuByWeek k={k} locale={locale} t={t} />
            </section>

            <section className="ms-k-reviews">
              <h2>{t.reviews}</h2>
              {reviews.isError ? (
                <p className="ms-sub" role="alert">
                  {t.reviewsFailed}{" "}
                  <button
                    type="button"
                    className="ms-link"
                    onClick={() => void reviews.refetch()}
                    disabled={reviews.isFetching}
                  >
                    {t.retry}
                  </button>
                </p>
              ) : !reviews.isSuccess ? (
                <p className="ms-sub">{t.loading}</p>
              ) : reviews.data.items.length === 0 ? (
                <p className="ms-sub">{t.noReviews}</p>
              ) : (
                <ul className="ms-reviews">
                  {reviews.data!.items.map((r) => (
                    <li key={r.id} className="ms-review">
                      <span className="ms-rating" aria-label={`${r.rating}/5`}>
                        <span aria-hidden="true">{"★".repeat(r.rating)}</span>
                        <span className="ms-rating-count">
                          {" "}
                          {t.subscriber} ·{" "}
                          {new Date(r.created_at).toLocaleDateString(
                            locale === "ar" ? "ar" : "en-GB",
                            {
                              day: "numeric",
                              month: "short",
                              year: "numeric",
                            },
                          )}
                        </span>
                      </span>
                      {r.comment && <p dir="auto">{r.comment}</p>}
                      {r.reply && (
                        <p className="ms-review-reply" dir="auto">
                          <strong>{t.kitchenReply}:</strong> {r.reply}
                        </p>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </div>

          <aside id="plans" className="ms-k-plans">
            <h2>{t.plans}</h2>
            <ul className="ms-plans">
              {k.plan_list.map((plan) => (
                <li
                  key={plan.id}
                  className={
                    plan.id === k.popular_plan_id
                      ? "ms-plan is-popular"
                      : "ms-plan"
                  }
                >
                  {plan.id === k.popular_plan_id && (
                    <span className="ms-pop">{t.popular}</span>
                  )}
                  <h3 dir="auto">
                    {(locale === "ar" && plan.name_ar) || plan.name}
                  </h3>
                  {plan.description && (
                    <p dir="auto">
                      {(locale === "ar" && plan.description_ar) ||
                        plan.description}
                    </p>
                  )}
                  <p className="ms-plan-meal">
                    <strong>
                      {money(
                        Math.round(plan.price_minor / mealsPerCycle(plan)),
                        plan.currency,
                      )}
                    </strong>{" "}
                    {t.mealUnit}
                  </p>
                  <p className="ms-plan-price">
                    {money(plan.price_minor, plan.currency)}{" "}
                    {t.cycle(plan.cycle_days)} ·{" "}
                    {t.mealCount(mealsPerCycle(plan))}
                  </p>
                  <ul className="ms-plan-facts">
                    <li>
                      {plan.delivery_weekdays
                        .map((d) => t.weekdays[d])
                        .join(" · ")}
                    </li>
                    <li>
                      {plan.meal_slots.map((s) => t.slot[s] ?? s).join(", ")} ·{" "}
                      {t.meals(plan.meals_per_day)}
                    </li>
                    <li>
                      {t.cutoff(plan.cutoff_hours)}
                      {plan.allowed_pause_days > 0 &&
                        ` · ${t.pause(plan.allowed_pause_days)}`}
                    </li>
                  </ul>
                  {plan.tiers.length > 1 && (
                    <ul className="ms-tiers">
                      {plan.tiers.map((tier) => (
                        <li key={tier.id}>
                          <span dir="auto">
                            {(locale === "ar" && tier.name_ar) || tier.name}
                          </span>
                          <span>{money(tier.price_minor, plan.currency)}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                  {plan.rotation_enabled &&
                    plan.rotation_week_price_minor != null && (
                      <details className="ms-plan-rot">
                        <summary>{t.inRotation}</summary>
                        <p>
                          {money(plan.rotation_week_price_minor, plan.currency)}{" "}
                          {t.rotationWeek} ·{" "}
                          {t.rotationMeal(
                            money(
                              Math.round(
                                plan.rotation_week_price_minor /
                                  Math.max(
                                    1,
                                    plan.days_per_week * plan.meals_per_day,
                                  ),
                              ),
                              plan.currency,
                            ),
                          )}
                          . {t.rotationHelp}
                        </p>
                      </details>
                    )}
                  {k.delivers_here === false ? (
                    <small className="ms-bad">{t.deliversNo}</small>
                  ) : (
                    <>
                      {subOffers.map((o) => (
                        <Link
                          key={o.id}
                          className="ms-offer"
                          href={`/${locale}/kitchens/${k.branch_id}/subscribe/${plan.id}?promo=${o.id}`}
                        >
                          <strong dir="auto">{offerLabel(o, locale)}</strong>
                          <span dir="auto">
                            {offerName(o, locale)}
                            {o.audience === "new_customers" &&
                              ` · ${OFFER_COPY[locale].newOnly}`}
                          </span>
                        </Link>
                      ))}
                      <Link
                        className="ms-cta"
                        href={`/${locale}/kitchens/${k.branch_id}/subscribe/${plan.id}`}
                      >
                        {t.subscribe}
                      </Link>
                    </>
                  )}
                </li>
              ))}
            </ul>
          </aside>
        </div>

        {k.plan_list.length > 0 && (
          <div className="ms-sticky">
            <b>
              {t.fromMeal(
                money(
                  Math.min(
                    ...k.plan_list.map((pl) =>
                      Math.round(pl.price_minor / mealsPerCycle(pl)),
                    ),
                  ),
                  k.currency,
                ),
              )}
            </b>
            <a className="ms-btn" href="#plans">
              {t.seePlans}
            </a>
          </div>
        )}
      </div>
    );
  }

  return (
    <CustomerShell locale={locale} back={{ href: `/${locale}`, label: t.back }}>
      {body}
    </CustomerShell>
  );
}

/** Meals one cycle delivers: delivery days in the cycle times meals a day. */
function mealsPerCycle(p: {
  cycle_days: number;
  days_per_week: number;
  meals_per_day: number;
}) {
  return Math.max(
    1,
    Math.round((p.cycle_days * p.days_per_week) / 7) * p.meals_per_day,
  );
}

/** Heart or "not again" on the kitchen: remembered for renewal proposals. Signed out it points at sign-in. */
function KitchenTaste({ branchId, locale, t }: { branchId: string; locale: "en" | "ar"; t: Copy }) {
  const hasSession = useHasSession();
  const qc = useQueryClient();
  const tastes = useQuery({
    queryKey: ["me", "tastes"],
    queryFn: listMyTastes,
    enabled: hasSession,
  });
  const mark = tastes.data?.items.find(
    (x) => x.subject_kind === "kitchen" && x.subject_id === branchId,
  )?.sentiment;
  const [error, setError] = useState<string | null>(null);
  const tap = useBusyGuard();
  const flip = useMutation({
    mutationFn: async (s: Sentiment | null) => {
      if (s) await setMyTaste({ subject_kind: "kitchen", subject_id: branchId, sentiment: s });
      else await clearMyTaste("kitchen", branchId);
    },
    onSuccess: async () => {
      setError(null);
      await qc.invalidateQueries({ queryKey: ["me", "tastes"] });
    },
    onError: (err) => setError(problemMessage(err, locale) || t.tasteFailed),
    onSettled: tap.release,
  });
  const set = (s: Sentiment | null) => tap.guard(() => flip.mutate(s));
  if (!hasSession) {
    return (
      <p className="ms-taste ms-sub">
        <Link href={signInHref(locale, `/${locale}/kitchens/${branchId}`)}>♡ {t.tasteSignIn}</Link>
      </p>
    );
  }
  if (tastes.isPending || tastes.isError) return null;
  const busy = flip.isPending;
  return (
    <div className="ms-taste ms-chips" role="group" aria-label={t.heart}>
      {mark === "favourite" ? (
        <>
          <span className="ms-taste-on">♥ {t.hearted}</span>
          <button type="button" className="ms-link" disabled={busy} onClick={set(null)}>
            {t.undoTaste}
          </button>
          <button type="button" className="ms-link" disabled={busy} onClick={set("avoid")}>
            {t.notAgain}
          </button>
        </>
      ) : mark === "avoid" ? (
        <>
          <span className="ms-taste-on">⊘ {t.avoidedKitchen}</span>
          <button type="button" className="ms-link" disabled={busy} onClick={set(null)}>
            {t.undoTaste}
          </button>
          <button type="button" className="ms-link" disabled={busy} onClick={set("favourite")}>
            ♡ {t.heart}
          </button>
        </>
      ) : (
        <>
          <button type="button" aria-pressed={false} disabled={busy} onClick={set("favourite")}>
            ♡ {t.heart}
          </button>
          <button type="button" aria-pressed={false} disabled={busy} onClick={set("avoid")}>
            {t.notAgain}
          </button>
        </>
      )}
      {error && (
        <small className="ms-bad" role="alert">
          {error}
        </small>
      )}
    </div>
  );
}
