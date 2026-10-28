"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { useCustomerPoint } from "@/components/meals/location";
import { KitchenCover } from "@/components/meals/cover";
import { CustomerShell } from "@/components/meals/shell";
import { getMealKitchens, type PublicKitchen } from "@/lib/meals-api";

/**
 * The front door: meal kitchens that deliver to you.
 *
 * Location is a hard filter on the API, so everything listed can actually
 * reach the customer. The video feed is still one click away at /feed.
 */

const COPY = {
  en: {
    title: "Kitchens that deliver to you",
    lede: "Subscribe to a home-style kitchen for the month. Each one cooks a fixed menu by weekday, and some let you pick between two dishes.",
    locating: "Finding kitchens near you…",
    fallback:
      "Showing Dubai Marina. Allow location to see kitchens that deliver to you.",
    search: "Search kitchens",
    any: "Any meal",
    rotation: "In the 4-kitchen rotation",
    rotationChip: "Rotation kitchens",
    promoEyebrow: "4-kitchen rotation",
    promoTitle: "Can't pick one kitchen? Rotate four.",
    promoBody:
      "Choose four kitchens for the month. Each one cooks a week, we set the order, and you pay each kitchen's weekly price.",
    promoCta: "Build a rotation",
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
    title: "مطابخ توصل إليك",
    lede: "اشترك في مطبخ منزلي لمدة شهر. لكل مطبخ قائمة ثابتة حسب أيام الأسبوع، وبعضها يتيح لك الاختيار بين طبقين.",
    locating: "جارٍ البحث عن مطابخ قريبة…",
    fallback: "نعرض دبي مارينا. اسمح بالموقع لرؤية المطابخ التي توصل إليك.",
    search: "ابحث عن مطبخ",
    any: "أي وجبة",
    rotation: "ضمن تناوب المطابخ الأربعة",
    rotationChip: "مطابخ التناوب",
    promoEyebrow: "تناوب ٤ مطابخ",
    promoTitle: "محتار بين المطابخ؟ جرّب أربعة.",
    promoBody:
      "اختر أربعة مطابخ للشهر. كل مطبخ يطبخ أسبوعًا، ونحن نحدد الترتيب، وتدفع السعر الأسبوعي لكل مطبخ.",
    promoCta: "ابدأ التناوب",
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
        <KitchenCover
          id={k.branch_id}
          name={name}
          src={k.cover_image_url}
          caption={k.cuisines.slice(0, 3).join(" · ")}
        />
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
              <span className="ms-rot-pill">
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
    <CustomerShell locale={locale}>
      <section className="ms-home-head">
        <div className="ms-intro">
          <h1>{t.title}</h1>
          <p>{t.lede}</p>
          {point && !precise && <p className="ms-note">{t.fallback}</p>}
        </div>
        <aside className="ms-promo">
          <p className="ms-eyebrow">{t.promoEyebrow}</p>
          <h2>{t.promoTitle}</h2>
          <div className="ms-promo-weeks" aria-hidden>
            <span />
            <span />
            <span />
            <span />
          </div>
          <p>{t.promoBody}</p>
          <Link className="ms-cta" href={`/${locale}/rotation`}>
            {t.promoCta}
          </Link>
        </aside>
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
        <div className="ms-chips ms-chips-scroll">
          <div role="radiogroup" aria-label={t.any} className="ms-chips">
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
          <button
            type="button"
            aria-pressed={rotation}
            className={rotation ? "on ms-chip-rot" : "ms-chip-rot"}
            onClick={() => setRotation(!rotation)}
            title={t.rotation}
          >
            {t.rotationChip}
          </button>
        </div>
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
    </CustomerShell>
  );
}
