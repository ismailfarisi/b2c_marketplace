import { apiFetch, exponentFor } from "@marketplace/api-client";

import type { CallOptions } from "@/lib/customer-api";

/**
 * Bindings for the vendor portal's kitchen setup surface.
 *
 * Hand-written for the same reason as `api.ts`: there is no generated client
 * yet. Every type is a copy of a schema in api/openapi.yaml under the
 * `kitchen` tag. Separate from `api.ts` so the customer bundle never carries
 * the portal's types.
 */

export type Role = "owner" | "manager" | "staff";

export interface BranchAccess {
  branch_id: string;
  branch_name: string;
  city: string | null;
  vendor_id: string;
  vendor_name: string;
  vendor_status: string;
  role: Role;
  /** IANA zone of the branch; the portal's "today" is this zone's date. */
  timezone?: string | undefined;
}

/**
 * Today's date (YYYY-MM-DD) in a branch's timezone. The kitchen's day, not the
 * browser's: a dispatcher abroad or a late-night session must not plan the
 * wrong date. Falls back to the market zone when the branch sends none.
 */
export function branchToday(
  timezone?: string | undefined,
  now: Date = new Date(),
): string {
  const fmt = (tz: string) =>
    new Intl.DateTimeFormat("en-CA", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(now);
  try {
    return fmt(timezone || "Asia/Dubai");
  } catch {
    return fmt("Asia/Dubai");
  }
}

export const SLOTS = [
  "breakfast",
  "lunch",
  "dinner",
  "snack",
  "suhoor",
  "iftar",
] as const;
export type Slot = (typeof SLOTS)[number];

/** Weekday 0 is Sunday, as everywhere in the API. */
export const WEEKDAYS = [
  "Sun",
  "Mon",
  "Tue",
  "Wed",
  "Thu",
  "Fri",
  "Sat",
] as const;

export interface Dish {
  id: string;
  branch_id: string;
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  image_url: string | null;
  calories: number | null;
  protein_g: number | null;
  carbs_g: number | null;
  fat_g: number | null;
  allergens: string[];
  tags: string[];
  is_active: boolean;
  menu_slots: number;
}

export interface DishInput {
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  image_url: string | null;
  calories: number | null;
  protein_g: number | null;
  carbs_g: number | null;
  fat_g: number | null;
  allergens: string[];
  tags: string[];
  is_active: boolean;
}

export interface MenuChoice {
  dish_id: string;
  name: string;
  name_ar: string | null;
  allergens: string[];
  calories: number | null;
  image_url: string | null;
  is_active: boolean;
}

export interface MenuSlot {
  /** Week of the menu cycle, 1..weeks. */
  week: number;
  weekday: number;
  slot: Slot;
  default: MenuChoice | null;
  alternative: MenuChoice | null;
}

export interface WeeklyMenu {
  cycle_id: string | null;
  /** Weeks the menu runs before it repeats. */
  weeks: number;
  anchor_date: string | null;
  /** The cycle week the kitchen's today falls in. */
  current_week: number;
  slots: MenuSlot[];
}

/** The most weeks a menu can rotate through in the portal. */
export const MAX_MENU_WEEKS = 4;

export interface Tier {
  id: string;
  key: string;
  name: string;
  name_ar: string | null;
  days_per_week: number | null;
  meals_per_day: number | null;
  portion_size: "small" | "regular" | "large" | null;
  price_minor: number;
  sort_order: number;
  is_active: boolean;
}

export interface TierInput {
  key: string;
  name: string;
  name_ar: string | null;
  days_per_week: number | null;
  meals_per_day: number | null;
  portion_size: "small" | "regular" | "large" | null;
  price_minor: number;
}

export interface Plan {
  id: string;
  branch_id: string;
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  image_url: string | null;
  cycle_days: number;
  days_per_week: number;
  meals_per_day: number;
  meal_slots: Slot[];
  delivery_weekdays: number[];
  allowed_pause_days: number;
  cutoff_hours: number;
  price_minor: number;
  currency: string;
  tax_rate_bp: number;
  rotation_enabled: boolean;
  rotation_week_price_minor: number | null;
  is_active: boolean;
  active_subscribers: number;
  tiers: Tier[];
}

export interface PlanInput {
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  image_url: string | null;
  cycle_days: number;
  days_per_week: number;
  meal_slots: Slot[];
  delivery_weekdays: number[];
  allowed_pause_days: number;
  cutoff_hours: number;
  price_minor: number;
  rotation_enabled: boolean;
  rotation_week_price_minor: number | null;
  is_active: boolean;
  tiers: TierInput[];
}

export interface DeliveryWindow {
  id: string;
  label: string;
  starts_at: string;
  ends_at: string;
}

export interface Profile {
  branch_id: string;
  vendor_id: string;
  vendor_name: string;
  vendor_status: string;
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  cuisines: string[];
  dietary_tags: string[];
  cover_image_url: string | null;
  phone: string | null;
  address_line: string | null;
  city: string | null;
  emirate: string | null;
  timezone: string;
  currency: string;
  is_active: boolean;
  has_service_area: boolean;
  delivery_windows: DeliveryWindow[];
}

export interface ProfileInput {
  name: string;
  name_ar: string | null;
  description: string | null;
  description_ar: string | null;
  cuisines: string[];
  dietary_tags: string[];
  cover_image_url: string | null;
  phone: string | null;
}

export interface Overview {
  active_subscribers: number;
  paused_subscribers: number;
  pending_payment: number;
  active_plans: number;
  rotation_plans: number;
  active_dishes: number;
  menu_gaps: Array<{ weekday: number; slot: Slot; week: number }>;
  profile_complete: boolean;
  menu_weeks: number;
}

// ── calls ───────────────────────────────────────────────────────────────────

export const listMyBranches = () =>
  apiFetch<{ items: BranchAccess[] }>({ url: "/me/branches", method: "GET" });

export const getOverview = (branchId: string) =>
  apiFetch<Overview>({ url: `/branches/${branchId}/overview`, method: "GET" });

export const getProfile = (branchId: string) =>
  apiFetch<Profile>({ url: `/branches/${branchId}/profile`, method: "GET" });

export const updateProfile = (branchId: string, body: ProfileInput) =>
  apiFetch<Profile>({
    url: `/branches/${branchId}/profile`,
    method: "PUT",
    data: body,
  });

export const listDishes = (branchId: string) =>
  apiFetch<{ items: Dish[]; allergens: string[] }>({
    url: `/branches/${branchId}/dishes`,
    method: "GET",
  });

export const createDish = (
  branchId: string,
  body: DishInput,
  opts?: CallOptions,
) =>
  apiFetch<Dish>({
    url: `/branches/${branchId}/dishes`,
    method: "POST",
    data: body,
    headers: opts?.headers,
  });

export const updateDish = (branchId: string, dishId: string, body: DishInput) =>
  apiFetch<Dish>({
    url: `/branches/${branchId}/dishes/${dishId}`,
    method: "PUT",
    data: body,
  });

export const getMenu = (branchId: string) =>
  apiFetch<WeeklyMenu>({ url: `/branches/${branchId}/menu`, method: "GET" });

export const setMenuSlot = (
  branchId: string,
  weekday: number,
  slot: Slot,
  body: {
    week?: number;
    default_dish_id: string | null;
    alternative_dish_id: string | null;
  },
) =>
  apiFetch<WeeklyMenu>({
    url: `/branches/${branchId}/menu/${weekday}/${slot}`,
    method: "PUT",
    data: body,
  });

export const setMenuWeeks = (branchId: string, weeks: number) =>
  apiFetch<WeeklyMenu>({
    url: `/branches/${branchId}/menu/weeks`,
    method: "PUT",
    data: { weeks },
  });

export const copyMenuWeek = (
  branchId: string,
  week: number,
  fromWeek: number,
  opts?: { headers?: HeadersInit },
) =>
  apiFetch<WeeklyMenu>({
    url: `/branches/${branchId}/menu/weeks/${week}/copy`,
    method: "POST",
    data: { from_week: fromWeek },
    headers: opts?.headers,
  });

export const listPlans = (branchId: string) =>
  apiFetch<{ items: Plan[] }>({
    url: `/branches/${branchId}/plans`,
    method: "GET",
  });

export const createPlan = (
  branchId: string,
  body: PlanInput,
  opts?: CallOptions,
) =>
  apiFetch<Plan>({
    url: `/branches/${branchId}/plans`,
    method: "POST",
    data: body,
    headers: opts?.headers,
  });

export const updatePlan = (branchId: string, planId: string, body: PlanInput) =>
  apiFetch<Plan>({
    url: `/branches/${branchId}/plans/${planId}`,
    method: "PUT",
    data: body,
  });

// ── money input ─────────────────────────────────────────────────────────────

/**
 * Parses what a person typed as a price ("1,200.50") into minor units, or null
 * when it is not a price. String arithmetic rather than `* 100`, because
 * `1.1 * 100` is 110.00000000000001 and a plan priced one fils off is a
 * support ticket.
 */
export function parseMajor(input: string, currency: string): number | null {
  const clean = input.replace(/[,\s]/g, "");
  if (!/^\d+(\.\d+)?$/.test(clean)) return null;
  const exp = exponentFor(currency);
  const [whole = "0", frac = ""] = clean.split(".");
  if (frac.length > exp) return null;
  return Number(whole) * 10 ** exp + Number(frac.padEnd(exp, "0") || "0");
}

/** The inverse of `parseMajor`, for pre-filling an input. */
export function toMajorInput(minor: number | null, currency: string): string {
  if (minor == null) return "";
  const exp = exponentFor(currency);
  const s = String(minor).padStart(exp + 1, "0");
  return `${s.slice(0, -exp)}.${s.slice(-exp)}`;
}

export interface PrepLine {
  meal_slot: string;
  dish_id: string | null;
  name: string;
  name_ar: string | null;
  portions: number;
  chosen_by_customer: number;
}

/** Portions per dish for one day: each subscriber's pick, or the day's default. */
export const getPrepSheet = (branchId: string, date: string) =>
  apiFetch<{ date: string; items: PrepLine[] }>({
    url: `/branches/${branchId}/prep`,
    method: "GET",
    params: { date },
  });

export interface BranchReview {
  id: string;
  branch_id: string;
  rating: number;
  comment: string;
  status: "published" | "hidden";
  hidden_reason?: string | null;
  reply: string | null;
  replied_at: string | null;
  created_at: string;
}

export const listBranchReviews = (branchId: string) =>
  apiFetch<{ items: BranchReview[] }>({
    url: `/branches/${branchId}/reviews`,
    method: "GET",
  });

export const replyToReview = (
  branchId: string,
  reviewId: string,
  reply: string,
  opts?: CallOptions,
) =>
  apiFetch<BranchReview>({
    url: `/branches/${branchId}/reviews/${reviewId}/reply`,
    method: "PUT",
    data: { reply },
    headers: opts?.headers,
  });
