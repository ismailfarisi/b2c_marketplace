import { formatMoney } from "@marketplace/api-client";
import Link from "next/link";

import { KitchenCover } from "@/components/meals/cover";
import type { Banner, HomeSection, PublicKitchen } from "@/lib/meals-api";

/** Kitchen card and copy shared by the home page and collection pages. */

export const COPY = {
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

export type Copy = (typeof COPY)["en"];
export const FILTER_SLOTS = ["breakfast", "lunch", "dinner", "suhoor", "iftar"];

export function KitchenCard({
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

export const MERCH_COPY = {
  en: {
    seeAll: "See all",
    cuisines: "Browse by cuisine",
    clear: "Clear cuisine",
  },
  ar: {
    seeAll: "عرض الكل",
    cuisines: "تصفح حسب المطبخ",
    clear: "إلغاء المطبخ",
  },
};

export function sectionTitle(s: HomeSection, locale: "en" | "ar") {
  const c = s.collection;
  return (
    (locale === "ar" && s.settings.title_ar) ||
    s.settings.title ||
    (c && ((locale === "ar" && c.title_ar) || c.title)) ||
    ""
  );
}

export function BannerCarousel({
  banners,
  locale,
}: {
  banners: Banner[];
  locale: "en" | "ar";
}) {
  return (
    <section className="ms-banners" aria-roledescription="carousel">
      <ul className="ms-rail">
        {banners.map((b) => {
          const title = (locale === "ar" && b.title_ar) || b.title;
          const body = (locale === "ar" && b.body_ar) || b.body;
          const cta = (locale === "ar" && b.cta_ar) || b.cta;
          const href = b.link_url?.startsWith("/")
            ? `/${locale}${b.link_url}`
            : b.link_url;
          const inner = (
            <>
              {b.image_url && <img src={b.image_url} alt="" />}
              <div className="ms-banner-copy">
                <h2 dir="auto">{title}</h2>
                {body && <p dir="auto">{body}</p>}
                {cta && href && <span className="ms-cta">{cta}</span>}
              </div>
            </>
          );
          return (
            <li
              key={b.id}
              className={b.image_url ? "ms-banner ms-banner-img" : "ms-banner"}
            >
              {href ? <Link href={href}>{inner}</Link> : <div>{inner}</div>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function KitchenRail({
  s,
  locale,
}: {
  s: HomeSection;
  locale: "en" | "ar";
}) {
  const t = COPY[locale];
  const c = s.collection;
  const sub = c && ((locale === "ar" && c.subtitle_ar) || c.subtitle);
  return (
    <section className="ms-shelf">
      <div className="ms-shelf-head">
        <div>
          <h2>{sectionTitle(s, locale)}</h2>
          {sub && <p dir="auto">{sub}</p>}
        </div>
        {c && (
          <Link className="ms-link" href={`/${locale}/collections/${c.slug}`}>
            {MERCH_COPY[locale].seeAll}
          </Link>
        )}
      </div>
      <ul className="ms-rail ms-rail-cards">
        {(s.kitchens ?? []).map((k) => (
          <KitchenCard key={k.branch_id} k={k} locale={locale} t={t} />
        ))}
      </ul>
    </section>
  );
}
