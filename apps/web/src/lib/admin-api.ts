import { apiFetch } from "@marketplace/api-client";

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
  apiFetch<{ queues: QueueDepth[] }>({ url: "/moderation/queues", method: "GET" });

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
  apiFetch<{ items: ReelQueueItem[] }>({ url: "/moderation/queue", method: "GET" });

export const decideReel = (reelId: string, decision: "approve" | "reject", notes: string) =>
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
  apiFetch<{ items: CommentQueueItem[] }>({ url: "/moderation/comments", method: "GET" });

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
  apiFetch<{ items: MessageQueueItem[] }>({ url: "/moderation/messages", method: "GET" });

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
  apiFetch<{ items: ListingQueueItem[] }>({ url: "/moderation/listings", method: "GET" });

export const decideListing = (listingId: string, approve: boolean, reason: string) =>
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
  apiFetch<{ items: FraudSignal[] }>({ url: "/moderation/fraud", method: "GET" });

export const decideFraud = (body: {
  subject_type: string;
  subject_id: string;
  kind: string;
  confirm: boolean;
}) => apiFetch<{ status: string }>({ url: "/moderation/fraud/decide", method: "POST", data: body });

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
  apiFetch<{ items: OverdueAppeal[] }>({ url: "/moderation/appeals", method: "GET" });

export const decideAppeal = (appealId: string, overturn: boolean, note: string) =>
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
  apiFetch<{ items: PendingPayoutAccount[] }>({ url: "/payout-accounts", method: "GET" });

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
  apiFetch<{ items: PendingCampaign[] }>({ url: "/moderation/boosts", method: "GET" });

export const reviewCampaign = (campaignId: string, approve: boolean, note: string) =>
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
  apiFetch<Record<string, unknown>>({ url: "/analytics/feed-funnel", method: "GET" });

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

export const getWhoami = () => apiFetch<Whoami>({ url: "/admin/whoami", method: "GET" });

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
  branch: { id: string; name: string; vendor: string; vendor_id: string } | null;
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

export const refundOrder = (id: string, body: { amount_minor?: number; reason: string }) =>
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
  reels: { published: number; pending_review: number; last_published: string | null };
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
}) => apiFetch<{ status: string }>({ url: "/admin/staff/grants", method: "POST", data: body });

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
