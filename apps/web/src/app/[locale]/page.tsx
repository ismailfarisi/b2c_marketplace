"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { useCustomerPoint } from "@/components/meals/location";
import {
  BannerCarousel,
  COPY,
  FILTER_SLOTS,
  KitchenCard,
  KitchenRail,
  MERCH_COPY,
  sectionTitle,
} from "@/components/meals/kitchen-card";
import { CustomerShell } from "@/components/meals/shell";
import {
  getMealKitchens,
  getMealsHome,
  type HomeSection,
} from "@/lib/meals-api";

/**
 * The front door: meal kitchens that deliver to you.
 *
 * Location is a hard filter on the API, so everything listed can actually
 * reach the customer. The video feed is still one click away at /feed.
 */

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

  const [cuisine, setCuisine] = useState("");

  const home = useQuery({
    queryKey: ["meals-home", point?.lat, point?.lng],
    queryFn: () => getMealsHome(point),
    enabled: point != null,
  });
  // A failed or empty merchandising response falls back to the default home.
  const sections: HomeSection[] | null = home.data?.sections ?? null;
  const showList =
    sections == null || sections.some((s) => s.type === "all_kitchens");

  const list = useQuery({
    queryKey: [
      "meal-kitchens",
      point?.lat,
      point?.lng,
      q,
      slot,
      rotation,
      cuisine,
    ],
    queryFn: () => getMealKitchens({ point, q, slot, rotation, cuisine }),
    enabled: point != null && showList,
  });

  const intro = (
    <div className="ms-intro">
      <h1>{t.title}</h1>
      <p>{t.lede}</p>
      {point && !precise && <p className="ms-note">{t.fallback}</p>}
    </div>
  );

  const promo = (
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
  );

  const allKitchens = (
    <div id="all-kitchens" className="ms-all">
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
          {cuisine && (
            <button
              type="button"
              className="on"
              aria-label={MERCH_COPY[locale].clear}
              onClick={() => setCuisine("")}
            >
              {cuisine} ×
            </button>
          )}
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
    </div>
  );

  const pickCuisine = (c: string) => {
    setCuisine(c);
    document
      .getElementById("all-kitchens")
      ?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  if (!point || home.isPending) {
    return (
      <CustomerShell locale={locale}>
        <section className="ms-home-head ms-home-solo">{intro}</section>
        <p className="ms-note">{t.locating}</p>
      </CustomerShell>
    );
  }

  if (sections == null) {
    return (
      <CustomerShell locale={locale}>
        <section className="ms-home-head">
          {intro}
          {promo}
        </section>
        {allKitchens}
      </CustomerShell>
    );
  }

  return (
    <CustomerShell locale={locale}>
      <section className="ms-home-head ms-home-solo">{intro}</section>
      {sections.map((s) => {
        switch (s.type) {
          case "banner_carousel":
            return (
              <BannerCarousel
                key={s.id}
                banners={s.banners ?? []}
                locale={locale}
              />
            );
          case "kitchen_rail":
            return <KitchenRail key={s.id} s={s} locale={locale} />;
          case "rotation_promo":
            return (
              <div key={s.id} className="ms-promo-wide">
                {promo}
              </div>
            );
          case "cuisine_strip":
            return (
              <section key={s.id} className="ms-shelf">
                <div className="ms-shelf-head">
                  <h2>
                    {sectionTitle(s, locale) || MERCH_COPY[locale].cuisines}
                  </h2>
                </div>
                <div className="ms-rail ms-cuisines">
                  {(s.cuisines ?? []).map((c) => (
                    <button
                      key={c.cuisine}
                      type="button"
                      className={cuisine === c.cuisine ? "on" : undefined}
                      onClick={() => pickCuisine(c.cuisine)}
                    >
                      <span dir="auto">{c.cuisine}</span>
                      <small>{t.count(c.kitchens)}</small>
                    </button>
                  ))}
                </div>
              </section>
            );
          case "all_kitchens":
            return <div key={s.id}>{allKitchens}</div>;
          default:
            return null;
        }
      })}
    </CustomerShell>
  );
}
