"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Fragment, use, useRef, useState } from "react";

import {
  CuisineCircles,
  DishRail,
  FOOD_COPY,
  HomeHero,
} from "@/components/meals/food";
import { useCustomerPoint } from "@/components/meals/location";
import { LocationPicker, placeLabel } from "@/components/meals/location-picker";
import {
  BannerCarousel,
  COPY,
  FILTER_SLOTS,
  KitchenCard,
  KitchenRail,
  MERCH_COPY,
  sectionTitle,
  tagLabel,
} from "@/components/meals/kitchen-card";
import { CustomerShell, formatDay } from "@/components/meals/shell";
import {
  getMealKitchens,
  getMealsHome,
  getTomorrowDishes,
  type HomeSection,
} from "@/lib/meals-api";

/** Tomorrow's date in Asia/Dubai as YYYY-MM-DD. */
function dubaiTomorrow() {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Dubai" }).format(
    Date.now() + 86_400_000,
  );
}

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
  const { point, precise, place, choose, locate } = useCustomerPoint();
  const [q, setQ] = useState("");
  const [slot, setSlot] = useState("");
  const [rotation, setRotation] = useState(false);

  const [cuisine, setCuisine] = useState("");
  // Kitchen count before any filter, so the cuisine strip stays put while filtering.
  const unfiltered = useRef<number | null>(null);

  const home = useQuery({
    queryKey: ["meals-home", point?.lat, point?.lng],
    queryFn: () => getMealsHome(point),
    enabled: point != null,
    // Picks up a scheduled layout in a tab left open across its publish time.
    refetchInterval: 60_000,
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

  const tomorrow = useQuery({
    queryKey: ["meal-dishes-next", point?.lat, point?.lng],
    queryFn: () => getTomorrowDishes(point),
    enabled: point != null,
  });
  const f = FOOD_COPY[locale];
  const seen = [
    ...(sections ?? []).flatMap((s) => s.kitchens ?? []),
    ...(list.data?.items ?? []),
  ];
  // Kitchen covers lead: kitchens pick them to show their signature food.
  const photos = [
    ...seen
      .filter((k) => k.cover_image_url)
      .map((k) => ({
        name: k.name,
        name_ar: k.name_ar,
        image_url: k.cover_image_url,
      })),
    ...(tomorrow.data?.items ?? []).map((d) => ({
      name: d.name,
      name_ar: d.name_ar,
      image_url: d.image_url,
    })),
    ...seen.flatMap((k) => k.week_dishes ?? []),
  ].filter(
    (p, i, all) =>
      p.image_url && all.findIndex((x) => x.image_url === p.image_url) === i,
  );

  const intro = (
    <>
      <HomeHero locale={locale} photos={photos} />
    </>
  );

  const next = tomorrow.data;
  const dishRail = next && next.items.length > 0 && (
    <DishRail
      locale={locale}
      items={next.items}
      title={
        next.date === dubaiTomorrow()
          ? f.tomorrow
          : f.onDay(formatDay(locale, next.date, { weekday: "long" }))
      }
      slotLabel={(s) => t.slot[s] ?? s}
    />
  );

  const promo = (
    <aside className="ms-promo">
      <h2>{t.promoTitle}</h2>
      {photos.length >= 4 && (
        <div className="ms-plates" aria-hidden>
          {photos.slice(0, 4).map((p) => (
            <img
              key={p.image_url}
              src={p.image_url!}
              alt=""
              onError={(e) => {
                e.currentTarget.style.display = "none";
              }}
            />
          ))}
        </div>
      )}
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

  const filtering = q !== "" || slot !== "" || rotation || cuisine !== "";
  const showPromo =
    sections == null || sections.some((s) => s.type === "rotation_promo");
  // Promo follows the first two rows of kitchens (or the whole list when
  // shorter) so the choice comes first and no kitchen is left stranded.
  const promoAfter = Math.min(list.data?.items.length ?? 0, 6);

  const filters = (
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
              {tagLabel(cuisine, locale)} ×
            </button>
          )}
        </div>
      </form>
    </div>
  );

  const kitchenList =
    !point || list.isPending ? (
      <p className="ms-note">{t.locating}</p>
    ) : list.isError ? (
      <p className="ms-note ms-bad">{t.failed}</p>
    ) : (
      <section className="ms-all-list">
        <div className="ms-count">
          <h2>
            {list.data.items.length === 0 && place && !filtering
              ? t.noneNear(placeLabel(place, locale))
              : place
                ? t.nearPlace(list.data.items.length, placeLabel(place, locale))
                : precise
                  ? t.near(list.data.items.length)
                  : t.nearFallback(list.data.items.length)}
          </h2>
          <LocationPicker
            locale={locale}
            current={place}
            onChoose={choose}
            onLocate={locate}
          />
        </div>
        {list.data.items.length === 0 && (
          <p className="ms-empty">{filtering ? t.empty : t.noneHelp}</p>
        )}
        <ul className="ms-grid">
          {list.data.items.map((k, i) => (
            <Fragment key={k.branch_id}>
              <KitchenCard k={k} locale={locale} t={t} />
              {showPromo && !filtering && i === promoAfter - 1 && (
                <li className="ms-grid-wide">{promo}</li>
              )}
            </Fragment>
          ))}
        </ul>
      </section>
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
        {intro}
        <p className="ms-note">{t.locating}</p>
      </CustomerShell>
    );
  }

  if (sections == null) {
    return (
      <CustomerShell locale={locale}>
        {intro}
        {filters}
        {kitchenList}
        {!filtering && dishRail}
      </CustomerShell>
    );
  }

  const cuisineStrip = sections.find((s) => s.type === "cuisine_strip");
  if (!filtering && list.data) unfiltered.current = list.data.items.length;
  // A cuisine every kitchen serves narrows nothing, so it isn't offered.
  const cuisines = (cuisineStrip?.cuisines ?? []).filter(
    (c) =>
      c.cuisine === cuisine ||
      unfiltered.current == null ||
      c.kitchens < unfiltered.current,
  );
  // Rails skip kitchens the grid already shows above them.
  const inGrid = new Set(
    showList && !filtering
      ? (list.data?.items ?? []).slice(0, promoAfter).map((k) => k.branch_id)
      : [],
  );
  const rails = sections
    .filter((s) => s.type === "kitchen_rail")
    .map((s) => ({
      ...s,
      kitchens: (s.kitchens ?? []).filter((k) => !inGrid.has(k.branch_id)),
    }))
    .filter((s) => s.kitchens.length > 0);

  return (
    <CustomerShell locale={locale}>
      {intro}
      {showList && filters}
      {cuisineStrip && cuisines.length > 1 && (
        <CuisineCircles
          title={sectionTitle(cuisineStrip, locale) || f.cuisines}
          items={cuisines}
          active={cuisine}
          count={t.count}
          label={(c) => tagLabel(c, locale)}
          onPick={pickCuisine}
        />
      )}
      {showList
        ? kitchenList
        : !filtering &&
          showPromo && <div className="ms-promo-wide">{promo}</div>}
      {!filtering &&
        sections
          .filter((s) => s.type === "banner_carousel")
          .map((s) => (
            <BannerCarousel
              key={s.id}
              banners={s.banners ?? []}
              locale={locale}
            />
          ))}
      {!filtering && dishRail}
      {!filtering &&
        rails.map((s) => <KitchenRail key={s.id} s={s} locale={locale} />)}
    </CustomerShell>
  );
}
