"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { KitchenCover } from "@/components/meals/cover";
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
    noReviews: "No reviews yet. Only customers who have had a delivery can rate a kitchen.",
    kitchenReply: "Kitchen's reply",
    days: "days",
    meals: (n: number) => (n === 1 ? "1 meal a day" : `${n} meals a day`),
    cutoff: (h: number) => `Changes up to ${h}h before delivery`,
    pause: (d: number) => `Pause up to ${d} days`,
    cycle: (d: number) => `per ${d}-day cycle`,
    rotation: "In the 4-kitchen rotation",
    choice: "Choose 1 of 2",
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
    noReviews: "لا توجد تقييمات بعد. يمكن فقط للعملاء الذين استلموا توصيلة تقييم المطبخ.",
    kitchenReply: "رد المطبخ",
    days: "أيام",
    meals: (n: number) => (n === 1 ? "وجبة يوميًا" : `${n} وجبات يوميًا`),
    cutoff: (h: number) => `التعديل حتى ${h} ساعة قبل التوصيل`,
    pause: (d: number) => `إيقاف حتى ${d} يومًا`,
    cycle: (d: number) => `لكل دورة ${d} يومًا`,
    rotation: "ضمن تناوب المطابخ الأربعة",
    choice: "اختر طبقًا من اثنين",
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

function Dish({
  d,
  locale,
  t,
}: {
  d: MenuChoice;
  locale: "en" | "ar";
  t: Copy;
}) {
  return (
    <span className="ms-dish">
      <span dir="auto">{(locale === "ar" && d.name_ar) || d.name}</span>
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
  if (slots.length === 0) return <p className="ms-note">{t.noMenu}</p>;
  const days = [0, 1, 2, 3, 4, 5, 6].filter((d) =>
    k.menu.slots.some((s) => s.weekday === d),
  );
  return (
    <div className="ms-menu">
      {slots.map((slot) => (
        <div key={slot} className="ms-menu-slot">
          <h3>{t.slot[slot] ?? slot}</h3>
          <dl>
            {days.map((day) => {
              const cell = k.menu.slots.find(
                (s) => s.weekday === day && s.slot === slot,
              );
              return (
                <div
                  key={day}
                  className={
                    cell?.alternative ? "ms-menu-row has-choice" : "ms-menu-row"
                  }
                >
                  <dt>
                    {t.weekdays[day]}
                    {cell?.alternative && (
                      <small className="ms-choice-tag">{t.choice}</small>
                    )}
                  </dt>
                  <dd>
                    {cell?.default ? (
                      <Dish d={cell.default} locale={locale} t={t} />
                    ) : (
                      <span className="ms-sub">—</span>
                    )}
                    {cell?.alternative && (
                      <>
                        <span className="ms-or">{t.or}</span>
                        <Dish d={cell.alternative} locale={locale} t={t} />
                      </>
                    )}
                  </dd>
                </div>
              );
            })}
          </dl>
        </div>
      ))}
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
      <div className="ms-kitchen">
        <section className="ms-hero ms-k-hero">
          <KitchenCover
            id={k.branch_id}
            name={name}
            src={k.cover_image_url}
            caption={k.cuisines.join(" · ")}
            className="ms-hero-img"
          />
          <div>
            <h1 dir="auto">{name}</h1>
            <p className="ms-sub" dir="auto">
              {(locale === "ar" && k.vendor_name_ar) || k.vendor_name}
              {k.city && ` · ${k.city}`}
            </p>
            {blurb && <p dir="auto">{blurb}</p>}
            <div className="ms-tags">
              {[...k.cuisines, ...k.dietary_tags].map((c) => (
                <span key={c} className="ms-tag">
                  {c}
                </span>
              ))}
            </div>
            {(k.rating_count > 0 || k.food_safety_verified) && (
              <div className="ms-trust">
                <RatingBadge rating={k.rating} count={k.rating_count} locale={locale} />
                {k.food_safety_verified && <SafetyBadge locale={locale} />}
              </div>
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
          </div>
        </section>

        <aside className="ms-k-plans">
          <h2>{t.plans}</h2>
          <ul className="ms-plans">
            {k.plan_list.map((plan) => (
              <li key={plan.id} className="ms-plan">
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
                  {plan.delivery_weekdays.map((d) => t.weekdays[d]).join(" · ")}
                  <br />
                  {plan.meal_slots.map((s) => t.slot[s] ?? s).join(", ")} ·{" "}
                  {t.meals(plan.meals_per_day)}
                  <br />
                  {t.cutoff(plan.cutoff_hours)}
                  {plan.allowed_pause_days > 0 &&
                    ` · ${t.pause(plan.allowed_pause_days)}`}
                </p>
                <p className="ms-plan-price">
                  <strong>{money(plan.price_minor, plan.currency)}</strong>{" "}
                  {t.cycle(plan.cycle_days)}
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
                      {t.rotationWeek}
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

        <section className="ms-k-menu">
          <h2>{t.menu}</h2>
          <p className="ms-sub">{t.menuNote}</p>
          <Menu k={k} locale={locale} t={t} />
        </section>

        <section className="ms-k-menu">
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
                      {new Date(r.created_at).toLocaleDateString(locale === "ar" ? "ar" : "en-GB", {
                        day: "numeric",
                        month: "short",
                        year: "numeric",
                      })}
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
    );
  }

  return (
    <CustomerShell locale={locale} back={{ href: `/${locale}`, label: t.back }}>
      {body}
    </CustomerShell>
  );
}
