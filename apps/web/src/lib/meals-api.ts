import { apiFetch } from "@marketplace/api-client";

import type { DeliveryWindow, Plan, WeeklyMenu } from "./vendor-api";

/**
 * The meals storefront. Anonymous, like the feed: a kitchen link shared in a
 * group chat must open the kitchen, not a sign-in page.
 */

export interface PublicKitchen {
  branch_id: string;
  vendor_id: string;
  name: string;
  name_ar: string | null;
  vendor_name: string;
  vendor_name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  cuisines: string[];
  dietary_tags: string[];
  cover_image_url: string | null;
  city: string | null;
  rating: number | null;
  rating_count: number;
  currency: string;
  meal_slots: string[];
  plans: number;
  from_price_minor: number;
  rotation_week_price_minor: number | null;
}

export interface PublicKitchenDetail extends PublicKitchen {
  delivers_here: boolean | null;
  delivery_windows: DeliveryWindow[];
  menu: WeeklyMenu;
  plan_list: Plan[];
}

export interface Point {
  lat: number;
  lng: number;
}

export interface KitchenQuery {
  point?: Point | null | undefined;
  q?: string | undefined;
  slot?: string | undefined;
  dietary?: string | undefined;
  rotation?: boolean | undefined;
}

export const getMealKitchens = (f: KitchenQuery) =>
  apiFetch<{ items: PublicKitchen[] }>({
    url: "/meals/kitchens",
    method: "GET",
    params: {
      lat: f.point?.lat,
      lng: f.point?.lng,
      q: f.q || undefined,
      slot: f.slot || undefined,
      dietary: f.dietary || undefined,
      rotation: f.rotation ? "true" : undefined,
    },
  });

export const getMealKitchen = (branchId: string, point?: Point | null) =>
  apiFetch<PublicKitchenDetail>({
    url: `/meals/kitchens/${branchId}`,
    method: "GET",
    params: { lat: point?.lat, lng: point?.lng },
  });

/** Dubai Marina, the same fallback the feed and the mobile app use. */
export const FALLBACK_POINT: Point = { lat: 25.08, lng: 55.14 };
