import Link from "next/link";

import { KitchenCover } from "@/components/meals/cover";
import { DishPhoto, dishName, shortMoney } from "@/components/meals/food";
import { RatingBadge, SafetyBadge } from "@/components/meals/trust";
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
    perMeal: "a meal",
    perWeek: "a week in a 4-kitchen rotation",
    thisWeek: "this week",
    empty: "No kitchen delivers here yet with those filters.",
    failed: "Could not load kitchens.",
    count: (n: number) => (n === 1 ? "1 kitchen" : `${n} kitchens`),
    near: (n: number) =>
      n === 1 ? "1 kitchen delivers to you" : `${n} kitchens deliver to you`,
    nearFallback: (n: number) =>
      n === 1
        ? "1 kitchen delivers to Dubai Marina"
        : `${n} kitchens deliver to Dubai Marina`,
    noneNear: (place: string) => `No kitchens deliver to ${place} yet`,
    noneHelp: "Try a nearby area or use your current location.",
    nearPlace: (n: number, place: string) =>
      n === 1
        ? `1 kitchen delivers to ${place}`
        : `${n} kitchens deliver to ${place}`,
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
    perMeal: "للوجبة",
    perWeek: "أسبوعيًا ضمن تناوب ٤ مطابخ",
    thisWeek: "هذا الأسبوع",
    empty: "لا يوجد مطبخ يوصل هنا بهذه الخيارات بعد.",
    failed: "تعذر تحميل المطابخ.",
    count: (n: number) => `${n} مطبخ`,
    near: (n: number) => `${n} مطبخ يوصل إليك`,
    nearFallback: (n: number) => `${n} مطبخ يوصل إلى دبي مارينا`,
    nearPlace: (n: number, place: string) => `${n} مطبخ يوصل إلى ${place}`,
    noneNear: (place: string) => `لا توجد مطابخ توصل إلى ${place} بعد`,
    noneHelp: "جرّب منطقة قريبة أو استخدم موقعك الحالي.",
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

const TAGS: Record<string, { en: string; ar: string }> = {
  indian: { en: "Indian", ar: "هندي" },
  kerala: { en: "Kerala", ar: "كيرلا" },
  south_indian: { en: "South Indian", ar: "جنوب هندي" },
  pakistani: { en: "Pakistani", ar: "باكستاني" },
  arabic: { en: "Arabic", ar: "عربي" },
  emirati: { en: "Emirati", ar: "إماراتي" },
  lebanese: { en: "Lebanese", ar: "لبناني" },
  egyptian: { en: "Egyptian", ar: "مصري" },
  filipino: { en: "Filipino", ar: "فلبيني" },
  mediterranean: { en: "Mediterranean", ar: "متوسطي" },
  asian: { en: "Asian", ar: "آسيوي" },
  chinese: { en: "Chinese", ar: "صيني" },
  continental: { en: "Continental", ar: "أوروبي" },
  healthy: { en: "Healthy", ar: "صحي" },
  halal: { en: "Halal", ar: "حلال" },
  vegetarian: { en: "Vegetarian", ar: "نباتي" },
  vegan: { en: "Vegan", ar: "نباتي صرف" },
  gluten_free: { en: "Gluten-free", ar: "خالٍ من الغلوتين" },
  dairy_free: { en: "Dairy-free", ar: "خالٍ من الألبان" },
};

/** Display label for a cuisine or dietary tag slug. */
export function tagLabel(tag: string, locale: "en" | "ar") {
  const key = tag
    .trim()
    .toLowerCase()
    .replace(/[\s-]+/g, "_");
  const known = TAGS[key];
  if (known) return known[locale];
  return tag.replace(/[_-]+/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}
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
  const money = (amount: number) => shortMoney(amount, k.currency, locale);
  const dishes = k.week_dishes ?? [];
  return (
    <li>
      <Link
        className="ms-card ms-kc"
        href={`/${locale}/kitchens/${k.branch_id}`}
      >
        <div className="ms-kc-ph">
          <KitchenCover
            id={k.branch_id}
            name={name}
            src={k.cover_image_url}
            fallback={dishes.find((d) => d.image_url)?.image_url}
            caption={k.cuisines
              .slice(0, 3)
              .map((c) => tagLabel(c, locale))
              .join(" · ")}
          />
          <span className="ms-kc-price">
            <span className="ms-sub">{t.from}</span>{" "}
            <strong>{money(k.from_meal_price_minor)}</strong>{" "}
            <span className="ms-sub">{t.perMeal}</span>
          </span>
          {(k.rating_count > 0 || k.food_safety_verified) && (
            <span className="ms-kc-badges">
              <RatingBadge
                rating={k.rating}
                count={k.rating_count}
                locale={locale}
              />
              {k.food_safety_verified && <SafetyBadge locale={locale} />}
            </span>
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
          {(k.cuisines.length > 0 || k.dietary_tags.length > 0) && (
            <p className="ms-kc-cu" dir="auto">
              {[...k.cuisines, ...k.dietary_tags]
                .map((c) => tagLabel(c, locale))
                .join(" · ")}
            </p>
          )}
          {dishes.length > 0 && (
            <div className="ms-week" aria-label={t.thisWeek}>
              {dishes.map((d) => (
                <DishPhoto
                  key={d.name}
                  name={dishName(d, locale)}
                  src={d.image_url}
                />
              ))}
            </div>
          )}
          {k.rotation_week_price_minor != null && (
            <p className="ms-kc-rot">
              {money(k.rotation_week_price_minor)} {t.perWeek}
            </p>
          )}
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
