"use client";

import { ApiError } from "@marketplace/api-client";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { use, useEffect, useState } from "react";

import { clickAction, getFeed, type FeedAction, type FeedItem } from "@/lib/feed-api";
import { LoadError } from "@/components/load-error";
import { signInHref } from "@/components/meals/shell";
import { problemMessage } from "@/lib/problem";
import { NEEDS_ACCOUNT, hrefFor } from "@/lib/feed-routes";
import { tokenStore } from "@/lib/token-store";

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
    badLocation: "We couldn't use that location. Showing nearby results needs a valid place — try again or allow location.",
    loadingFeed: "Loading the feed…",
    more: "Load more",
    moreLoading: "Loading…",
    moreFailed: "Couldn't load more. Try again.",
    sponsored: "Sponsored",
    unavailable: "This isn't available right now.",
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
    badLocation: "تعذّر استخدام هذا الموقع. عرض النتائج القريبة يحتاج إلى مكان صالح — حاول مرة أخرى أو فعّل الموقع.",
    loadingFeed: "جارٍ تحميل الخلاصة…",
    more: "عرض المزيد",
    moreLoading: "جارٍ التحميل…",
    moreFailed: "تعذّر تحميل المزيد. حاول مرة أخرى.",
    sponsored: "مموّل",
    unavailable: "هذا غير متاح حاليًا.",
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

function Cta({
  action,
  reelId,
  t,
  locale,
}: {
  action: FeedAction;
  reelId: string;
  t: Copy;
  locale: "en" | "ar";
}) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const label = (locale === "ar" && action.label_ar) || action.label;
  return (
    <>
      <button
        type="button"
        className="cta"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          setNote(null);
          try {
            const r = await clickAction(action.id);
            if (!r.available || !r.route) {
              // The server's reason is English-only; Arabic readers get ours.
              setNote(locale === "en" && r.reason ? r.reason : t.unavailable);
              return;
            }
            const href = hrefFor(r.route, locale, { reelId, actionId: action.id });
            if (href.startsWith("http")) window.open(href, "_blank", "noopener");
            else if (
              NEEDS_ACCOUNT.test(href) &&
              !tokenStore.getAccessToken() &&
              !tokenStore.getRefreshToken()
            )
              router.push(signInHref(locale, href));
            else router.push(href);
          } catch (err) {
            setNote(problemMessage(err, locale));
          } finally {
            setBusy(false);
          }
        }}
      >
        {label}
      </button>
      {note ? (
        <span className="muted" role="status">
          {note}
        </span>
      ) : null}
    </>
  );
}

function Card({ item, t, locale }: { item: FeedItem; t: Copy; locale: "en" | "ar" }) {
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
            <Cta key={a.id} action={a} reelId={item.id} t={t} locale={locale} />
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

/** Placeholder cards while the first page is in flight, so the page reads as loading rather than stuck. */
function FeedSkeleton({ label }: { label: string }) {
  const bar = (width: string) => (
    <span
      aria-hidden
      style={{
        display: "block",
        height: "0.9em",
        width,
        borderRadius: 4,
        background: "currentColor",
        opacity: 0.12,
        marginBlock: "0.4em",
      }}
    />
  );
  return (
    <div role="status" aria-busy="true" aria-label={label}>
      <ul className="stack">
        {[0, 1, 2].map((i) => (
          <li key={i} className="card reel" aria-hidden>
            {bar("40%")}
            {bar("90%")}
            {bar("70%")}
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function FeedPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const t = COPY[locale === "ar" ? "ar" : "en"];
  const { point, precise } = useViewerLocation();

  const loc = locale === "ar" ? "ar" : "en";
  const {
    data,
    isPending,
    error,
    refetch,
    isRefetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
  } = useInfiniteQuery({
    queryKey: ["feed", point?.lat, point?.lng, locale],
    queryFn: ({ pageParam }) =>
      getFeed({ lat: point!.lat, lng: point!.lng, locale, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    // Not fetched until a location exists. Firing with a default and
    // re-fetching when the real one arrives would show a viewer content from
    // somewhere else first, which is the one impression this product cannot
    // afford to make.
    enabled: point !== null,
    // One retry, not react-query's three: a dead API otherwise leaves the page
    // looking stuck for several seconds before it admits the failure.
    retry: 1,
  });

  const items = data?.pages.flatMap((p) => p.items) ?? [];
  // A 422 means the API refused the coordinates — retrying the same request
  // will not help the way it does for a 5xx, so it gets its own words.
  const badLocation = error instanceof ApiError && error.status === 422;
  const firstPageFailed = error && !data;

  return (
    <main className="page">
      <h1>{t.title}</h1>
      <p className="muted">{t.subtitle}</p>

      {point && !precise && <p className="warn">{t.fallback}</p>}

      {!point && <p className="muted" role="status">{t.locating}</p>}
      {point && isPending && !error && <FeedSkeleton label={t.loadingFeed} />}

      {firstPageFailed &&
        (badLocation ? (
          <div className="ms-empty" role="alert">
            <p>{t.badLocation}</p>
            <button
              type="button"
              className="ms-btn"
              disabled={isRefetching}
              onClick={() => void refetch()}
            >
              {loc === "ar" ? "حاول مرة أخرى" : "Try again"}
            </button>
          </div>
        ) : (
          <LoadError error={error} onRetry={() => void refetch()} locale={loc} />
        ))}

      {point && !isPending && !error && items.length === 0 && (
        <p className="muted">{t.empty}</p>
      )}

      <ul className="stack">
        {items.map((it) => (
          <Card key={it.id} item={it} t={t} locale={loc} />
        ))}
      </ul>

      {items.length > 0 && hasNextPage && (
        <div className="row">
          <button
            type="button"
            className="ms-btn"
            disabled={isFetchingNextPage}
            onClick={() => void fetchNextPage()}
          >
            {isFetchingNextPage ? t.moreLoading : t.more}
          </button>
          {isFetchNextPageError && (
            <span className="bad" role="status">
              {t.moreFailed}
            </span>
          )}
        </div>
      )}

      {/* A feed that *can* be exhausted is the product promise, not a failure
          state — finite and locally relevant beats infinite and irrelevant. So
          the end of the list says so rather than spinning. */}
      {items.length > 0 && !hasNextPage && !firstPageFailed && (
        <p className="muted">{t.exhausted}</p>
      )}
    </main>
  );
}
