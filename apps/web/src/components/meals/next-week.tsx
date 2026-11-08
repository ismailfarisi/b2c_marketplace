"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

import { LoadError } from "@/components/load-error";
import { KitchenCover } from "@/components/meals/cover";
import { DishPhoto } from "@/components/meals/food";
import { COPY as CARD, tagLabel } from "@/components/meals/kitchen-card";
import {
  formatDay,
  formatRange,
  useHasSession,
} from "@/components/meals/shell";
import {
  getMyNextWeek,
  type NextWeekPreview,
  type PreviewDish,
  type RotationRecap,
} from "@/lib/customer-api";

type Locale = "en" | "ar";

const COPY = {
  en: {
    nextWeek: "Next week",
    title: (kitchen: string) => `Next week: ${kitchen}`,
    homeCooking: (cuisine: string) => `${cuisine} home cooking`,
    homeStyle: "Home-style cooking",
    noDishes: "Menu coming soon",
    noMeals: "No meals next week",
    noMealsHint: "Nothing is on the way next week. Renew, or find a kitchen.",
    browse: "Find a kitchen",
    loading: "Loading next week…",
    open: "See the kitchen",
    more: (n: number) => `+${n} more kitchen${n === 1 ? "" : "s"} next week`,
    recapTitle: "Your month of kitchens",
    recapEnded: "Your month of kitchens is done",
    kitchens: "Kitchens",
    cuisines: "Cuisines",
    dishes: "Meals delivered",
    skipped: "Days skipped",
    favourite: (k: string, r: number) => `Your favourite: ${k} (${r}★)`,
    renew: "See next month's kitchens",
  },
  ar: {
    nextWeek: "الأسبوع القادم",
    title: (kitchen: string) => `الأسبوع القادم: ${kitchen}`,
    homeCooking: (cuisine: string) => `طبخ بيتي ${cuisine}`,
    homeStyle: "طبخ بيتي",
    noDishes: "القائمة قريبًا",
    noMeals: "لا وجبات الأسبوع القادم",
    noMealsHint: "لا شيء في الطريق الأسبوع القادم. جدّد اشتراكك أو ابحث عن مطبخ.",
    browse: "ابحث عن مطبخ",
    loading: "جارٍ تحميل الأسبوع القادم…",
    open: "عرض المطبخ",
    more: (n: number) => `+${n} مطابخ أخرى الأسبوع القادم`,
    recapTitle: "شهرك مع المطابخ",
    recapEnded: "انتهى شهرك مع المطابخ",
    kitchens: "المطابخ",
    cuisines: "المطابخ الشعبية",
    dishes: "الوجبات المُوصلة",
    skipped: "الأيام المتخطاة",
    favourite: (k: string, r: number) => `المفضل لديك: ${k} (${r}★)`,
    renew: "شاهد مطابخ الشهر القادم",
  },
};

const pick = (en: string, ar: string | null, locale: Locale) =>
  (locale === "ar" && ar) || en;

/** "Lebanese home cooking", from the kitchen's first cuisine tag. */
export function cuisineLine(cuisines: string[], locale: Locale) {
  const t = COPY[locale];
  const first = cuisines[0];
  return first ? t.homeCooking(tagLabel(first, locale)) : t.homeStyle;
}

/** The dish the kitchen will send: chosen, else the default, else the first. */
function shownDish(dishes: PreviewDish[]) {
  return (
    dishes.find((d) => d.chosen) ??
    dishes.find((d) => d.is_default) ??
    dishes[0]
  );
}

/**
 * The "Next week" card at the top of My meals. `hasSubscriptions` turns the
 * empty answer into a "No meals next week" card; a customer with nothing to
 * renew sees the page's own empty state instead.
 */
export function NextWeekCard({
  locale,
  hasSubscriptions = false,
}: {
  locale: Locale;
  hasSubscriptions?: boolean;
}) {
  const signedIn = useHasSession();
  const q = useQuery({
    queryKey: ["me", "next-week"],
    queryFn: getMyNextWeek,
    enabled: signedIn,
  });
  if (!signedIn) return null;
  const t = COPY[locale];
  if (q.isPending) {
    return (
      <p className="ms-note" aria-live="polite">
        {t.loading}
      </p>
    );
  }
  if (q.isError) {
    return (
      <LoadError
        error={q.error}
        onRetry={() => void q.refetch()}
        locale={locale}
      />
    );
  }
  const [first, ...rest] = q.data.items;
  if (!first) {
    if (!hasSubscriptions) return null;
    return (
      <section className="ms-next-week ms-next-week-empty" aria-live="polite">
        <strong>{t.noMeals}</strong>
        <p className="ms-sub">{t.noMealsHint}</p>
        <Link className="ms-cta ms-cta-quiet" href={`/${locale}`}>
          {t.browse}
        </Link>
      </section>
    );
  }
  return (
    <section className="ms-next-week" aria-labelledby="ms-next-week-title">
      <NextWeekBody locale={locale} p={first} />
      {rest.length > 0 && <p className="ms-sub">{t.more(rest.length)}</p>}
    </section>
  );
}

function NextWeekBody({ locale, p }: { locale: Locale; p: NextWeekPreview }) {
  const t = COPY[locale];
  const kitchen = pick(p.kitchen_name, p.kitchen_name_ar, locale);
  const slots = CARD[locale].slot;
  return (
    <>
      <Link
        href={`/${locale}/kitchens/${p.branch_id}`}
        className="ms-next-week-head"
      >
        <KitchenCover
          id={p.branch_id}
          name={kitchen}
          src={p.cover_image_url}
          fallback={
            p.meals.flatMap((m) => m.dishes).find((d) => d.image_url)?.image_url
          }
          className="ms-next-week-cover"
        />
        <span className="ms-next-week-text">
          <span className="ms-next-week-kicker">
            {t.nextWeek} · {formatRange(locale, p.starts_on, p.ends_on)}
          </span>
          <strong id="ms-next-week-title" dir="auto">
            {t.title(kitchen)} – {cuisineLine(p.cuisines, locale)}
          </strong>
          <span className="ms-next-week-open">{t.open}</span>
        </span>
      </Link>
      <ul className="ms-next-week-days">
        {p.meals.map((m) => {
          const d = shownDish(m.dishes);
          const name = d ? pick(d.name, d.name_ar, locale) : t.noDishes;
          return (
            <li key={`${m.date}-${m.meal_slot}`}>
              <DishPhoto
                name={name}
                src={d?.image_url}
                className="ms-next-week-ph"
              />
              <span className="ms-sub">
                {formatDay(locale, m.date, {
                  weekday: "short",
                  day: "numeric",
                })}{" "}
                · {slots[m.meal_slot] ?? m.meal_slot}
              </span>
              <span dir="auto">{name}</span>
            </li>
          );
        })}
      </ul>
    </>
  );
}

/** The end-of-cycle recap inside a rotation card, from week 4 on. */
export function RecapCard({
  locale,
  recap,
}: {
  locale: Locale;
  recap: RotationRecap;
}) {
  const t = COPY[locale];
  const fav = recap.favourite;
  return (
    <div className="ms-recap" aria-labelledby={`ms-recap-${recap.rotation_id}`}>
      <strong id={`ms-recap-${recap.rotation_id}`}>
        {recap.ended ? t.recapEnded : t.recapTitle}
      </strong>
      <dl className="ms-recap-stats">
        <div>
          <dt>{t.kitchens}</dt>
          <dd>{recap.kitchen_count}</dd>
        </div>
        <div>
          <dt>{t.cuisines}</dt>
          <dd>{recap.cuisine_count}</dd>
        </div>
        <div>
          <dt>{t.dishes}</dt>
          <dd>{recap.dishes_delivered}</dd>
        </div>
        <div>
          <dt>{t.skipped}</dt>
          <dd>{recap.days_skipped}</dd>
        </div>
      </dl>
      {fav && (
        <p dir="auto">
          {t.favourite(
            pick(fav.kitchen_name, fav.kitchen_name_ar, locale),
            fav.rating,
          )}
        </p>
      )}
      <Link className="ms-cta" href={`/${locale}${recap.renew_path}`}>
        {t.renew}
      </Link>
    </div>
  );
}
