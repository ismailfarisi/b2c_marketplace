"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { KitchenCover } from "@/components/meals/cover";
import { DishPhoto } from "@/components/meals/food";
import { useCustomerPoint } from "@/components/meals/location";
import { CustomerShell } from "@/components/meals/shell";
import { OFFER_COPY, offerLabel, offerName } from "@/components/meals/offers";
import { RatingBadge, SafetyBadge } from "@/components/meals/trust";
import {
  getKitchenOffers,
  getKitchenReviews,
  getMealKitchen,
  type PublicKitchenDetail,
} from "@/lib/meals-api";
import type { MenuChoice } from "@/lib/vendor-api";

const COPY = {
  en: {
    back: "All kitchens",
    deliversYes: "Delivers to your location",
    deliversNo: "Does not deliver to your location",
    windows: "Delivery windows",
    menu: "Weekly menu",
    menuNote:
      "The same dish every week on the same day. Where two dishes are listed you choose one before the cutoff.",
    or: "or",
    noMenu: "This kitchen has not published its menu yet.",
    plans: "Plans",
    reviews: "Reviews from subscribers",
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
    loading: "Loading…",
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
    back: "كل المطابخ",
    deliversYes: "يوصل إلى موقعك",
    deliversNo: "لا يوصل إلى موقعك",
    windows: "أوقات التوصيل",
    menu: "القائمة الأسبوعية",
    menuNote:
      "الطبق نفسه كل أسبوع في اليوم نفسه. عند وجود طبقين تختار أحدهما قبل الموعد النهائي.",
    or: "أو",
    noMenu: "لم ينشر هذا المطبخ قائمته بعد.",
    plans: "الباقات",
    reviews: "تقييمات المشتركين",
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
              <span title={t.allergens}>{d.allergens.join(", ")}</span>
            )}
          </small>
        )}
      </span>
    </div>
  );
}

function Menu({
  k,
  locale,
  t,
}: {
  k: PublicKitchenDetail;
  locale: "en" | "ar";
  t: Copy;
}) {
  const slots = [...new Set(k.menu.slots.map((s) => s.slot))];
  const days = [0, 1, 2, 3, 4, 5, 6].filter((d) =>
    k.menu.slots.some((s) => s.weekday === d && s.default),
  );
  const [picked, setPicked] = useState<number | null>(null);
  if (slots.length === 0 || days.length === 0)
    return <p className="ms-note">{t.noMenu}</p>;
  const from = dubaiTomorrowWeekday();
  const day =
    picked ??
    [...days].sort((a, b) => ((a - from + 7) % 7) - ((b - from + 7) % 7))[0]!;
  const cell = (d: number, slot: string) =>
    k.menu.slots.find((s) => s.weekday === d && s.slot === slot);
  const dishName = (d: MenuChoice) => (locale === "ar" && d.name_ar) || d.name;
  return (
    <div className="ms-menu2">
      <div className="ms-days" role="tablist" aria-label={t.menu}>
        {days.map((d) => (
          <button
            key={d}
            type="button"
            role="tab"
            aria-selected={d === day}
            className={d === day ? "ms-day on" : "ms-day"}
            onClick={() => setPicked(d)}
          >
            {t.weekdays[d]}
            {slots.some((slot) => cell(d, slot)?.alternative) && (
              <small>{t.or}</small>
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
              <h3>
                {t.slot[slot] ?? slot}
                {c.alternative && (
                  <small className="ms-choice-tag">{t.choice}</small>
                )}
              </h3>
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
  const q = useQuery({
    queryKey: [
      "meal-kitchen",
      p.branchId,
      precise ? point?.lat : null,
      precise ? point?.lng : null,
    ],
    queryFn: () => getMealKitchen(p.branchId, precise ? point : null),
    enabled: point != null,
  });
  const offers = useQuery({
    queryKey: ["meal-kitchen-offers", p.branchId],
    queryFn: () => getKitchenOffers(p.branchId),
  });
  const reviews = useQuery({
    queryKey: ["meal-kitchen-reviews", p.branchId],
    queryFn: () => getKitchenReviews(p.branchId),
  });
  const subOffers = (offers.data?.items ?? []).filter(
    (o) => o.applies_to !== "rotations",
  );

  const money = (amount: number, currency: string) =>
    formatMoney({ amount, currency }, locale);

  let body;
  if (!point || q.isPending) body = <p className="ms-note">{t.loading}</p>;
  else if (q.isError)
    body = (
      <p className="ms-empty">
        {q.error instanceof ApiError && q.error.status === 404
          ? t.notFound
          : t.failed}
      </p>
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
            caption={k.cuisines.join(" · ")}
            className="ms-khero-img"
          />
          <div className="ms-khero-copy">
            {(k.rating_count > 0 || k.food_safety_verified) && (
              <div className="ms-kc-badges ms-khero-badges">
                <RatingBadge
                  rating={k.rating}
                  count={k.rating_count}
                  locale={locale}
                />
                {k.food_safety_verified && <SafetyBadge locale={locale} />}
              </div>
            )}
            <h1 dir="auto">{name}</h1>
            <p className="ms-khero-sub" dir="auto">
              {(locale === "ar" && k.vendor_name_ar) || k.vendor_name}
              {k.city && ` · ${k.city}`}
              {k.cuisines.length > 0 && ` · ${k.cuisines.join(" · ")}`}
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
            <div className="ms-tags">
              {k.dietary_tags.map((c) => (
                <span key={c} className="ms-tag">
                  {c}
                </span>
              ))}
            </div>
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
              <p className="ms-sub">{t.menuNote}</p>
              <Menu k={k} locale={locale} t={t} />
            </section>

            <section className="ms-k-reviews">
              <h2>{t.reviews}</h2>
              {(reviews.data?.items ?? []).length === 0 ? (
                <p className="ms-sub">{t.noReviews}</p>
              ) : (
                <ul className="ms-reviews">
                  {reviews.data!.items.map((r) => (
                    <li key={r.id} className="ms-review">
                      <span className="ms-rating" aria-label={`${r.rating}/5`}>
                        <span aria-hidden="true">{"★".repeat(r.rating)}</span>
                        <span className="ms-rating-count">
                          {" "}
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
                  <p className="ms-sub">
                    {plan.delivery_weekdays
                      .map((d) => t.weekdays[d])
                      .join(" · ")}
                    <br />
                    {plan.meal_slots
                      .map((s) => t.slot[s] ?? s)
                      .join(", ")} · {t.meals(plan.meals_per_day)}
                    <br />
                    {t.cutoff(plan.cutoff_hours)}
                    {plan.allowed_pause_days > 0 &&
                      ` · ${t.pause(plan.allowed_pause_days)}`}
                  </p>
                  <p className="ms-plan-price">
                    <strong>{money(plan.price_minor, plan.currency)}</strong>{" "}
                    {t.cycle(plan.cycle_days)}
                  </p>
                  <p className="ms-meal-price">
                    {t.perMeal(
                      money(
                        Math.round(plan.price_minor / mealsPerCycle(plan)),
                        plan.currency,
                      ),
                      mealsPerCycle(plan),
                    )}
                  </p>
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
                      <p className="ms-rot">
                        {t.rotation}:{" "}
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
                      </p>
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
