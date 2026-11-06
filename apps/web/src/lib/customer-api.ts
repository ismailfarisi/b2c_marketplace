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

/** Deletes the signed-in account; the caller clears the local session after. */
export const deleteAccount = () =>
  apiFetch<void>({ url: "/me", method: "DELETE" });

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

/**
 * Per-call options for mutating calls. Pass `headers` from
 * `useIdempotencyKey()` so a retried draft reuses its key instead of the
 * fresh one apiFetch would otherwise mint per call.
 */
export interface CallOptions {
  headers?: Record<string, string>;
}

export const subscribe = (data: SubscribeInput, opts?: CallOptions) =>
  apiFetch<Subscribed>({
    url: "/subscriptions",
    method: "POST",
    data,
    headers: opts?.headers,
  });

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
  company_name: string | null;
  guest_week?: GuestWeekLink | null;
  guest_of?: GuestWeekLink | null;
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
  delivery_status?: string | null;
  skipped?: boolean;
}

export interface MyDelivery {
  date: string;
  meal_slot: string;
  status: string;
  at: string | null;
}

/** Structured answer to "why are you leaving?"; the free-text reason stays as a note. */
export type CancellationReasonCode =
  | "bored_of_food"
  | "too_expensive"
  | "food_quality"
  | "delivery_problems"
  | "moving_away"
  | "other";

export interface CancelBody {
  reason_code?: CancellationReasonCode;
  reason?: string;
}

/**
 * The other half of a guest week. On the original subscription `kitchen_name`
 * is the guest kitchen; on the guest subscription it is the original kitchen.
 */
export interface GuestWeekLink {
  subscription_id: string;
  guest_subscription_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  week_start: string;
  week_end: string;
  status: string;
  pending_order_id: string | null;
  completed: boolean;
}

export interface GuestWeekOption {
  plan_id: string;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  plan_name: string;
  plan_name_ar: string | null;
  cuisines: string[];
  per_meal_minor: number;
  week_minor: number;
  wallet_minor: number;
  due_minor: number;
  leftover_credit_minor: number;
  currency: string;
}

export interface GuestWeekOffer {
  eligible: boolean;
  reason?: string;
  week_start?: string;
  week_end?: string;
  delivery_days: number;
  meals: number;
  credit_minor: number;
  current_per_meal_minor: number;
  currency: string;
  options: GuestWeekOption[];
}

export interface GuestWeekResult {
  guest_subscription_id: string;
  status: "active" | "pending_payment";
  pending_order_id: string | null;
  week_start: string;
  week_end: string;
  credit_minor: number;
  wallet_minor: number;
  due_minor: number;
  leftover_credit_minor: number;
  currency: string;
}

export interface CheaperPlan {
  plan_id: string;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  plan_name: string;
  plan_name_ar: string | null;
  cuisines: string[];
  per_meal_minor: number;
  currency: string;
}

export interface CheaperPlans {
  current_per_meal_minor: number;
  currency: string;
  items: CheaperPlan[];
}

export interface MySubscriptionDetail extends MySubscription {
  cutoff_hours: number;
  pause_allowance_days: number;
  pause_days_used: number;
  guest_week_offer?: GuestWeekOffer | null;
  upcoming: UpcomingMeal[];
  skipped?: UpcomingMeal[];
  recent?: MyDelivery[];
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

export const unskipMeal = (id: string, date: string, slot: string) =>
  apiFetch<unknown>({
    url: `/subscriptions/${id}/unskip`,
    method: "POST",
    data: { date, meal_slot: slot },
  });

export const resumeSubscription = (id: string, opts?: CallOptions) =>
  apiFetch<{ resumes_on: string; days_returned: number }>({
    url: `/subscriptions/${id}/resume`,
    method: "POST",
    data: {},
    headers: opts?.headers,
  });

export const cancelSubscription = (id: string, body: CancelBody, opts?: CallOptions) =>
  apiFetch<unknown>({
    url: `/subscriptions/${id}/cancel`,
    method: "POST",
    data: body,
    headers: opts?.headers,
  });

export const getGuestWeekOffer = (id: string) =>
  apiFetch<GuestWeekOffer>({ url: `/me/subscriptions/${id}/guest-week`, method: "GET" });

export const startGuestWeek = (id: string, planId: string, opts?: CallOptions) =>
  apiFetch<GuestWeekResult>({
    url: `/subscriptions/${id}/guest-week`,
    method: "POST",
    data: { plan_id: planId },
    headers: opts?.headers,
  });

export const dismissGuestWeek = (id: string) =>
  apiFetch<void>({ url: `/subscriptions/${id}/guest-week/dismiss`, method: "POST", data: {} });

export const getCheaperPlans = (id: string) =>
  apiFetch<CheaperPlans>({ url: `/me/subscriptions/${id}/cheaper-plans`, method: "GET" });

export const getRotationCheaperPlans = (id: string) =>
  apiFetch<CheaperPlans>({ url: `/me/rotations/${id}/cheaper-plans`, method: "GET" });

/** Automatic renewal on or off; off ends with the last paid cycle. */
export const setRenewal = (id: string, auto: boolean) =>
  apiFetch<void>({
    url: `/subscriptions/${id}/renewal`,
    method: "PUT",
    data: { auto },
  });

/** A new subscription like this one, pending payment of its order_id. */
export const renewSubscription = (id: string, opts?: CallOptions) =>
  apiFetch<Subscribed>({
    url: `/subscriptions/${id}/renew`,
    method: "POST",
    data: {},
    headers: opts?.headers,
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
  /** The customer can still change this week's kitchen; see swapRotationWeek. */
  swappable?: boolean;
  swapped?: boolean;
  swap_until?: string | null;
}

export interface RotationQuote {
  start_date: string;
  end_date: string;
  weeks: RotationWeek[];
  amount_minor: number;
  discount_minor: number;
  promo?: Offer;
  currency: string;
  /** Advisory only; never blocks buying the rotation. */
  variety?: RotationVariety;
}

export interface RotationVarietyWarning {
  code: "same_cuisine" | "repeated_dish" | "repeated_across_weeks";
  cuisines?: string[];
  dish?: string;
  dish_ar?: string | null;
  /** repeated_dish only; 0 = Sunday. */
  weekday?: number;
  kitchens: number;
}

export interface RotationVariety {
  level: "low" | "good" | "great";
  score: number;
  distinct_cuisines: number;
  warnings: RotationVarietyWarning[];
}

export interface RotationSuggestionInput {
  address_id: string;
  start_date?: string;
  delivery_weekdays?: number[];
  meal_slots?: string[];
  promo_code?: string;
}

export interface RotationSuggestion {
  plan_ids: string[];
  branch_ids: string[];
  start_date: string;
  delivery_weekdays: number[];
  meal_slots: string[];
  quote: RotationQuote;
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
  /** Renewals propose fresh kitchens instead of the same four. */
  refresh_on_renew: boolean;
}

/** Which kitchen the platform would give each week, and the total. */
export const previewRotation = (data: RotationInput) =>
  apiFetch<RotationQuote>({ url: "/rotations/preview", method: "POST", data });

/** Four balanced kitchens for the address, quoted like preview. Creates nothing. */
export const suggestRotation = (data: RotationSuggestionInput) =>
  apiFetch<RotationSuggestion>({ url: "/rotations/suggestion", method: "POST", data });

export const createRotation = (data: RotationInput, opts?: CallOptions) =>
  apiFetch<Rotation>({
    url: "/rotations",
    method: "POST",
    data,
    headers: opts?.headers,
  });

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

export const cancelRotation = (id: string, body: CancelBody, opts?: CallOptions) =>
  apiFetch<void>({
    url: `/rotations/${id}/cancel`,
    method: "POST",
    data: body,
    headers: opts?.headers,
  });

export interface RotationSwapOption {
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  cuisines: string[];
  cover_image_url: string | null;
  rating: number | null;
  rating_count: number;
  plan_id: string;
  plan_name: string;
  price_minor: number;
  /** price_minor less the week's current price: positive is charged, negative credited. */
  difference_minor: number;
  currency: string;
}

export interface RotationSwapOptions {
  week: number;
  current_price_minor: number;
  swap_until: string;
  currency: string;
  items: RotationSwapOption[];
}

export interface RotationSwapResult {
  rotation: Rotation;
  due_minor: number;
  currency: string;
  /** Collects due_minor when the new kitchen costs more. */
  payment: PaymentIntent | null;
}

/** Kitchens one future rotation week could change to, best rated first. */
export const listRotationSwapOptions = (id: string, week: number) =>
  apiFetch<RotationSwapOptions>({ url: `/rotations/${id}/weeks/${week}/options`, method: "GET" });

/** Changes one future week's kitchen; a dearer kitchen's difference is paid on the spot. */
export const swapRotationWeek = (id: string, week: number, planId: string, opts?: CallOptions) =>
  apiFetch<RotationSwapResult>({
    url: `/rotations/${id}/weeks/${week}/swap`,
    method: "POST",
    data: { plan_id: planId },
    headers: opts?.headers,
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

export const setRotationRenewal = (
  id: string,
  data: { auto?: boolean; refresh_on_renew?: boolean },
) =>
  apiFetch<void>({
    url: `/rotations/${id}/renewal`,
    method: "PUT",
    data,
  });

/**
 * The next rotation, pending payment: the four plans given (an edited
 * proposal) or, without them, the same four kitchens.
 */
export const renewRotation = (
  id: string,
  opts?: CallOptions & { planIds?: string[] },
) =>
  apiFetch<Rotation>({
    url: `/rotations/${id}/renew`,
    method: "POST",
    data: opts?.planIds ? { plan_ids: opts.planIds } : {},
    headers: opts?.headers,
  });

// ── Variety at renewal: proposal, taste memory, adventure setting ──────────

export type ProposalReason =
  | ""
  | "loved"
  | "top_rated"
  | "kept"
  | "new_cuisine"
  | "familiar_cuisine"
  | "new_kitchen";

export interface ProposalKitchen {
  branch_id: string;
  plan_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  cuisines: string[];
  rating: number | null;
  rating_count: number;
  week_price_minor: number;
  meal_price_minor: number;
  currency: string;
  in_current: boolean;
  favourite: boolean;
  own_rating: number;
  reason: ProposalReason;
  reason_cuisine?: string;
}

export type Adventure = "familiar" | "balanced" | "surprise";

export interface RenewalProposal {
  rotation_id: string;
  start_date: string;
  adventure: Adventure;
  refresh_on_renew: boolean;
  kitchens: ProposalKitchen[];
  eligible: ProposalKitchen[];
  amount_minor: number;
  previous_amount_minor: number;
  currency: string;
  meals_per_week: number;
}

export const getRenewalProposal = (id: string) =>
  apiFetch<RenewalProposal>({
    url: `/me/rotations/${id}/renewal-proposal`,
    method: "GET",
  });

export type TasteKind = "kitchen" | "dish";
export type Sentiment = "favourite" | "avoid";

export interface Taste {
  subject_kind: TasteKind;
  subject_id: string;
  sentiment: Sentiment;
  updated_at: string;
}

export const listMyTastes = () =>
  apiFetch<{ items: Taste[] }>({ url: "/me/tastes", method: "GET" });

export const setMyTaste = (data: {
  subject_kind: TasteKind;
  subject_id: string;
  sentiment: Sentiment;
}) => apiFetch<Taste>({ url: "/me/tastes", method: "PUT", data });

export const clearMyTaste = (kind: TasteKind, subjectId: string) =>
  apiFetch<void>({ url: `/me/tastes/${kind}/${subjectId}`, method: "DELETE" });

export interface MealPreferences {
  adventure: Adventure;
}

export const getMealPreferences = () =>
  apiFetch<MealPreferences>({ url: "/me/meal-preferences", method: "GET" });

export const setMealPreferences = (data: MealPreferences) =>
  apiFetch<MealPreferences>({ url: "/me/meal-preferences", method: "PUT", data });

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

// --- Office lunch accounts -------------------------------------------------

export interface CompanyPolicy {
  weekdays: number[];
  slots: string[];
  meal_cap_minor: number;
}

export interface CompanyOffice {
  id: string;
  label: string;
  building: string;
  area: string;
  city: string;
  lat: number;
  lng: number;
}

export interface CompanyBenefit {
  company_id: string;
  name: string;
  role: "admin" | "employee";
  status: "pending" | "active" | "suspended" | "rejected";
  policy: CompanyPolicy;
  offices: CompanyOffice[];
  currency: string;
}

export interface Company {
  id: string;
  name: string;
  legal_name: string;
  trn: string;
  billing_email: string;
  billing_phone: string;
  status: CompanyBenefit["status"];
  currency: string;
  credit_limit_minor: number;
  payment_terms_days: number;
  policy: CompanyPolicy;
  review_note: string;
  reviewed_at: string | null;
  created_at: string;
}

export interface CompanyMember {
  id: string;
  phone: string;
  name: string;
  role: "admin" | "employee";
  joined: boolean;
  joined_at: string | null;
  lunches: number;
}

export interface CompanyCredit {
  limit_minor: number;
  unbilled_minor: number;
  outstanding_minor: number;
  available_minor: number;
  month_spend_minor: number;
  overdue_statements: number;
}

export interface CompanyCharge {
  id: string;
  kind: "charge" | "credit";
  subscription_id: string;
  subscription_no: string;
  employee: string;
  kitchen: string;
  amount_minor: number;
  currency: string;
  adjustment: boolean;
  created_at: string;
}

export interface CompanyStatement {
  id: string;
  statement_no: string;
  period_end: string;
  total_minor: number;
  adjustment_minor: number;
  amount_due_minor: number;
  currency: string;
  status: "issued" | "paid" | "void";
  issued_at: string;
  due_on: string;
  paid_at: string | null;
  paid_reference: string;
  lines?: CompanyCharge[];
}

export interface CompanyLunch {
  subscription_id: string;
  subscription_no: string;
  employee: string;
  phone: string;
  kitchen: string;
  plan: string;
  status: string;
  start_date: string;
  renewal_unpaid?: boolean;
}

export interface CompanyDashboard {
  company: Company;
  role: "admin" | "employee" | "staff";
  credit: CompanyCredit;
  members: CompanyMember[] | null;
  offices: CompanyOffice[];
  lunches: CompanyLunch[] | null;
  statements: CompanyStatement[] | null;
  unbilled: CompanyCharge[] | null;
}

export interface OfficeInput {
  label: string;
  building: string;
  area: string;
  city: string;
  lat: number;
  lng: number;
}

export interface RegisterCompanyInput {
  name: string;
  legal_name: string;
  trn: string;
  billing_email: string;
  billing_phone: string;
  office: OfficeInput;
}

export interface LunchInput {
  office_id: string;
  plan_id: string;
  tier_id?: string;
  start_date: string;
  schedule: ScheduleRule[];
}

export const listMyCompanies = () =>
  apiFetch<{ items: CompanyBenefit[] }>({
    url: "/me/companies",
    method: "GET",
  });
export const registerCompany = (data: RegisterCompanyInput) =>
  apiFetch<Company>({ url: "/companies", method: "POST", data });
export const getCompany = (id: string) =>
  apiFetch<CompanyDashboard>({ url: `/companies/${id}`, method: "GET" });
export const getCompanyStatement = (id: string, statementId: string) =>
  apiFetch<CompanyStatement>({
    url: `/companies/${id}/statements/${statementId}`,
    method: "GET",
  });
export const addCompanyMember = (
  id: string,
  data: { phone: string; name: string; role: "admin" | "employee" },
) => apiFetch<void>({ url: `/companies/${id}/members`, method: "POST", data });
export const removeCompanyMember = (id: string, memberId: string) =>
  apiFetch<void>({
    url: `/companies/${id}/members/${memberId}`,
    method: "DELETE",
  });
export const setCompanyPolicy = (id: string, data: CompanyPolicy) =>
  apiFetch<void>({ url: `/companies/${id}/policy`, method: "PUT", data });
export const addCompanyOffice = (id: string, data: OfficeInput) =>
  apiFetch<void>({ url: `/companies/${id}/offices`, method: "POST", data });
export const removeCompanyOffice = (id: string, officeId: string) =>
  apiFetch<void>({
    url: `/companies/${id}/offices/${officeId}`,
    method: "DELETE",
  });
export const previewLunch = (companyId: string, data: LunchInput) =>
  apiFetch<{ preview: SubscriptionPreview; per_meal_minor: number }>({
    url: `/companies/${companyId}/lunches/preview`,
    method: "POST",
    data,
  });
export const startLunch = (
  companyId: string,
  data: LunchInput,
  opts?: CallOptions,
) =>
  apiFetch<{ id: string; subscription_no: string; status: string }>({
    url: `/companies/${companyId}/lunches`,
    method: "POST",
    data,
    headers: opts?.headers,
  });

// --- support ----------------------------------------------------------------

export type SupportTicket = {
  id: string;
  subject: string;
  status: "open" | "answered" | "closed";
  subscription_id: string | null;
  created_at: string;
  updated_at: string;
};

export type SupportMessage = {
  id: string;
  from_staff: boolean;
  body: string;
  created_at: string;
};

export type SupportThread = SupportTicket & { messages: SupportMessage[] };

export const listMyTickets = (cursor?: string) =>
  apiFetch<{ items: SupportTicket[]; next_cursor?: string | null }>({
    url: "/me/support/tickets",
    method: "GET",
    params: cursor ? { cursor } : undefined,
  });

export const openTicket = (
  body: { subject: string; body: string; subscription_id?: string },
  opts?: CallOptions,
) =>
  apiFetch<SupportThread>({
    url: "/me/support/tickets",
    method: "POST",
    data: body,
    headers: opts?.headers,
  });

export const getMyTicket = (id: string) =>
  apiFetch<SupportThread>({ url: `/me/support/tickets/${id}`, method: "GET" });

export const replyToTicket = (id: string, body: string, opts?: CallOptions) =>
  apiFetch<SupportThread>({
    url: `/me/support/tickets/${id}/messages`,
    method: "POST",
    data: { body },
    headers: opts?.headers,
  });

// --- invoices ---------------------------------------------------------------

export type InvoiceSummary = {
  id: string;
  number: string;
  kind: "tax_invoice" | "simplified" | "credit_note";
  total: { amount: number; currency: string };
  tax: { amount: number; currency: string };
  supply_date: string;
  issued_at: string;
  status: "issued" | "cancelled" | "superseded";
};

export type InvoiceDocument = {
  id: string;
  number: string;
  kind: "tax_invoice" | "simplified" | "credit_note";
  status: "issued" | "cancelled" | "superseded";
  country: string;
  currency: string;
  supplier: { name: string; trn: string; address: string };
  buyer: { name: string; trn: string; address: string };
  supply_date: string;
  issued_at: string;
  corrects_number: string | null;
  net_minor: number;
  tax_minor: number;
  gross_minor: number;
  lines: Array<{
    description: string;
    description_ar: string;
    quantity: number;
    unit_net_minor: number;
    net_minor: number;
    tax_rate_bp: number;
    tax_minor: number;
    gross_minor: number;
  }>;
};

export const listMyInvoices = (cursor?: string) =>
  apiFetch<{ items: InvoiceSummary[]; next_cursor?: string | null }>({
    url: "/me/invoices",
    method: "GET",
    params: cursor ? { cursor } : undefined,
  });

export const getMyInvoice = (id: string) =>
  apiFetch<InvoiceDocument>({ url: `/me/invoices/${id}`, method: "GET" });

// Next-week preview and end-of-cycle recap (anti-boredom).
export interface PreviewDish {
  id: string;
  name: string;
  name_ar: string | null;
  image_url: string | null;
  is_default: boolean;
  chosen: boolean;
}

export interface PreviewMeal {
  date: string;
  weekday: number;
  meal_slot: string;
  dishes: PreviewDish[];
}

export interface NextWeekPreview {
  subscription_id: string;
  rotation_id: string | null;
  rotation_week: number | null;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  cover_image_url: string | null;
  cuisines: string[];
  kitchen_description: string | null;
  kitchen_description_ar: string | null;
  starts_on: string;
  ends_on: string;
  menu_week: number;
  meals: PreviewMeal[];
}

export interface RecapKitchen {
  week: number;
  branch_id: string;
  kitchen_name: string;
  kitchen_name_ar: string | null;
  cover_image_url: string | null;
  cuisines: string[];
}

export interface RotationRecap {
  rotation_id: string;
  rotation_no: string;
  starts_on: string;
  ends_on: string;
  week4_starts_on: string;
  ended: boolean;
  kitchens: RecapKitchen[];
  kitchen_count: number;
  cuisine_count: number;
  dishes_delivered: number;
  days_skipped: number;
  favourite: {
    branch_id: string;
    kitchen_name: string;
    kitchen_name_ar: string | null;
    rating: number;
  } | null;
  renew_path: string;
}

export const getMyNextWeek = () =>
  apiFetch<{ items: NextWeekPreview[] }>({
    url: "/me/next-week",
    method: "GET",
  });

export const listMyRotationRecaps = () =>
  apiFetch<{ items: RotationRecap[] }>({
    url: "/me/rotation-recaps",
    method: "GET",
  });
