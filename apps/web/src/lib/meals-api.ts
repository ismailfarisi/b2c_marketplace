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
  /** Staff checked a current food-safety licence. */
  food_safety_verified: boolean;
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
  cuisine?: string | undefined;
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
      cuisine: f.cuisine || undefined,
      rotation: f.rotation ? "true" : undefined,
    },
  });

export const getMealKitchen = (branchId: string, point?: Point | null) =>
  apiFetch<PublicKitchenDetail>({
    url: `/meals/kitchens/${branchId}`,
    method: "GET",
    params: { lat: point?.lat, lng: point?.lng },
  });

export type SectionType =
  | "banner_carousel"
  | "kitchen_rail"
  | "cuisine_strip"
  | "rotation_promo"
  | "all_kitchens";

export interface SectionSettings {
  title?: string;
  title_ar?: string;
  collection_id?: string;
  banner_ids?: string[];
}

export interface Section {
  id: string;
  type: SectionType;
  enabled: boolean;
  settings: SectionSettings;
}

export interface CollectionRules {
  cuisines?: string[];
  dietary?: string[];
  slots?: string[];
  rotation_only?: boolean;
  max_price_minor?: number;
}

export interface Collection {
  id: string;
  slug: string;
  title: string;
  title_ar: string | null;
  subtitle: string | null;
  subtitle_ar: string | null;
  rules: CollectionRules;
  sort: "rating" | "price_asc" | "price_desc" | "newest";
  max_items: number | null;
  starts_at: string | null;
  ends_at: string | null;
  is_active: boolean;
  items:
    { branch_id: string; role: "pin" | "exclude"; position: number }[] | null;
  updated_at: string;
}

export interface Banner {
  id: string;
  title: string;
  title_ar: string | null;
  body: string | null;
  body_ar: string | null;
  cta: string | null;
  cta_ar: string | null;
  image_url: string | null;
  link_url: string | null;
  starts_at: string | null;
  ends_at: string | null;
  is_active: boolean;
  updated_at: string;
}

export interface HomeSection extends Section {
  collection?: Collection;
  kitchens?: PublicKitchen[];
  banners?: Banner[];
  cuisines?: { cuisine: string; kitchens: number }[];
}

/** `sections` is null when nothing is published: render the default home. */
export interface MealsHome {
  version_id: string | null;
  sections: HomeSection[] | null;
}

export const getMealsHome = (point?: Point | null) =>
  apiFetch<MealsHome>({
    url: "/meals/home",
    method: "GET",
    params: { lat: point?.lat, lng: point?.lng },
  });

export const getMealCollection = (slug: string, point?: Point | null) =>
  apiFetch<Collection & { kitchens: PublicKitchen[] }>({
    url: `/meals/collections/${slug}`,
    method: "GET",
    params: { lat: point?.lat, lng: point?.lng },
  });

/** Dubai Marina, the same fallback the feed and the mobile app use. */
export const FALLBACK_POINT: Point = { lat: 25.08, lng: 55.14 };

/** A public promo or trial, as a customer sees it. */
export interface Offer {
  id: string;
  code?: string | null;
  name: string;
  name_ar: string;
  description: string;
  description_ar: string;
  discount_type: "percent" | "fixed" | "none";
  percent_bp: number;
  amount_minor: number;
  max_discount_minor: number | null;
  currency: string;
  trial_days: number | null;
  applies_to: "subscriptions" | "rotations" | "both";
  audience: "everyone" | "new_customers";
  ends_at: string | null;
}

/** Public promos and trials usable at a kitchen right now. */
export const getKitchenOffers = (branchId: string) =>
  apiFetch<{ items: Offer[] }>({
    url: `/meals/kitchens/${branchId}/offers`,
    method: "GET",
  });

/** A published review from a verified subscriber. */
export interface KitchenReview {
  id: string;
  branch_id: string;
  rating: number;
  comment: string;
  reply: string | null;
  replied_at: string | null;
  created_at: string;
  updated_at: string;
}

export const getKitchenReviews = (branchId: string) =>
  apiFetch<{ items: KitchenReview[] }>({
    url: `/meals/kitchens/${branchId}/reviews`,
    method: "GET",
  });
