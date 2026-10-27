"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { useCustomerPoint } from "@/components/meals/location";
import { getMealKitchens, type PublicKitchen } from "@/lib/meals-api";

/**
 * The front door: meal kitchens that deliver to you.
 *
 * Location is a hard filter on the API, so everything listed can actually
 * reach the customer. The video feed is still one click away at /feed.
 */

const COPY = {
  en: {
    brand: "Meals",
    feed: "Videos",
    title: "Kitchens that deliver to you",
    lede: "Subscribe to a home-style kitchen for the month. Each one cooks a fixed menu by weekday, and some let you pick between two dishes.",
    locating: "Finding kitchens near you…",
    fallback:
      "Showing Dubai Marina. Allow location to see kitchens that deliver to you.",
    search: "Search kitchens",
    any: "Any meal",
    rotation: "In the 4-kitchen rotation",
    from: "from",
    perCycle: "per cycle",
    perWeek: "a week in rotation",
    empty: "No kitchen delivers here yet with those filters.",
    failed: "Could not load kitchens.",
    count: (n: number) => (n === 1 ? "1 kitchen" : `${n} kitchens`),
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
    brand: "وجبات",
    feed: "فيديو",
    title: "مطابخ توصل إليك",
    lede: "اشترك في مطبخ منزلي لمدة شهر. لكل مطبخ قائمة ثابتة حسب أيام الأسبوع، وبعضها يتيح لك الاختيار بين طبقين.",
    locating: "جارٍ البحث عن مطابخ قريبة…",
    fallback: "نعرض دبي مارينا. اسمح بالموقع لرؤية المطابخ التي توصل إليك.",
    search: "ابحث عن مطبخ",
    any: "أي وجبة",
    rotation: "ضمن تناوب المطابخ الأربعة",
    from: "من",
    perCycle: "لكل دورة",
    perWeek: "أسبوعيًا ضمن التناوب",
    empty: "لا يوجد مطبخ يوصل هنا بهذه الخيارات بعد.",
    failed: "تعذر تحميل المطابخ.",
    count: (n: number) => `${n} مطبخ`,
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
const FILTER_SLOTS = ["breakfast", "lunch", "dinner", "suhoor", "iftar"];

function KitchenCard({
  k,
  locale,
  t,
}: {
  k: PublicKitchen;
  locale: "en" | "ar";
  t: Copy;
}) {
  const name = (locale === "ar" && k.name_ar) || k.name;
  const vendor = (locale === "ar" && k.vendor_name_ar) || k.vendor_name;
  const blurb = (locale === "ar" && k.description_ar) || k.description;
  return (
    <li>
      <Link className="ms-card" href={`/${locale}/kitchens/${k.branch_id}`}>
        <div className="ms-cover" aria-hidden>
          {k.cover_image_url ? (
            <img
              src={k.cover_image_url}
              alt=""
              loading="lazy"
              onError={(e) => (e.currentTarget.hidden = true)}
            />
          ) : (
            <span>{name.slice(0, 1)}</span>
          )}
        </div>
        <div className="ms-card-body">
          <div className="ms-card-head">
            <h2 dir="auto">{name}</h2>
            {name !== vendor && (
              <span className="ms-sub" dir="auto">
                {vendor}
              </span>
            )}
          </div>
          {blurb && (
            <p className="ms-blurb" dir="auto">
              {blurb}
            </p>
          )}
          <div className="ms-tags">
            {k.meal_slots.map((s) => (
              <span key={s} className="ms-tag ms-tag-slot">
                {t.slot[s] ?? s}
              </span>
            ))}
            {[...k.cuisines, ...k.dietary_tags].map((c) => (
              <span key={c} className="ms-tag">
                {c}
              </span>
            ))}
          </div>
          <div className="ms-price">
            <span>
              {t.from}{" "}
              <strong>
                {formatMoney(
                  { amount: k.from_price_minor, currency: k.currency },
                  locale,
                )}
              </strong>{" "}
              {t.perCycle}
            </span>
            {k.rotation_week_price_minor != null && (
              <span className="ms-rot">
                {formatMoney(
                  { amount: k.rotation_week_price_minor, currency: k.currency },
                  locale,
                )}{" "}
                {t.perWeek}
              </span>
            )}
          </div>
        </div>
      </Link>
    </li>
  );
}

export default function MealsHome({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = use(params).locale === "ar" ? "ar" : "en";
  const t = COPY[locale];
  const { point, precise } = useCustomerPoint();
  const [q, setQ] = useState("");
  const [slot, setSlot] = useState("");
  const [rotation, setRotation] = useState(false);

  const list = useQuery({
    queryKey: ["meal-kitchens", point?.lat, point?.lng, q, slot, rotation],
    queryFn: () => getMealKitchens({ point, q, slot, rotation }),
    enabled: point != null,
  });

  return (
    <div className="ms">
      <header className="ms-bar">
        <Link href={`/${locale}`} className="ms-brand">
          {t.brand}
        </Link>
        <nav className="ms-nav">
          <Link href={`/${locale}/feed`}>{t.feed}</Link>
          <Link href={`/${locale === "ar" ? "en" : "ar"}`}>
            {locale === "ar" ? "English" : "العربية"}
          </Link>
        </nav>
      </header>

      <main className="ms-main">
        <section className="ms-intro">
          <h1>{t.title}</h1>
          <p>{t.lede}</p>
          {point && !precise && <p className="ms-note">{t.fallback}</p>}
        </section>

        <form
          className="ms-filters"
          role="search"
          onSubmit={(e) => e.preventDefault()}
        >
          <input
            type="search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t.search}
            aria-label={t.search}
          />
          <div className="ms-chips" role="radiogroup" aria-label={t.any}>
            {["", ...FILTER_SLOTS].map((s) => (
              <button
                key={s || "any"}
                type="button"
                role="radio"
                aria-checked={slot === s}
                className={slot === s ? "on" : undefined}
                onClick={() => setSlot(s)}
              >
                {s ? t.slot[s] : t.any}
              </button>
            ))}
          </div>
          <label className="ms-toggle">
            <input
              type="checkbox"
              checked={rotation}
              onChange={(e) => setRotation(e.target.checked)}
            />
            {t.rotation}
          </label>
        </form>

        {!point || list.isPending ? (
          <p className="ms-note">{t.locating}</p>
        ) : list.isError ? (
          <p className="ms-note ms-bad">{t.failed}</p>
        ) : list.data.items.length === 0 ? (
          <p className="ms-empty">{t.empty}</p>
        ) : (
          <>
            <p className="ms-count">{t.count(list.data.items.length)}</p>
            <ul className="ms-grid">
              {list.data.items.map((k) => (
                <KitchenCard key={k.branch_id} k={k} locale={locale} t={t} />
              ))}
            </ul>
          </>
        )}
      </main>
    </div>
  );
}
