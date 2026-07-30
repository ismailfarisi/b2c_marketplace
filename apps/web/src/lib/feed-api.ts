import { apiFetch } from "@marketplace/api-client";

/**
 * The public feed, as the web consumes it.
 *
 * Separate from `api.ts` (vendor and driver) and `admin-api.ts` (staff) because
 * this is the only one of the three that is called *without a session*.
 * `GET /feed` carries `security: []` in the contract — anonymous browsing is a
 * supported mode, and putting a login wall in front of the product's front door
 * would be fatal to it.
 *
 * The device id is what makes an anonymous viewer coherent across requests: it
 * namespaces the seen-set, so a page refresh does not serve the same reels, and
 * it namespaces the interest profile that Phase 4's ranking reads. Losing it
 * means every visit is a first visit.
 */

export interface FeedAction {
  id: string;
  kind: string;
  label: string;
  target_type: string;
  target_id: string;
}

/**
 * How a reel's time bar and pill are rendered. Distinguished by what the viewer
 * is being told, not by which fields are set:
 *
 *   countdown  this ends and is then gone      "02:04 left"
 *   window     true between two times          "1h 48m of 3h"
 *   freshness  has been true for a while       "listed 3d ago"
 *   cutoff     act before the next boundary    "order by 16:00"
 *
 * A union rather than `string` so that adding a fifth mode is a compile error at
 * every render site instead of a card that silently draws nothing.
 */
export type TimeMode = "countdown" | "window" | "freshness" | "cutoff";

/**
 * When the thing this reel is about stops being true.
 *
 * Instants only — there is no rendered label and there will not be one. A
 * server-composed "02:04 left" is wrong by the time it is parsed, and the same
 * response is served to an English and an Arabic reader whose digits differ.
 *
 * Render from `mode` alone. A `switch (item.subject_type)` in the overlay is the
 * leak this shape exists to prevent.
 */
export interface TimeSignal {
  mode: TimeMode;
  /** Present for window and freshness; on a countdown it only gives the bar a denominator. */
  starts_at: string | null;
  /** Present for every mode but freshness. On a cutoff it is the *next* occurrence. */
  ends_at: string | null;
  /**
   * The bar's denominator when the interval doesn't supply a useful one — an
   * auction open for nine days needs a bar that moves during the two minutes
   * anyone is watching.
   */
  horizon_seconds: number | null;
  /** IANA name, present on cutoff. Format the wall time in this zone, not the browser's. */
  timezone: string | null;
  /** The boundary recurs: a passed cutoff rolls forward, an expired countdown does not. */
  recurring: boolean;
  /**
   * Server clock at page build. Apply it: `skew = as_of - Date.now()`, computed
   * once per page and added to every countdown on it. A machine four minutes
   * fast shows a live auction as closed, and that reads as a broken product
   * rather than a wrong clock.
   */
  as_of: string;
}

/** The badge over the video and the lines above the CTA, composed by the owning vertical. */
export interface SubjectSummary {
  badge: string;
  badge_ar: string;
  title: string;
  title_ar: string;
  /** Pre-joined, separators included. Splitting and re-joining gets the middot wrong in RTL. */
  line: string;
  line_ar: string;
  /** Minor units. A null currency means no price to show — not the same as free. */
  price_minor: number | null;
  currency: string | null;
  price_suffix: string;
  price_suffix_ar: string;
}

export interface FeedItem {
  id: string;
  author: { type: string; id: string; name: string };
  caption: string;
  duration_ms: number;
  poster_key: string;
  lqip: string;
  renditions: Array<{ label: string; playlist_url: string }>;
  counters: { views: number; likes: number; comments: number; shares: number; saves: number };
  actions: FeedAction[];
  published_at: string;
  explore: boolean;
  sponsored: boolean;
  delivery_id?: string;
  /** Null for a reel with no time dimension. A normal state, not a degraded one. */
  time_signal: TimeSignal | null;
  /** Null when the subject type has no registered provider. */
  summary: SubjectSummary | null;
}

export interface FeedPage {
  items: FeedItem[];
  next_cursor: string | null;
  ranking_profile: string;
}

const DEVICE_KEY = "viewer.device_id";

/**
 * A stable per-browser identifier, in localStorage rather than sessionStorage.
 *
 * The opposite choice from the auth token next door, and deliberately. A token
 * is a credential and dies with the tab; this is a preference key with no
 * authority attached — it cannot be used to read anything — and its whole value
 * is surviving a closed tab, because an interest profile that resets every
 * session never gets good enough to matter.
 */
function deviceId(): string {
  if (typeof window === "undefined") return "";
  let id = window.localStorage.getItem(DEVICE_KEY);
  if (!id) {
    id = crypto.randomUUID();
    window.localStorage.setItem(DEVICE_KEY, id);
  }
  return id;
}

export const getFeed = (opts: {
  lat: number;
  lng: number;
  locale: string;
  cursor?: string | undefined;
}) =>
  apiFetch<FeedPage>({
    url: "/feed",
    method: "GET",
    params: { lat: opts.lat, lng: opts.lng, cursor: opts.cursor },
    headers: {
      "X-Device-Id": deviceId(),
      // The locale the viewer is browsing in. Organic retrieval ignores it —
      // a reel is watchable in either language and filtering would halve an
      // already-thin feed — but sponsored targeting honours it, so an
      // advertiser with Arabic creative can decline English viewers.
      "Accept-Language": opts.locale,
    },
  });

/**
 * Reports what the viewer actually watched.
 *
 * Fire-and-forget on purpose: this is the input to ranking and to boost
 * billing, and neither is worth failing a page render over. The ingest endpoint
 * is idempotent, so a retry is safe and a drop costs one data point.
 */
export const reportEvents = (
  events: Array<{
    type: "impression" | "view" | "skip";
    reel_id: string;
    occurred_at: string;
    watched_ms?: number;
  }>,
) =>
  apiFetch<void>({
    url: "/events",
    method: "POST",
    data: { session_id: deviceId(), events },
    headers: { "X-Device-Id": deviceId() },
  });
