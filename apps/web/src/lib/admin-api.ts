import { apiFetch } from "@marketplace/api-client";

import type { Dish, Overview, Plan, Profile, WeeklyMenu } from "./vendor-api";
import type {
  Banner as MerchBanner,
  Collection as MerchCollection,
  CollectionRules,
  MealsHome,
  PublicKitchen,
  Section as MerchSection,
} from "@/lib/meals-api";

/**
 * Bindings for the staff surface.
 *
 * Split from `api.ts` rather than appended to it, and the reason is not file
 * size. Everything here requires a `platform_admins` row, and keeping it in one
 * module means the admin bundle is one import away from being code-split out of
 * the customer app entirely — which matters because a customer's browser should
 * not be shipped the shape of the moderation queue.
 *
 * Hand-written, like `api.ts`, because `make gen-ts` has never run. Every type
 * is a copy of a schema in api/openapi.yaml and can therefore drift; the
 * mitigation is that it is one short file.
 */

// ── the overview ────────────────────────────────────────────────────────────

export interface QueueDepth {
  queue: string;
  waiting: number;
  oldest_seconds: number;
  /** The promise this queue makes, carried from the server so the client
   *  renders the deadline the system actually keeps rather than one a designer
   *  picked. */
  sla_seconds: number;
  sla_breached: boolean;
}

export const getQueues = () =>
  apiFetch<{ queues: QueueDepth[] }>({
    url: "/moderation/queues",
    method: "GET",
  });

// ── reels ───────────────────────────────────────────────────────────────────

export interface ReelQueueItem {
  reel_id: string;
  author_type: string;
  author_id: string;
  caption: string;
  poster_key: string;
  categories: string[] | null;
  tier: string;
  reports: number;
  entered_at: string;
  waiting_seconds: number;
  sla_breached: boolean;
}

export const getReelQueue = () =>
  apiFetch<{ items: ReelQueueItem[] }>({
    url: "/moderation/queue",
    method: "GET",
  });

export const decideReel = (
  reelId: string,
  decision: "approve" | "reject",
  notes: string,
) =>
  apiFetch<{ status: string }>({
    url: `/moderation/${reelId}/decide`,
    method: "POST",
    data: { decision, notes },
  });

// ── comments ────────────────────────────────────────────────────────────────

export interface CommentQueueItem {
  id: string;
  reel_id: string;
  author_id: string;
  author_name: string;
  body: string;
  state: string;
  reports: number;
  created_at: string;
}

export const getCommentQueue = () =>
  apiFetch<{ items: CommentQueueItem[] }>({
    url: "/moderation/comments",
    method: "GET",
  });

export const decideComment = (
  commentId: string,
  decision: "approve" | "reject",
  notes: string,
) =>
  apiFetch<{ status: string }>({
    url: `/moderation/comments/${commentId}/decide`,
    method: "POST",
    data: { decision, notes },
  });

// ── chat messages ───────────────────────────────────────────────────────────

export interface MessageQueueItem {
  id: string;
  conversation_id: string;
  listing_id: string | null;
  sender_id: string;
  body: string;
  /** What the scanner found — a phone number shape, advance-fee language. The
   *  reviewer decides *with* this, not against it. */
  findings: string[] | null;
  reason: string;
  created_at: string;
}

export const getMessageQueue = () =>
  apiFetch<{ items: MessageQueueItem[] }>({
    url: "/moderation/messages",
    method: "GET",
  });

export const decideMessage = (messageId: string, release: boolean) =>
  apiFetch<{ status: string }>({
    url: `/moderation/messages/${messageId}/decide`,
    method: "POST",
    data: { release },
  });

// ── listings ────────────────────────────────────────────────────────────────

export interface Money {
  amount: number;
  currency: string;
}

export interface ListingQueueItem {
  id: string;
  title: string;
  seller_kind: string;
  seller_id: string;
  seller_name: string;
  make: string;
  model: string;
  year: number;
  price: Money;
  mileage_km: number;
  vin: string | null;
  regional_spec: string;
  city: string | null;
  photo_key: string | null;
  submitted_at: string;
  trust: { score: number; band: string };
}

export const getListingQueue = () =>
  apiFetch<{ items: ListingQueueItem[] }>({
    url: "/moderation/listings",
    method: "GET",
  });

export const decideListing = (
  listingId: string,
  approve: boolean,
  reason: string,
) =>
  apiFetch<{ status: string }>({
    url: `/moderation/listings/${listingId}/decide`,
    method: "POST",
    data: { approve, reason },
  });

export interface FraudSignal {
  subject_type: string;
  subject_id: string;
  kind: string;
  severity: string;
  score: number;
  band: string;
  /** What confirming actually takes off this seller today — severity-scaled
   *  and age-decayed, exactly as the scorer applies it. */
  cost_if_confirmed: number;
  /** Where the seller lands if this is confirmed. The figure a reviewer
   *  reasons with: "does this restrict them" is a question about the total. */
  score_if_confirmed: number;
  created_at: string;
}

export const getFraudQueue = () =>
  apiFetch<{ items: FraudSignal[] }>({
    url: "/moderation/fraud",
    method: "GET",
  });

export const decideFraud = (body: {
  subject_type: string;
  subject_id: string;
  kind: string;
  confirm: boolean;
}) =>
  apiFetch<{ status: string }>({
    url: "/moderation/fraud/decide",
    method: "POST",
    data: body,
  });

// ── appeals ─────────────────────────────────────────────────────────────────

export interface OverdueAppeal {
  id: string;
  subject_type: string;
  subject_id: string;
  appellant_id: string;
  created_at: string;
  due_at: string;
  overdue_by_hours: number;
}

export const getAppealQueue = () =>
  apiFetch<{ items: OverdueAppeal[] }>({
    url: "/moderation/appeals",
    method: "GET",
  });

export const decideAppeal = (
  appealId: string,
  overturn: boolean,
  note: string,
) =>
  apiFetch<void>({
    url: `/moderation/appeals/${appealId}/decide`,
    method: "POST",
    data: { overturn, note },
  });

// ── KYC ─────────────────────────────────────────────────────────────────────

export interface KycDocument {
  id: string;
  kind: string;
  document_ref: string | null;
  number_last4: string | null;
  issued_on: string | null;
  expires_on: string | null;
  status: string;
  /** Computed server-side. Approving a case backed by a licence that expired
   *  last week is the failure this flag exists to make impossible to miss. */
  expired: boolean;
}

export interface KycCase {
  id: string;
  subject_type: string;
  subject_id: string;
  subject_name: string;
  tier: string;
  status: string;
  created_at: string;
  documents: KycDocument[];
}

export const getKycQueue = () =>
  apiFetch<{ items: KycCase[] }>({ url: "/kyc/cases", method: "GET" });

export const decideKyc = (caseId: string, approve: boolean, reason: string) =>
  apiFetch<void>({
    url: `/kyc/cases/${caseId}/decide`,
    method: "POST",
    data: { approve, reason },
  });

export interface PendingPayoutAccount {
  id: string;
  owner_type: string;
  owner_id: string;
  /** The legal name on record and the name on the bank account. Both are
   *  returned uncompared — deciding whether they are the same entity is the
   *  reviewer's entire job, and "Karama Kitchen L.L.C." against "KARAMA KITCHEN
   *  LLC" is not a regex. */
  owner_name: string;
  holder_name: string;
  iban: string;
  bank_name: string | null;
  currency: string;
  created_at: string;
}

export const getPayoutAccountQueue = () =>
  apiFetch<{ items: PendingPayoutAccount[] }>({
    url: "/payout-accounts",
    method: "GET",
  });

export const verifyPayoutAccount = (accountId: string, approve: boolean) =>
  apiFetch<void>({
    url: `/payout-accounts/${accountId}/verify`,
    method: "POST",
    data: { approve },
  });

// ── campaigns ───────────────────────────────────────────────────────────────

export interface PendingCampaign {
  id: string;
  reference: string;
  advertiser_type: string;
  advertiser_name: string;
  reel_id: string;
  caption: string;
  poster_key: string;
  bid_cpm: Money;
  funded: Money;
  daily_cap: Money;
  starts_on: string;
  created_at: string;
  /** Rendered as a sentence server-side. The reviewer's question is "is this
   *  sane", and eight nullable columns is not the shape that answers it. */
  targeting: string;
}

export const getCampaignQueue = () =>
  apiFetch<{ items: PendingCampaign[] }>({
    url: "/moderation/boosts",
    method: "GET",
  });

export const reviewCampaign = (
  campaignId: string,
  approve: boolean,
  note: string,
) =>
  apiFetch<void>({
    url: `/moderation/boosts/${campaignId}/decide`,
    method: "POST",
    data: { approve, note },
  });

// ── disputes ────────────────────────────────────────────────────────────────

export interface Dispute {
  id: string;
  reference_no: string;
  subject_type: string;
  subject_id: string;
  kind: string;
  state: string;
  due_at: string | null;
  assigned_to: string | null;
  created_at: string;
  overdue: boolean;
}

export const getDisputeQueue = () =>
  apiFetch<{ items: Dispute[] }>({ url: "/disputes", method: "GET" });

export const resolveDispute = (
  disputeId: string,
  body: { resolution: string; amount_minor?: number | undefined; note: string },
) =>
  apiFetch<{ status: string }>({
    url: `/disputes/${disputeId}/resolve`,
    method: "POST",
    data: body,
  });

// ── analytics ───────────────────────────────────────────────────────────────

export const getPlatformFunnel = () =>
  apiFetch<Record<string, unknown>>({
    url: "/analytics/feed-funnel",
    method: "GET",
  });

export const getRankingProfile = () =>
  apiFetch<Record<string, unknown>>({ url: "/ranking/profile", method: "GET" });

// ── the console: search, detail, actions ────────────────────────────────────
//
// Everything above is a *queue*: a filtered list answering "what needs doing".
// Everything below is the console proper, answering "what is going on with this
// thing" — a different question that needs a composed document rather than a
// row.
//
// One call per page, not one per panel. An order detail reads twelve tables
// server-side and returns one document; the alternative is a browser making
// twelve requests and assembling a view of the business out of them.

export interface Money {
  amount: number;
  currency: string;
}

export interface Whoami {
  user_id: string;
  is_staff: boolean;
  roles: string[];
  permissions: string[];
}

export const getWhoami = () =>
  apiFetch<Whoami>({ url: "/admin/whoami", method: "GET" });

export interface SearchHit {
  kind: string;
  id: string;
  label: string;
  sub: string;
}

export const adminSearch = (q: string) =>
  apiFetch<{ items: SearchHit[] }>({
    url: `/admin/search?q=${encodeURIComponent(q)}`,
    method: "GET",
  });

/** One thing that happened, from whichever of five history tables recorded it. */
export interface TimelineEntry {
  at: string;
  source: string;
  kind: string;
  detail: string;
  /** Null means the system did it — worth showing, because "who cancelled this"
   *  and "it expired" look identical in a status column and are different
   *  events. */
  actor: string | null;
}

// ── order detail ────────────────────────────────────────────────────────────

export interface OrderDetail {
  id: string;
  order_no: string;
  status: string;
  kind: string;
  service_type: string;
  currency: string;
  placed_at: string;
  notes: string | null;
  customer: { id: string; name: string | null; phone: string | null } | null;
  branch: {
    id: string;
    name: string;
    vendor: string;
    vendor_id: string;
  } | null;
  lines: Array<{ name: string; quantity: number; options: string | null }>;
  quote: {
    state: string;
    tax_inclusive: boolean;
    subtotal: Money;
    delivery: Money;
    surcharge: Money;
    discount: Money;
    tax: Money;
    total: Money;
    lines: Array<{
      kind: string;
      label: string;
      amount: Money;
      tax: Money;
      tax_rate_bp: number;
    }>;
  } | null;
  payments: Array<{
    id: string;
    kind: string;
    status: string;
    provider: string;
    amount: Money;
    created_at: string;
    settled_at: string | null;
    failure_code: string | null;
    three_ds: string | null;
  }>;
  stops: Array<{
    id: string;
    kind: string;
    sequence: number;
    status: string;
    date: string;
    window: string;
    address: string | null;
    recipient: string | null;
    phone: string | null;
    driver_id: string | null;
    driver_name: string | null;
    attempts: number;
    cod_minor: number;
    /** Issued but not sent is the failure that strands a driver at the door, so
     *  the pair is carried rather than one "OTP" flag. */
    otp_issued: boolean;
    otp_sent: boolean;
    proof: string | null;
    completed_at: string | null;
    failed_at: string | null;
    failure_reason: string | null;
  }>;
  notifications: Array<{
    at: string;
    key: string;
    channel: string;
    destination: string | null;
    status: string;
    suppressed: string | null;
  }>;
  invoice: { id: string; number: string; status: string; total: Money } | null;
  attribution: { model: string; reel_id: string | null } | null;
}

export const getOrderDetail = (id: string) =>
  apiFetch<OrderDetail>({ url: `/admin/orders/${id}`, method: "GET" });

export const getOrderTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/orders/${id}/timeline`,
    method: "GET",
  });

export const refundOrder = (
  id: string,
  body: { amount_minor?: number; reason: string },
) =>
  apiFetch<{ refund_id: string; status: string }>({
    url: `/admin/orders/${id}/refund`,
    method: "POST",
    data: body,
  });

export const cancelOrder = (id: string, reason: string) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/orders/${id}/cancel`,
    method: "POST",
    data: { reason },
  });

// ── customer detail ─────────────────────────────────────────────────────────

export interface CustomerDetail {
  id: string;
  display_name: string | null;
  phone: string | null;
  email: string | null;
  status: string;
  country: string;
  locale: string;
  notification_locale: string | null;
  created_at: string;
  blocked_count: number;
  standing: { active_strikes: number; open_appeals: number };
  addresses: Array<{
    id: string;
    label: string | null;
    line: string;
    area: string | null;
    city: string | null;
    recipient: string | null;
    is_default: boolean;
  }>;
  orders: Array<{
    id: string;
    order_no: string;
    status: string;
    branch: string | null;
    total: Money;
    placed_at: string;
  }>;
  subscriptions: Array<{
    id: string;
    status: string;
    plan: string;
    next: string | null;
  }> | null;
  notifications: Array<{
    at: string;
    key: string;
    channel: string;
    destination: string | null;
    status: string;
    suppressed: string | null;
  }>;
}

export const getCustomerDetail = (id: string) =>
  apiFetch<CustomerDetail>({ url: `/admin/users/${id}`, method: "GET" });

export const getCustomerTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/users/${id}/timeline`,
    method: "GET",
  });

// ── vendor detail ───────────────────────────────────────────────────────────

export interface VendorDetail {
  id: string;
  trade_name: string;
  legal_name: string | null;
  slug: string;
  status: string;
  country: string;
  currency: string;
  rating: number | null;
  tax_registration: string | null;
  created_at: string;
  open_disputes: number;
  /** Read from ledger_entries at request time, never a cached column. A payable
   *  that disagrees with the ledger is the one number nobody can afford to have
   *  wrong. */
  payable: Money;
  compliance: { kyc_state: string | null; payout_account: string | null };
  trade_licence: {
    number: string | null;
    expires: string | null;
    expired: boolean;
  } | null;
  reels: {
    published: number;
    pending_review: number;
    last_published: string | null;
  };
  branches: Array<{
    id: string;
    name: string;
    city: string | null;
    is_active: boolean;
    drivers: number;
  }>;
  staff: Array<{
    user_id: string;
    name: string | null;
    phone: string | null;
    role: string;
  }>;
  statements: Array<{
    id: string;
    reference_no: string;
    state: string;
    net: Money;
    period_end: string;
  }> | null;
}

export const getVendorDetail = (id: string) =>
  apiFetch<VendorDetail>({ url: `/admin/vendors/${id}`, method: "GET" });

export const getVendorTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/vendors/${id}/timeline`,
    method: "GET",
  });

// ── staff administration ────────────────────────────────────────────────────

export interface StaffMember {
  user_id: string;
  name: string | null;
  phone: string | null;
  roles: string[];
  expires_at: string | null;
}

export const getStaff = () =>
  apiFetch<{ items: StaffMember[] }>({ url: "/admin/staff", method: "GET" });

export const grantRole = (body: {
  user_id: string;
  role: string;
  reason: string;
  expires_at?: string;
}) =>
  apiFetch<{ status: string }>({
    url: "/admin/staff/grants",
    method: "POST",
    data: body,
  });

export const revokeRole = (userId: string, role: string) =>
  apiFetch<{ status: string }>({
    url: `/admin/staff/${userId}/roles/${role}`,
    method: "DELETE",
  });

/** Who read this record. After a breach the question is who looked, not who
 *  changed — so reads are audited, and audits nobody can read are theatre. */
export const getAccessHistory = (type: string, id: string) =>
  apiFetch<{ items: Array<{ at: string; actor: string; action: string }> }>({
    url: `/admin/access/${type}/${id}`,
    method: "GET",
  });

// ── money (§4.3) ────────────────────────────────────────────────────────────
//
// All of these need `money.read`, which the moderator and support roles do not
// have. A 403 here is the permission model working, not a bug.

export interface AccountBalance {
  account_id: string;
  type: string;
  name: string;
  owner_type: string | null;
  owner_id: string | null;
  debit: Money;
  credit: Money;
  net: Money;
  entries: number;
}

export interface TrialBalance {
  as_of: string;
  currency: string;
  accounts: AccountBalance[];
  total_debit: Money;
  total_credit: Money;
  /** Carried from the server rather than compared client-side. A client that
   *  compares two numbers wrongly reports green on a broken ledger. */
  balanced: boolean;
}

export const getTrialBalance = (params: { from?: string; to?: string } = {}) =>
  apiFetch<TrialBalance>({
    url: `/admin/trial-balance${query(params)}`,
    method: "GET",
  });

export interface JournalLine {
  account_id: string;
  account_type: string;
  account: string;
  direction: "debit" | "credit";
  amount: Money;
}

export interface Journal {
  id: string;
  event_type: string;
  reference_type: string | null;
  reference_id: string | null;
  description: string | null;
  occurred_at: string;
  reverses_id: string | null;
  lines: JournalLine[];
  /** Per journal, so an imbalanced posting is visible at the row that caused it
   *  rather than only in a total nobody scrolls to. */
  balanced: boolean;
}

export const getJournals = (
  params: {
    event_type?: string;
    reference_id?: string;
    from?: string;
    to?: string;
    limit?: number;
  } = {},
) =>
  apiFetch<{ items: Journal[] }>({
    url: `/admin/ledger${query(params)}`,
    method: "GET",
  });

export const getLedgerEventTypes = () =>
  apiFetch<{ items: string[] }>({
    url: "/admin/ledger/event-types",
    method: "GET",
  });

export interface RefundRow {
  id: string;
  order_id: string;
  order_no: string;
  customer: string | null;
  vendor: string | null;
  amount: Money;
  provider: string;
  at: string;
  reason: string | null;
  /** Null means no admin action matched — issued by something other than a
   *  person. That is the row worth asking about, so it is shown, not hidden. */
  actor: string | null;
}

export const getRefunds = (
  params: { from?: string; to?: string; limit?: number } = {},
) =>
  apiFetch<{ items: RefundRow[] }>({
    url: `/admin/refunds${query(params)}`,
    method: "GET",
  });

export interface PayoutRun {
  id: string;
  period_start: string;
  period_end: string;
  state: string;
  statements: number;
  total: Money;
  approved_by: string | null;
  approved_at: string | null;
  paid_at: string | null;
  created_at: string;
}

export const getPayoutRuns = () =>
  apiFetch<{ items: PayoutRun[] }>({
    url: "/admin/payouts/runs",
    method: "GET",
  });

export interface PayoutStatement {
  id: string;
  run_id: string | null;
  vendor_id: string;
  vendor: string;
  reference_no: string;
  period_start: string;
  period_end: string;
  gross: Money;
  commission: Money;
  vat: Money;
  adjustments: Money;
  net: Money;
  state: string;
  issued_at: string | null;
  paid_at: string | null;
  payment_ref: string | null;
  /** Why this cannot be paid, in words. Null means it can. */
  blocked: string | null;
}

export const getStatements = (
  params: { run_id?: string; vendor_id?: string } = {},
) =>
  apiFetch<{ items: PayoutStatement[] }>({
    url: params.run_id
      ? `/admin/payouts/runs/${params.run_id}/statements`
      : `/admin/payouts/statements${query({ vendor_id: params.vendor_id })}`,
    method: "GET",
  });

export interface DriverCash {
  driver_id: string;
  driver: string;
  phone: string | null;
  /** Collected and not yet remitted — the platform's money in someone else's
   *  pocket, which is the number this page exists for. */
  held: Money;
  held_stops: number;
  remitted: Money;
  settled: Money;
  shortfall: Money;
  oldest_held: string | null;
}

export const getDriverCash = () =>
  apiFetch<{ items: DriverCash[]; total_held: Money }>({
    url: "/admin/cod/drivers",
    method: "GET",
  });

export interface Remittance {
  id: string;
  driver_id: string;
  driver: string;
  reference_no: string;
  declared: Money;
  counted: Money | null;
  expected: Money;
  variance: Money;
  state: string;
  method: string;
  declared_at: string;
  settled_at: string | null;
  collections: number;
}

export const getRemittances = (params: { driver_id?: string } = {}) =>
  apiFetch<{ items: Remittance[] }>({
    url: `/admin/cod/remittances${query(params)}`,
    method: "GET",
  });

export interface InvoiceRow {
  id: string;
  number: string;
  kind: string;
  status: string;
  supplier: string;
  supplier_trn: string | null;
  buyer: string | null;
  buyer_trn: string | null;
  country: string;
  net: Money;
  tax: Money;
  gross: Money;
  supply_date: string;
  issued_at: string;
  order_id: string | null;
  order_no: string | null;
  corrects_id: string | null;
  corrects_number: string | null;
  has_pdf: boolean;
}

export const getInvoices = (
  params: { q?: string; from?: string; to?: string } = {},
) =>
  apiFetch<{ items: InvoiceRow[] }>({
    url: `/admin/invoices${query(params)}`,
    method: "GET",
  });

/** Builds a query string, dropping empties so a blank filter is absent rather
 *  than present-and-empty — the server treats those differently. */
function query(params: Record<string, string | number | undefined>): string {
  const parts = Object.entries(params)
    .filter(([, v]) => v !== undefined && v !== "")
    .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`);
  return parts.length ? `?${parts.join("&")}` : "";
}

// ── supply (§4.4) ───────────────────────────────────────────────────────────

export interface ReelDetail {
  id: string;
  state: string;
  visibility: string;
  caption: string | null;
  locale: string | null;
  author_type: string;
  author_id: string;
  author: string;
  category_id: string | null;
  duration_ms: number;
  created_at: string;
  published_at: string | null;
  removed_at: string | null;
  /** The four timestamps and a failure code are the whole "why is this stuck"
   *  diagnostic. A reel that never probed failed somewhere different to one
   *  that probed and never transcoded, and a state name cannot tell them
   *  apart. */
  pipeline: {
    bytes_ready_at: string | null;
    metadata_ready_at: string | null;
    upload_expires_at: string | null;
    probed_at: string | null;
    attempts: number;
    failure_code: string | null;
    failure_detail: string | null;
  };
  asset: {
    id: string | null;
    mime_type: string | null;
    bytes: number | null;
    width: number | null;
    height: number | null;
    has_source: boolean;
    has_poster: boolean;
  };
  coverage: { kind: string | null; resolved: boolean };
  renditions: Array<{
    label: string;
    width: number;
    height: number;
    bitrate_kbps: number;
    playlist_url: string;
  }>;
  reports: number;
  engagement: {
    views: number;
    likes: number;
    comments: number;
    shares: number;
    saves: number;
  };
}

export const getReelDetail = (id: string) =>
  apiFetch<ReelDetail>({ url: `/admin/reels/${id}`, method: "GET" });

export const getReelTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/reels/${id}/timeline`,
    method: "GET",
  });

/**
 * A short-lived playback URL for staff.
 *
 * Separate from the detail fetch on purpose: opening a reel and *watching* it
 * are different events, and only the second belongs in the answer to "who
 * looked at this video". Every call is recorded server-side.
 */
export const getReelMedia = (id: string, kind?: "source" | "poster") =>
  apiFetch<{
    url: string;
    mime_type: string | null;
    expires_in: number;
    state: string;
  }>({
    url: `/admin/reels/${id}/media${kind === "poster" ? "?kind=poster" : ""}`,
    method: "GET",
  });

export interface SupplyGap {
  vendor_id: string;
  vendor: string;
  city: string | null;
  published: number;
  pending_review: number;
  last_published: string | null;
  /** Null means never published — a different and more urgent problem than
   *  having gone quiet, so it is not flattened into a large number. */
  days_quiet: number | null;
}

export const getSupplyGaps = (quietDays?: number) =>
  apiFetch<{ items: SupplyGap[] }>({
    url: `/admin/supply/gaps${quietDays ? `?quiet_days=${quietDays}` : ""}`,
    method: "GET",
  });

export interface CoverageArea {
  emirate: string;
  city: string;
  reels: number;
  vendors: number;
  last_published: string | null;
}

export const getCoverage = () =>
  apiFetch<{ items: CoverageArea[] }>({
    url: "/admin/supply/coverage",
    method: "GET",
  });

// ── configuration and health (§4.5, §4.6) ───────────────────────────────────

export interface FeatureFlag {
  key: string;
  description: string;
  enabled: boolean;
  rollout_pct: number;
  enabled_for: number;
  updated_by: string | null;
  updated_at: string;
}

export const getFlags = () =>
  apiFetch<{ items: FeatureFlag[] }>({
    url: "/admin/config/flags",
    method: "GET",
  });

export const setFlag = (
  key: string,
  body: { enabled: boolean; rollout_pct: number; reason: string },
) =>
  apiFetch<{ status: string }>({
    url: `/admin/config/flags/${encodeURIComponent(key)}`,
    method: "PUT",
    data: body,
  });

export interface AppSetting {
  key: string;
  value: unknown;
  description: string;
  /** A sensitive setting's value is withheld from callers without
   *  `config.write`; the row itself is not, so nobody creates a duplicate under
   *  a slightly different name. */
  sensitive: boolean;
  updated_by: string | null;
  updated_at: string;
}

export const getSettings = () =>
  apiFetch<{ items: AppSetting[] }>({
    url: "/admin/config/settings",
    method: "GET",
  });

export const setSetting = (
  key: string,
  body: { value: unknown; reason: string },
) =>
  apiFetch<{ status: string }>({
    url: `/admin/config/settings/${encodeURIComponent(key)}`,
    method: "PUT",
    data: body,
  });

export interface Health {
  outbox: Array<{
    aggregate_type: string;
    unpublished: number;
    /** The number the alarm is set on. Ten thousand events a minute old is a
     *  busy system; three from yesterday is a dead consumer. */
    oldest_seconds: number | null;
    failing: number;
    max_attempts: number;
    last_error: string | null;
  }>;
  partitions: Array<{
    parent: string;
    name: string;
    bound: string;
    is_default: boolean;
    rows: number;
    /** A DEFAULT partition holding rows: the range it occupies can never be
     *  attached as a real partition without moving them, so a late partition
     *  job has become a permanent one. */
    trapped: boolean;
  }>;
  loops: Array<{
    loop: string;
    last_seen: string | null;
    pending: number;
    stale: boolean;
  }>;
  delivery: Array<{
    channel: string;
    sent: number;
    failed: number;
    suppressed: number;
    queued: number;
    last_sent_at: string | null;
  }>;
  moderation: Array<{
    day: string;
    decided: number;
    breached: number;
    median_seconds: number | null;
  }>;
  generated_at: string;
}

export const getHealth = () =>
  apiFetch<Health>({ url: "/admin/health", method: "GET" });

// ── payout actions ──────────────────────────────────────────────────────────
//
// All need `money.write`. Each re-checks vendor eligibility server-side at the
// moment it runs — a statement issued on Monday and paid on Friday can belong
// to a vendor whose licence lapsed on Wednesday, and nothing writes to their
// record when the date passes.

export const approveRun = (runId: string, reason: string) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/payouts/runs/${runId}/approve`,
    method: "POST",
    data: { reason },
  });

export interface IssueRunResult {
  issued: number;
  /** Returned rather than swallowed: eighteen skips that all read the same is a
   *  process problem, and that is only visible as a list. */
  skipped: Array<{
    statement_id: string;
    vendor_id: string;
    vendor: string;
    reason: string;
  }>;
}

export const issueRun = (runId: string, reason: string) =>
  apiFetch<IssueRunResult>({
    url: `/admin/payouts/runs/${runId}/issue`,
    method: "POST",
    data: { reason },
  });

export const issueStatement = (statementId: string, reason: string) =>
  apiFetch<{ status: string }>({
    url: `/admin/payouts/statements/${statementId}/issue`,
    method: "POST",
    data: { reason },
  });

export const payStatement = (
  statementId: string,
  body: { payment_ref: string; reason: string },
) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/payouts/statements/${statementId}/pay`,
    method: "POST",
    data: body,
  });

// ── versioned configuration ─────────────────────────────────────────────────
//
// These supersede rather than update. The old row is closed and a new one
// opened, so an invoice raised last month still references the rate that was in
// force when it was raised.

export interface TaxRate {
  id: string;
  country: string;
  region: string | null;
  category: string;
  rate_bp: number;
  label: string;
  from: string;
  to: string | null;
  current: boolean;
}

export const getTaxRates = () =>
  apiFetch<{ items: TaxRate[] }>({
    url: "/admin/config/tax-rates",
    method: "GET",
  });

export const supersedeTaxRate = (
  id: string,
  body: {
    rate_bp: number;
    label?: string;
    effective_from: string;
    reason: string;
  },
) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/config/tax-rates/${id}/supersede`,
    method: "POST",
    data: body,
  });

export interface RateCard {
  id: string;
  placement: string;
  country: string;
  region: string | null;
  floor: Money;
  from: string;
  to: string | null;
  current: boolean;
}

export const getRateCards = () =>
  apiFetch<{ items: RateCard[] }>({
    url: "/admin/config/rate-cards",
    method: "GET",
  });

export const supersedeRateCard = (
  id: string,
  body: { floor_cpm_minor: number; effective_from: string; reason: string },
) =>
  apiFetch<{ status: string }>({
    url: `/admin/config/rate-cards/${id}/supersede`,
    method: "POST",
    data: body,
  });

export interface NotificationTemplate {
  id: string;
  key: string;
  channel: string;
  locale: string;
  version: number;
  category: string;
  subject: string | null;
  body: string;
  active: boolean;
  /** What one send costs. Arabic sits entirely outside GSM-7, so its budget is
   *  70 characters rather than 160 — an Arabic message shorter than an English
   *  one can cost twice as much, and a character count hides that completely. */
  segments: number;
  placeholders: string[];
  updated_at: string;
}

export const getTemplates = (all = false) =>
  apiFetch<{ items: NotificationTemplate[] }>({
    url: `/admin/config/templates${all ? "?all=true" : ""}`,
    method: "GET",
  });

export const previewSegments = (body: string) =>
  apiFetch<{ segments: number; characters: number; placeholders: string[] }>({
    url: "/admin/config/templates/preview",
    method: "POST",
    data: { body },
  });

export const reviseTemplate = (
  id: string,
  body: { subject?: string; body: string; reason: string },
) =>
  apiFetch<{ status: string; segments: number; note: string }>({
    url: `/admin/config/templates/${id}/revise`,
    method: "POST",
    data: body,
  });

// ── listing, driver, feed inspector ─────────────────────────────────────────

export interface ListingDetail {
  id: string;
  reference_no: string;
  status: string;
  seller: {
    id: string;
    kind: string;
    name: string | null;
    phone: string | null;
    dealer_id: string | null;
    dealer: string | null;
    /** The number that matters most and is invisible from the listing alone: a
     *  private seller with eleven cars is a dealer who has not said so. */
    other_listings: number;
    sold: number;
  };
  vehicle: {
    make: string;
    model: string;
    year: number;
    mileage_km: number;
    vin: string | null;
    spec: string | null;
    body: string | null;
    fuel: string | null;
    colour: string | null;
    registration_expiry: string | null;
    warranty_until: string | null;
  };
  price: Money;
  emirate: string | null;
  city: string | null;
  photos: number;
  lifecycle: {
    created_at: string;
    published_at: string | null;
    expires_at: string | null;
    featured_until: string | null;
    bumped_at: string | null;
    sold_at: string | null;
    sold_price: Money | null;
    removed_at: string | null;
    rejection: string | null;
  };
  /** Severity is 0–100, not a word: the thresholds that decide whether a signal
   *  blocks publication are numeric, and bucketing here would put the boundary
   *  in two places. */
  signals: Array<{
    kind: string;
    severity: number;
    detail: string | null;
    state: string;
    at: string;
  }>;
  entitlement: {
    code: string;
    name: string;
    purchased_at: string;
    starts_at: string | null;
    ends_at: string | null;
    listing_quota: number;
    listings_used: number;
    bump_quota: number;
    bumps_used: number;
    status: string;
  } | null;
}

export const getListingDetail = (id: string) =>
  apiFetch<ListingDetail>({ url: `/admin/listings/${id}`, method: "GET" });

export const getListingTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/listings/${id}/timeline`,
    method: "GET",
  });

export interface DriverDetail {
  id: string;
  user_id: string;
  name: string;
  phone: string | null;
  status: string;
  vehicle_type: string;
  plate_no: string | null;
  vendor_id: string | null;
  vendor: string | null;
  max_stops: number;
  created_at: string;
  position: { lat: number; lng: number } | null;
  /** Carried separately from the position. A stale position with a recent ping
   *  and no position at all are different faults needing different calls. */
  last_ping_at: string | null;
  shift: { on: boolean; from: string | null; until: string | null };
  workload: {
    open_stops: number;
    completed_today: number;
    failed_today: number;
  };
  cash: { held: Money; stops: number; oldest: string | null };
  documents: Array<{
    kind: string;
    state: string;
    expires_on: string | null;
    expired: boolean;
  }>;
}

export const getDriverDetail = (id: string) =>
  apiFetch<DriverDetail>({ url: `/admin/drivers/${id}`, method: "GET" });

export const getDriverTimeline = (id: string) =>
  apiFetch<{ items: TimelineEntry[] }>({
    url: `/admin/drivers/${id}/timeline`,
    method: "GET",
  });

export interface FeedInspection {
  profile: string;
  /** Before the page limit. Ten drawn from eleven and ten drawn from four
   *  hundred look identical in the list and are entirely different. */
  retrieved: number;
  shown: number;
  items: Array<{
    rank: number;
    reel_id: string;
    caption: string | null;
    author: string | null;
    score: number;
    terms: {
      conversion: number;
      intent: number;
      engagement: number;
      author: number;
      recency: number;
      affinity: number;
      followed: number;
      proximity: number;
    };
    features: {
      impressions: number;
      views: number;
      orders: number;
      lifetime: number;
      distance_km: number;
      age_hours: number;
      affinity: number;
      followed: boolean;
    };
    /** Below the lifetime-impression floor, so it can be interleaved regardless
     *  of score. The usual explanation for "why is this at position 4", and
     *  invisible in the score by construction. */
    explore_eligible: boolean;
  }>;
}

export const inspectFeed = (params: {
  lat: number;
  lng: number;
  viewer_id?: string;
  limit?: number;
}) =>
  apiFetch<FeedInspection>({
    url: `/admin/feed/inspect?lat=${params.lat}&lng=${params.lng}${
      params.viewer_id ? `&viewer_id=${params.viewer_id}` : ""
    }${params.limit ? `&limit=${params.limit}` : ""}`,
    method: "GET",
  });

// ── account lifecycle ───────────────────────────────────────────────────────
//
// All three need `identity.review`, which is deliberately never in a default
// role with `money.write`: the person who can change the number on an account
// and the person who can move money out of it should be two people.

export const changePhone = (
  userId: string,
  body: { phone: string; reason: string },
) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/users/${userId}/phone`,
    method: "POST",
    data: body,
  });

export interface MergeResult {
  status: string;
  moved: { addresses: number; favourites: number; searches: number };
  /** What deliberately did not move. Shown rather than buried — an operator who
   *  does not see this discovers it when the customer asks where their receipts
   *  went. */
  stayed: { orders: number; invoices: number };
  note: string;
}

export const mergeAccounts = (
  duplicateId: string,
  body: { survivor_id: string; reason: string },
) =>
  apiFetch<MergeResult>({
    url: `/admin/users/${duplicateId}/merge`,
    method: "POST",
    data: body,
  });

export const eraseAccount = (userId: string, reason: string) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/users/${userId}/erase`,
    method: "POST",
    data: { reason },
  });

// ── view as (§3.7) ──────────────────────────────────────────────────────────

export interface ViewAsSession {
  access_token: string;
  expires_in: number;
  locale: string;
  subject_id: string;
  /** Always true, and enforced server-side by middleware on the whole
   *  authenticated router — not by hiding buttons. Every unsafe method under
   *  this token is refused before it reaches a handler. */
  read_only: boolean;
  note: string;
}

export const viewAs = (userId: string, reason: string) =>
  apiFetch<ViewAsSession>({
    url: `/admin/users/${userId}/view-as`,
    method: "POST",
    data: { reason },
  });

// ── meals ───────────────────────────────────────────────────────────────────

export interface MealKitchen {
  branch_id: string;
  branch_name: string;
  city: string | null;
  branch_active: boolean;
  vendor_id: string;
  vendor_name: string;
  vendor_status: string;
  dishes: number;
  live_plans: number;
  rotation_plans: number;
  active_subscribers: number;
  created_at: string;
}

export interface MealKitchenDetail {
  profile: Profile;
  overview: Overview;
  menu: WeeklyMenu;
  plans: Plan[];
  dishes: Dish[];
}

export const SUBSCRIPTION_STATUSES = [
  "pending_payment",
  "active",
  "paused",
  "suspended",
  "cancelled",
  "completed",
] as const;

export interface MealSubscriptionRow {
  id: string;
  subscription_no: string;
  status: (typeof SUBSCRIPTION_STATUSES)[number];
  user_id: string;
  branch_id: string;
  branch_name: string;
  vendor_id: string;
  vendor_name: string;
  plan_id: string;
  plan_name: string;
  start_date: string;
  end_date: string | null;
  cycle_no: number | null;
  cycle_status: string | null;
  cycle_amount_minor: number | null;
  currency: string | null;
  created_at: string;
}

export interface MealSubscriptionDetail extends MealSubscriptionRow {
  cancelled_at: string | null;
  cancellation_reason: string | null;
  pause_days_used: number;
  allowed_pause_days: number;
  cycles: Array<{
    cycle_no: number;
    starts_on: string;
    ends_on: string;
    status: string;
    amount_minor: number;
    recognised_minor: number;
    currency: string;
    order_id: string | null;
  }>;
  pauses: Array<{
    from_date: string;
    to_date: string;
    credit_mode: string;
    state: string;
    suppressed_days: number;
  }>;
  skips: Array<{
    skip_date: string;
    meal_slot: string | null;
    reason: string | null;
  }>;
  events: Array<{
    event_type: string;
    from_status: string | null;
    to_status: string | null;
    actor_type: string;
    actor_id: string | null;
    metadata: Record<string, unknown> | null;
    occurred_at: string;
  }>;
}

export const getMealKitchens = (
  params: { q?: string; rotation?: boolean } = {},
) =>
  apiFetch<{ items: MealKitchen[] }>({
    url: "/admin/meals/kitchens",
    method: "GET",
    params: {
      q: params.q || undefined,
      rotation: params.rotation ? "true" : undefined,
    },
  });

export const getMealKitchen = (branchId: string) =>
  apiFetch<MealKitchenDetail>({
    url: `/admin/meals/kitchens/${branchId}`,
    method: "GET",
  });

export const setMealPlanActive = (
  planId: string,
  active: boolean,
  reason: string,
) =>
  apiFetch<Plan>({
    url: `/admin/meals/plans/${planId}/active`,
    method: "POST",
    data: { active, reason },
  });

export const getMealSubscriptions = (
  params: { q?: string; status?: string; branch_id?: string } = {},
) =>
  apiFetch<{ items: MealSubscriptionRow[] }>({
    url: "/admin/meals/subscriptions",
    method: "GET",
    params: {
      q: params.q || undefined,
      status: params.status || undefined,
      branch_id: params.branch_id || undefined,
    },
  });

export const getMealSubscription = (id: string) =>
  apiFetch<MealSubscriptionDetail>({
    url: `/admin/meals/subscriptions/${id}`,
    method: "GET",
  });

export const cancelMealSubscription = (id: string, reason: string) =>
  apiFetch<{ status: string; note: string }>({
    url: `/admin/meals/subscriptions/${id}/cancel`,
    method: "POST",
    data: { reason },
  });

export function statusTone(
  status: string,
): "ok" | "warn" | "danger" | undefined {
  if (status === "active") return "ok";
  if (status === "paused" || status === "pending_payment") return "warn";
  if (status === "suspended") return "danger";
  return undefined;
}

// ---- Meals merchandising --------------------------------------------------

export type { MerchBanner, MerchCollection, MerchSection, CollectionRules };

export type MerchCollectionInput = Omit<
  MerchCollection,
  "id" | "items" | "updated_at"
>;
export type MerchBannerInput = Omit<MerchBanner, "id" | "updated_at">;

export interface MerchVersion {
  id: string;
  version_number: number;
  status: "draft" | "published";
  sections: MerchSection[];
  publish_at: string | null;
  published_by: string | null;
  note: string | null;
  updated_at: string;
}

export interface MerchLayoutState {
  draft: MerchVersion | null;
  live_id: string | null;
  versions: MerchVersion[];
  section_types: string[];
}

type Pt = { lat: number; lng: number } | null | undefined;

export const getMerchCollections = () =>
  apiFetch<{ items: MerchCollection[] }>({
    url: "/admin/meals/collections",
    method: "GET",
  });
export const getMerchCollection = (id: string) =>
  apiFetch<MerchCollection>({
    url: `/admin/meals/collections/${id}`,
    method: "GET",
  });
export const saveMerchCollection = (
  id: string | null,
  data: MerchCollectionInput,
) =>
  apiFetch<MerchCollection>({
    url: id ? `/admin/meals/collections/${id}` : "/admin/meals/collections",
    method: id ? "PUT" : "POST",
    data,
  });
export const setMerchCollectionItems = (
  id: string,
  items: { branch_id: string; role: "pin" | "exclude" }[],
) =>
  apiFetch<MerchCollection>({
    url: `/admin/meals/collections/${id}/items`,
    method: "PUT",
    data: { items },
  });
export const deleteMerchCollection = (id: string) =>
  apiFetch<void>({ url: `/admin/meals/collections/${id}`, method: "DELETE" });
export const previewMerchCollection = (id: string, p: Pt) =>
  apiFetch<{ items: PublicKitchen[] }>({
    url: `/admin/meals/collections/${id}/preview`,
    method: "GET",
    params: { lat: p?.lat, lng: p?.lng },
  });

export const getMerchBanners = () =>
  apiFetch<{ items: MerchBanner[] }>({
    url: "/admin/meals/banners",
    method: "GET",
  });
export const saveMerchBanner = (id: string | null, data: MerchBannerInput) =>
  apiFetch<MerchBanner>({
    url: id ? `/admin/meals/banners/${id}` : "/admin/meals/banners",
    method: id ? "PUT" : "POST",
    data,
  });
export const deleteMerchBanner = (id: string) =>
  apiFetch<void>({ url: `/admin/meals/banners/${id}`, method: "DELETE" });

export const getMerchLayout = () =>
  apiFetch<MerchLayoutState>({ url: "/admin/meals/layout", method: "GET" });
export const saveMerchDraft = (sections: MerchSection[]) =>
  apiFetch<MerchVersion>({
    url: "/admin/meals/layout/draft",
    method: "PUT",
    data: { sections },
  });
export const previewMerchLayout = (sections: MerchSection[], p: Pt) =>
  apiFetch<MealsHome>({
    url: "/admin/meals/layout/preview",
    method: "POST",
    data: { sections },
    params: { lat: p?.lat, lng: p?.lng },
  });
export const publishMerchLayout = (publishAt: string | null, note: string) =>
  apiFetch<MerchVersion>({
    url: "/admin/meals/layout/publish",
    method: "POST",
    data: { publish_at: publishAt, note },
  });
export const restoreMerchVersion = (versionId: string) =>
  apiFetch<MerchVersion>({
    url: `/admin/meals/layout/versions/${versionId}/restore`,
    method: "POST",
  });

// ---- promos & trials ------------------------------------------------------

export interface MerchPromoInput {
  code: string | null;
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
  kitchen_share_bp: number;
  usage_limit: number | null;
  per_customer_limit: number | null;
  is_public: boolean;
  is_active: boolean;
  starts_at: string | null;
  ends_at: string | null;
  branch_ids: string[];
}

export interface MerchPromo extends MerchPromoInput {
  id: string;
  usage_count: number;
  updated_at: string;
}

export interface PromoRedemption {
  id: string;
  user_id: string;
  subscription_id: string | null;
  rotation_id: string | null;
  reference: string;
  discount_minor: number;
  platform_funded_minor: number;
  currency: string;
  status: "pending" | "redeemed" | "released";
  created_at: string;
}

export const getPromos = () =>
  apiFetch<{ items: MerchPromo[] }>({ url: "/admin/meals/promos", method: "GET" });
export const savePromo = (id: string | null, data: MerchPromoInput) =>
  apiFetch<MerchPromo>({
    url: id ? `/admin/meals/promos/${id}` : "/admin/meals/promos",
    method: id ? "PUT" : "POST",
    data,
  });
export const deletePromo = (id: string) =>
  apiFetch<void>({ url: `/admin/meals/promos/${id}`, method: "DELETE" });
export const getPromoRedemptions = (id: string) =>
  apiFetch<{ items: PromoRedemption[] }>({
    url: `/admin/meals/promos/${id}/redemptions`,
    method: "GET",
  });

// ---- kitchen reviews & food safety ----------------------------------------

export interface AdminReview {
  id: string;
  branch_id: string;
  subscription_id: string;
  user_id: string;
  kitchen_name: string;
  rating: number;
  comment: string;
  status: "published" | "hidden";
  hidden_reason?: string | null;
  reply: string | null;
  created_at: string;
}

export const getMealReviews = (status = "") =>
  apiFetch<{ items: AdminReview[] }>({
    url: "/admin/meals/reviews",
    method: "GET",
    params: { status: status || undefined },
  });

export const setMealReviewHidden = (id: string, hidden: boolean, reason: string) =>
  apiFetch<AdminReview>({
    url: `/admin/meals/reviews/${id}/visibility`,
    method: "PUT",
    data: { hidden, reason },
  });

export interface FoodSafety {
  ref: string | null;
  expires_on: string | null;
  verified_at: string | null;
  verified: boolean;
}

export const getFoodSafety = (branchId: string) =>
  apiFetch<FoodSafety>({
    url: `/admin/meals/kitchens/${branchId}/food-safety`,
    method: "GET",
  });

export const setFoodSafety = (
  branchId: string,
  data: { verified: boolean; ref: string; expires_on: string },
) =>
  apiFetch<FoodSafety>({
    url: `/admin/meals/kitchens/${branchId}/food-safety`,
    method: "PUT",
    data,
  });

// --- Office lunch accounts -------------------------------------------------

export type {
  Company,
  CompanyCharge,
  CompanyCredit,
  CompanyDashboard,
  CompanyStatement,
} from "./customer-api";
import type {
  Company as CompanyT,
  CompanyCredit as CompanyCreditT,
  CompanyDashboard as CompanyDashboardT,
  CompanyStatement as CompanyStatementT,
} from "./customer-api";

export interface AdminCompany extends CompanyT {
  members: number;
  lunches: number;
  credit: CompanyCreditT;
}

export const getCompanies = (status = "", q = "") =>
  apiFetch<{ items: AdminCompany[] }>({
    url: `/admin/meals/companies${query({ status: status || undefined, q: q || undefined })}`,
    method: "GET",
  });
export const getAdminCompany = (id: string) =>
  apiFetch<CompanyDashboardT>({ url: `/admin/meals/companies/${id}`, method: "GET" });
export const getAdminCompanyStatement = (id: string, statementId: string) =>
  apiFetch<CompanyStatementT>({
    url: `/admin/meals/companies/${id}/statements/${statementId}`,
    method: "GET",
  });
export const reviewCompany = (
  id: string,
  data: {
    status?: "active" | "rejected" | "suspended";
    credit_limit_minor?: number;
    payment_terms_days?: number;
    note?: string;
  },
) => apiFetch<void>({ url: `/admin/meals/companies/${id}/review`, method: "PUT", data });
export const issueCompanyStatement = (id: string, periodEnd: string) =>
  apiFetch<{ id: string }>({
    url: `/admin/meals/companies/${id}/statements`,
    method: "POST",
    data: { period_end: periodEnd },
  });
export const markCompanyStatementPaid = (id: string, statementId: string, reference: string) =>
  apiFetch<void>({
    url: `/admin/meals/companies/${id}/statements/${statementId}/paid`,
    method: "POST",
    data: { reference },
  });
