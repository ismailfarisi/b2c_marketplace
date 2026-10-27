import { apiFetch } from "@marketplace/api-client";

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
}

export interface Subscribed {
  id: string;
  subscription_no: string;
  status: string;
  start_date: string;
  order_id: string;
  amount_minor: number;
  currency: string;
}

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
