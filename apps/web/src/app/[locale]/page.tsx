"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useEffect, useState } from "react";

import { getFeed, type FeedItem } from "@/lib/feed-api";

/**
 * The front door.
 *
 * Until this QA pass it said *"Scaffold is up. The feed lands in Phase 2."* —
 * seven phases after Phase 2 shipped. The feed endpoint had been serving real
 * reels for months and nothing on the web called it.
 *
 * **What this is and is not.** It is a browse surface: posters, captions,
 * authors and CTAs. It is not a video player, and that is a decision rather
 * than a shortfall — PLAN-VIDEO-FIRST puts the reel experience in the native
 * app, and `apps/mobile-kmm` exists because playback performance is the product
 * and a JS bridge could not deliver it. A web page that autoplayed HLS in a
 * vertical viewport would be a worse version of the thing the KMM app is for.
 *
 * What the web is good for is the part the app is bad at: a link someone shares,
 * a search result, a browser on a laptop. So this renders what can be rendered
 * well and links onward.
 */

const COPY = {
  en: {
    title: "Near you",
    subtitle: "Everything here can reach your address right now.",
    locating: "Finding what is available near you…",
    fallback: "Showing Dubai Marina — allow location for what is deliverable to you.",
    empty:
      "Nothing is available at this location yet. This feed is finite by design: it shows what can actually reach you, so a thin result means thin local supply rather than a filter to loosen.",
    exhausted: "That is everything available near you.",
    failed: "Could not load the feed.",
    sponsored: "Sponsored",
    seconds: "s",
  },
  ar: {
    title: "بالقرب منك",
    subtitle: "كل ما هنا يمكن أن يصل إلى عنوانك الآن.",
    locating: "نبحث عمّا هو متاح بالقرب منك…",
    fallback: "نعرض دبي مارينا — فعّل الموقع لعرض ما يمكن توصيله إليك.",
    empty:
      "لا يوجد شيء متاح في هذا الموقع بعد. هذه الخلاصة محدودة بطبيعتها: تعرض ما يمكن أن يصلك فعلًا، لذا فإن قلّة النتائج تعني قلّة المعروض المحلي وليست فلترًا يمكن تخفيفه.",
    exhausted: "هذا كل ما هو متاح بالقرب منك.",
    failed: "تعذّر تحميل الخلاصة.",
    sponsored: "مموّل",
    seconds: "ث",
  },
} as const;

/**
 * Dubai Marina.
 *
 * The same fallback the KMM app takes, for the same reason: `lat`/`lng` are
 * required by the API because *a feed without a location is a different
 * product*, so there is no "everywhere" to fall back to. A fixed point is more
 * honest than a silent nationwide feed — and the banner says which is being
 * shown, because a viewer who sees laundry from a district they do not live in
 * should be told why.
 */
const FALLBACK = { lat: 25.08, lng: 55.14 };

function useViewerLocation() {
  const [point, setPoint] = useState<{ lat: number; lng: number } | null>(null);
  const [precise, setPrecise] = useState(false);

  useEffect(() => {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      setPoint(FALLBACK);
      return;
    }
    // A short timeout and no high-accuracy request. The feed needs a district,
    // not a doorstep, and waiting on GPS for a page that could already have
    // rendered is how a front door feels slow.
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setPoint({ lat: pos.coords.latitude, lng: pos.coords.longitude });
        setPrecise(true);
      },
      () => setPoint(FALLBACK),
      { timeout: 4000, maximumAge: 300_000 },
    );
  }, []);

  return { point, precise };
}

type Copy = (typeof COPY)[keyof typeof COPY];

function Card({ item, t }: { item: FeedItem; t: Copy }) {
  const seconds = Math.round(item.duration_ms / 1000);

  return (
    <li className="card reel">
      <div className="row spread">
        <strong dir="auto">{item.author.name}</strong>
        <div className="row">
          {/* Labelling paid placement is a legal requirement in both markets,
              not a nicety. The flag reaches this component because the QA pass
              found the API was not emitting it. */}
          {item.sponsored && <span className="pill">{t.sponsored}</span>}
          <span className="muted">
            {seconds}
            {t.seconds}
          </span>
        </div>
      </div>

      {/* dir="auto" rather than inheriting the page. A caption is written in
          whichever language the vendor chose, and an Arabic caption forced
          left-to-right inside an English page is unreadable. */}
      <p dir="auto">{item.caption}</p>

      {item.actions.length > 0 && (
        <div className="row">
          {item.actions.map((a) => (
            <button key={a.id} className="cta">
              {a.label}
            </button>
          ))}
        </div>
      )}

      {item.counters.likes + item.counters.comments > 0 && (
        <p className="muted">
          {item.counters.likes} · {item.counters.comments}
        </p>
      )}
    </li>
  );
}

export default function HomePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const t = COPY[locale === "ar" ? "ar" : "en"];
  const { point, precise } = useViewerLocation();

  const { data, isPending, error } = useQuery({
    queryKey: ["feed", point?.lat, point?.lng, locale],
    queryFn: () => getFeed({ lat: point!.lat, lng: point!.lng, locale }),
    // Not fetched until a location exists. Firing with a default and
    // re-fetching when the real one arrives would show a viewer content from
    // somewhere else first, which is the one impression this product cannot
    // afford to make.
    enabled: point !== null,
  });

  const items = data?.items ?? [];

  return (
    <main className="page">
      <h1>{t.title}</h1>
      <p className="muted">{t.subtitle}</p>

      {point && !precise && <p className="warn">{t.fallback}</p>}

      {(!point || isPending) && <p className="muted">{t.locating}</p>}
      {error && <p className="bad">{t.failed}</p>}

      {point && !isPending && !error && items.length === 0 && (
        <p className="muted">{t.empty}</p>
      )}

      <ul className="stack">
        {items.map((it) => (
          <Card key={it.id} item={it} t={t} />
        ))}
      </ul>

      {/* A feed that *can* be exhausted is the product promise, not a failure
          state — finite and locally relevant beats infinite and irrelevant. So
          the end of the list says so rather than spinning. */}
      {items.length > 0 && !data?.next_cursor && (
        <p className="muted">{t.exhausted}</p>
      )}
    </main>
  );
}
