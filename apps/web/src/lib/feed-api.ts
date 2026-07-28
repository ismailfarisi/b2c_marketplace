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
