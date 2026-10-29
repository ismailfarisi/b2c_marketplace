import { apiFetch } from "@marketplace/api-client";

import type { KitchenReview, Offer } from "./meals-api";

/**
 * The signed-in customer: saved addresses, subscribing and paying, and managing
 * a meal subscription. Every call is scoped to the token's user on the server;
 * someone else's subscription or address answers 404.
 */

export interface Address {
  id: string;
  label: string;
  recipient_name: string | null;
  recipient_phone: string | null;
  building: string | null;
  flat: string | null;
  street: string | null;
  landmark: string | null;
  area: string | null;
  city: string;
  emirate: string | null;
  notes: string | null;
  country: string;
  point: { lat: number; lng: number };
  is_default: boolean;
  created_at: string;
}

export interface AddressInput {
  label: string;
  recipient_name?: string;
  recipient_phone?: string;
  building?: string;
  flat?: string;
  street?: string;
  area?: string;
  city: string;
  emirate?: string;
  notes?: string;
  country?: string;
  point: { lat: number; lng: number };
  is_default?: boolean;
}

export const describeAddress = (a: Address) =>
  [a.flat, a.building, a.street, a.area, a.city].filter(Boolean).join(", ");

export const listAddresses = () =>
  apiFetch<{ items: Address[] }>({ url: "/me/addresses", method: "GET" });

export const createAddress = (data: AddressInput) =>
  apiFetch<Address>({ url: "/me/addresses", method: "POST", data });

export const deleteAddress = (id: string) =>
  apiFetch<void>({ url: `/me/addresses/${id}`, method: "DELETE" });

export interface ScheduleRule {
  weekday: number;
  meal_slot: string;
  window_id: string;
}

export interface SubscribeInput {
  plan_id: string;
  tier_id?: string;
  address_id: string;
  start_date: string;
  schedule: ScheduleRule[];
  promo_code?: string;
  promo_id?: string;
}

export interface Subscribed {
  id: string;
  subscription_no: string;
  status: string;
  start_date: string;
  order_id: string;
  amount_minor: number;
  discount_minor: number;
  trial_days: number | null;
  currency: string;
}

export interface SubscriptionPreview {
  subtotal_minor: number;
  discount_minor: number;
  delivery_minor: number;
  total_minor: number;
  currency: string;
  starts_on: string;
  ends_on: string;
  deliverable_days: number;
  renews: boolean;
  promo: Offer | null;
}

/** Prices the first cycle, with any promo, without creating anything. */
export const previewSubscription = (data: SubscribeInput) =>
  apiFetch<SubscriptionPreview>({
    url: "/subscriptions/preview",
    method: "POST",
    data,
  });

export const subscribe = (data: SubscribeInput) =>
  apiFetch<Subscribed>({ url: "/subscriptions", method: "POST", data });

export interface PaymentIntent {
  id: string;
  status: string;
  redirect_url: string | null;
}

/**
 * Pays a billing order. The key is the order id, so a double click or a retry
 * after a dropped response re-reads the same authorization instead of holding
 * the card twice.
 */
export const payOrder = (orderId: string) =>
  apiFetch<PaymentIntent>({
    url: `/orders/${orderId}/payment-intent`,
    method: "POST",
    data: {},
    headers: { "Idempotency-Key": orderId },
  });

export interface MySubscription {
  id: string;
  subscription_no: string;
  status: string;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  plan_name: string;
  plan_name_ar: string | null;
  start_date: string;
  end_date: string | null;
  cycle_no: number;
  cycle_ends_on: string | null;
  amount_minor: number;
  currency: string;
  pending_order_id: string | null;
  rotation_id: string | null;
  rotation_week: number | null;
  renewal_mode: "auto" | "manual" | "none";
  renewed_by_id: string | null;
  can_renew: boolean;
}

export interface MealOption {
  id: string;
  name: string;
  name_ar: string | null;
  allergens: string[];
  calories: number | null;
  is_default: boolean;
}

export interface UpcomingMeal {
  date: string;
  weekday: number;
  meal_slot: string;
  window_label: string;
  options: MealOption[];
  chosen_dish_id: string | null;
  chosen_by: string;
  cutoff_at: string;
  locked: boolean;
}

export interface MySubscriptionDetail extends MySubscription {
  cutoff_hours: number;
  pause_allowance_days: number;
  pause_days_used: number;
  upcoming: UpcomingMeal[];
  cycles: Array<{
    cycle_no: number;
    starts_on: string;
    ends_on: string;
    status: string;
    amount_minor: number;
    currency: string;
  }>;
  pauses: Array<{ from_date: string; to_date: string }>;
}

export const listMySubscriptions = () =>
  apiFetch<{ items: MySubscription[] }>({
    url: "/me/subscriptions",
    method: "GET",
  });

export const getMySubscription = (id: string) =>
  apiFetch<MySubscriptionDetail>({
    url: `/me/subscriptions/${id}`,
    method: "GET",
  });

export const chooseMeal = (
  id: string,
  date: string,
  slot: string,
  dishId: string,
) =>
  apiFetch<UpcomingMeal>({
    url: `/subscriptions/${id}/meals/${date}/${slot}`,
    method: "PUT",
    data: { dish_id: dishId },
  });

export const skipMeal = (id: string, date: string, slot: string) =>
  apiFetch<unknown>({
    url: `/subscriptions/${id}/skip`,
    method: "POST",
    data: { date, meal_slot: slot },
  });

export const pauseSubscription = (id: string, from: string, to: string) =>
  apiFetch<unknown>({
    url: `/subscriptions/${id}/pause`,
    method: "POST",
    data: { from_date: from, to_date: to },
  });

export const cancelSubscription = (id: string, reason: string) =>
  apiFetch<unknown>({
    url: `/subscriptions/${id}/cancel`,
    method: "POST",
    data: { reason },
  });

/** Automatic renewal on or off; off ends with the last paid cycle. */
export const setRenewal = (id: string, auto: boolean) =>
  apiFetch<void>({
    url: `/subscriptions/${id}/renewal`,
    method: "PUT",
    data: { auto },
  });

/** A new subscription like this one, pending payment of its order_id. */
export const renewSubscription = (id: string) =>
  apiFetch<Subscribed>({
    url: `/subscriptions/${id}/renew`,
    method: "POST",
    data: {},
  });

export interface RotationInput {
  plan_ids: string[];
  address_id: string;
  start_date: string;
  delivery_weekdays: number[];
  meal_slots: string[];
  promo_code?: string;
}

export interface RotationWeek {
  week: number;
  starts_on: string;
  ends_on: string;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  plan_id: string;
  plan_name: string;
  price_minor: number;
  subscription_id: string | null;
  subscription_no: string | null;
  status: string | null;
}

export interface RotationQuote {
  start_date: string;
  end_date: string;
  weeks: RotationWeek[];
  amount_minor: number;
  discount_minor: number;
  promo?: Offer;
  currency: string;
}

export interface Rotation extends RotationQuote {
  id: string;
  rotation_no: string;
  status: string;
  meal_slots: string[];
  delivery_weekdays: number[];
  created_at: string;
  auto_renew: boolean;
  renewed_from_id: string | null;
  renewed_by_id: string | null;
  renewed_by_status: string | null;
}

/** Which kitchen the platform would give each week, and the total. */
export const previewRotation = (data: RotationInput) =>
  apiFetch<RotationQuote>({ url: "/rotations/preview", method: "POST", data });

export const createRotation = (data: RotationInput) =>
  apiFetch<Rotation>({ url: "/rotations", method: "POST", data });

/** One payment for all four weeks, keyed on the rotation like payOrder. */
export const payRotation = (id: string) =>
  apiFetch<PaymentIntent>({
    url: `/rotations/${id}/payment-intent`,
    method: "POST",
    data: {},
    headers: { "Idempotency-Key": id },
  });

export const listMyRotations = () =>
  apiFetch<{ items: Rotation[] }>({ url: "/me/rotations", method: "GET" });

export const getMyRotation = (id: string) =>
  apiFetch<Rotation>({ url: `/me/rotations/${id}`, method: "GET" });

export const cancelRotation = (id: string, reason: string) =>
  apiFetch<void>({
    url: `/rotations/${id}/cancel`,
    method: "POST",
    data: { reason },
  });

export interface MyReview {
  can_review: boolean;
  reason?: string;
  review: (KitchenReview & { status: string }) | null;
}

export const getMyReview = (subscriptionId: string) =>
  apiFetch<MyReview>({
    url: `/me/subscriptions/${subscriptionId}/review`,
    method: "GET",
  });

export const writeReview = (
  subscriptionId: string,
  data: { rating: number; comment: string },
) =>
  apiFetch<KitchenReview>({
    url: `/subscriptions/${subscriptionId}/review`,
    method: "PUT",
    data,
  });

export const setRotationRenewal = (id: string, auto: boolean) =>
  apiFetch<void>({
    url: `/rotations/${id}/renewal`,
    method: "PUT",
    data: { auto },
  });

/** The next rotation with the same four kitchens, pending payment. */
export const renewRotation = (id: string) =>
  apiFetch<Rotation>({
    url: `/rotations/${id}/renew`,
    method: "POST",
    data: {},
  });

// ---- referrals ------------------------------------------------------------

export interface ReferralVoucher {
  code: string;
  amount_minor: number;
  currency: string;
  ends_at: string | null;
  used: boolean;
  active: boolean;
}

export interface MyReferrals {
  active: boolean;
  code?: string;
  friend_amount_minor: number;
  reward_amount_minor: number;
  currency: string;
  pending: number;
  rewarded: number;
  vouchers: ReferralVoucher[];
}

export const getMyReferrals = () =>
  apiFetch<MyReferrals>({ url: "/me/referrals", method: "GET" });

const INVITE_KEY = "meals.invite";

/** Remembers an invite code from a shared link until checkout. */
export function rememberInvite(code: string) {
  try {
    window.localStorage.setItem(INVITE_KEY, code.trim().toUpperCase());
  } catch {}
}

export function rememberedInvite(): string {
  try {
    return window.localStorage.getItem(INVITE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function forgetInvite() {
  try {
    window.localStorage.removeItem(INVITE_KEY);
  } catch {}
}
