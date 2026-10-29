"use client";

import { formatMoney } from "@marketplace/api-client";
import Link from "next/link";
import { useState } from "react";

import type { DayDish, DishThumb } from "@/lib/meals-api";

type Locale = "en" | "ar";

const TINTS = 4;

function tint(seed: string) {
  let h = 0;
  for (const c of seed) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return (h % TINTS) + 1;
}

/** A dish photo, or a drawn tile with the dish's initial when there is none. */
export function DishPhoto({
  name,
  src,
  className = "",
}: {
  name: string;
  src: string | null | undefined;
  className?: string;
}) {
  const [broken, setBroken] = useState(false);
  if (src && !broken) {
    return (
      <img
        className={`ms-dish-ph ${className}`}
        src={src}
        alt=""
        loading="lazy"
        onError={() => setBroken(true)}
      />
    );
  }
  return (
    <span
      className={`ms-dish-ph ms-dish-drawn ms-cover-p${tint(name)} ${className}`}
      aria-hidden
    >
      {Array.from(name.trim())[0] ?? "·"}
    </span>
  );
}

export const dishName = (d: DishThumb, locale: Locale) =>
  (locale === "ar" && d.name_ar) || d.name;

export const FOOD_COPY = {
  en: {
    eyebrow: "Home-style kitchens across the UAE",
    title: ["Home-cooked lunch,", "every weekday."],
    lede: "Subscribe to a local kitchen for the month. A fixed menu by weekday, a second dish on some days, delivered to your door or office.",
    browse: "See kitchens near me",
    rotate: "Try 4 kitchens a month",
    proof: ["Delivers to you", "Skip or pause any day", "Cancel anytime"],
    tomorrow: "On the menu tomorrow",
    onDay: (day: string) => `On the menu ${day}`,
    tomorrowSub: "What kitchens near you are cooking next",
    choice: "2 dishes",
    perMeal: "a meal",
    cuisines: "What are you craving?",
  },
  ar: {
    eyebrow: "مطابخ منزلية في الإمارات",
    title: ["غداء منزلي،", "كل يوم عمل."],
    lede: "اشترك في مطبخ محلي لمدة شهر. قائمة ثابتة حسب أيام الأسبوع، وطبق ثانٍ في بعض الأيام، يصل إلى بابك أو مكتبك.",
    browse: "المطابخ القريبة مني",
    rotate: "جرّب ٤ مطابخ في الشهر",
    proof: ["يوصل إليك", "تخطَّ أو أوقف أي يوم", "ألغِ متى شئت"],
    tomorrow: "على القائمة غدًا",
    onDay: (day: string) => `على القائمة يوم ${day}`,
    tomorrowSub: "ما تطبخه المطابخ القريبة منك",
    choice: "طبقان",
    perMeal: "للوجبة",
    cuisines: "ماذا تشتهي؟",
  },
};

/** Photo collage for the home hero, from real dish photos when there are any. */
export function HomeHero({
  locale,
  photos,
}: {
  locale: Locale;
  photos: DishThumb[];
}) {
  const t = FOOD_COPY[locale];
  const shown = photos.filter((p) => p.image_url).slice(0, 3);
  return (
    <section className="ms-hh">
      <div className="ms-hh-copy">
        <h1>
          {t.title[0]} <em>{t.title[1]}</em>
        </h1>
        <p className="ms-hh-lede">{t.lede}</p>
        <div className="ms-hh-cta">
          <a className="ms-btn" href="#all-kitchens">
            {t.browse}
          </a>
          <Link className="ms-btn ms-btn-ghost" href={`/${locale}/rotation`}>
            {t.rotate}
          </Link>
        </div>
        <ul className="ms-proof">
          {t.proof.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      </div>
      {shown.length > 0 && (
        <div className={`ms-collage ms-collage-${shown.length}`} aria-hidden>
          {shown.map((p, i) => (
            <img key={i} className={`ms-c${i + 1}`} src={p.image_url!} alt="" />
          ))}
        </div>
      )}
    </section>
  );
}

/** The next delivery day's dishes from kitchens that deliver to the customer. */
export function DishRail({
  locale,
  items,
  title,
  slotLabel,
}: {
  locale: Locale;
  items: DayDish[];
  title: string;
  slotLabel: (s: string) => string;
}) {
  const t = FOOD_COPY[locale];
  return (
    <section className="ms-shelf">
      <div className="ms-shelf-head">
        <div>
          <h2>{title}</h2>
          <p>{t.tomorrowSub}</p>
        </div>
      </div>
      <ul className="ms-rail ms-dish-rail">
        {items.map((d, i) => {
          const name = (locale === "ar" && d.name_ar) || d.name;
          const kitchen =
            (locale === "ar" && d.kitchen_name_ar) || d.kitchen_name;
          return (
            <li key={`${d.branch_id}-${d.slot}-${i}`}>
              <Link
                className="ms-dish-card"
                href={`/${locale}/kitchens/${d.branch_id}`}
              >
                <span className="ms-dish-card-ph">
                  <DishPhoto name={name} src={d.image_url} />
                  {d.has_alternative && (
                    <span className="ms-chip ms-chip-hot">{t.choice}</span>
                  )}
                </span>
                <span className="ms-dish-card-body">
                  <b dir="auto">{name}</b>
                  <small dir="auto">
                    {kitchen} · {slotLabel(d.slot)}
                  </small>
                  <span className="ms-dish-card-meta">
                    <span>
                      <strong>
                        {formatMoney(
                          {
                            amount: d.from_meal_price_minor,
                            currency: d.currency,
                          },
                          locale,
                        )}
                      </strong>{" "}
                      {t.perMeal}
                    </span>
                    {d.rating != null && d.rating_count > 0 && (
                      <span className="ms-dish-card-star">
                        ★ {d.rating.toFixed(1)}
                      </span>
                    )}
                  </span>
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** Round cuisine photos; a cuisine without a photo gets a drawn circle. */
export function CuisineCircles({
  title,
  items,
  active,
  count,
  label = (c) => c,
  onPick,
}: {
  title: string;
  items: { cuisine: string; kitchens: number; image_url: string | null }[];
  active: string;
  count: (n: number) => string;
  label?: (c: string) => string;
  onPick: (c: string) => void;
}) {
  // A photo reused across cuisines reads as a bug; later repeats get a drawn tile.
  const used = new Set<string>();
  const shown = items.map((c) => {
    if (!c.image_url || used.has(c.image_url)) return { ...c, image_url: null };
    used.add(c.image_url);
    return c;
  });
  return (
    <section className="ms-shelf">
      <div className="ms-shelf-head">
        <h2>{title}</h2>
      </div>
      <div className="ms-rail ms-circles">
        {shown.map((c) => (
          <button
            key={c.cuisine}
            type="button"
            aria-pressed={active === c.cuisine}
            className={active === c.cuisine ? "on" : undefined}
            onClick={() => onPick(active === c.cuisine ? "" : c.cuisine)}
          >
            <DishPhoto
              name={label(c.cuisine)}
              src={c.image_url}
              className="ms-circle"
            />
            <span dir="auto">{label(c.cuisine)}</span>
            <small>{count(c.kitchens)}</small>
          </button>
        ))}
      </div>
    </section>
  );
}
