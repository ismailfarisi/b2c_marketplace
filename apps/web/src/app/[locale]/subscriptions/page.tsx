"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { LoadError } from "@/components/load-error";
import { AccountSections } from "@/components/meals/account";
import {
  CustomerShell,
  STATUS,
  asLocale,
  formatDay,
  formatRange,
} from "@/components/meals/shell";
import { problemMessage } from "@/lib/problem";
import { tokenStore } from "@/lib/token-store";
import {
  deleteAccount,
  getMyReferrals,
  listMyRotations,
  listMySubscriptions,
} from "@/lib/customer-api";

const COPY = {
  en: {
    title: "My subscriptions",
    invoices: "Invoices",
    empty: "You have no meal subscriptions yet.",
    browse: "Find a kitchen",
    cycle: (n: number, end: string | null) =>
      end ? `Cycle ${n}, ends ${end}` : `Cycle ${n}`,
    loading: "Loading…",
    failed: "Could not load your subscriptions.",
    rotations: "4-kitchen rotations",
    singles: "Kitchen subscriptions",
    weekShort: (n: number) => `Week ${n}`,
    rotationTitle: "4-kitchen rotation",
    rotationWeek: (n: number) => `Rotation week ${n}`,
    build: "Build a 4-kitchen rotation",
  },
  ar: {
    title: "اشتراكاتي",
    invoices: "الفواتير",
    empty: "لا توجد لديك اشتراكات وجبات بعد.",
    browse: "ابحث عن مطبخ",
    cycle: (n: number, end: string | null) =>
      end ? `الدورة ${n}، تنتهي ${end}` : `الدورة ${n}`,
    loading: "جارٍ التحميل…",
    failed: "تعذر تحميل اشتراكاتك.",
    rotations: "تناوب ٤ مطابخ",
    singles: "اشتراكات المطابخ",
    weekShort: (n: number) => `الأسبوع ${n}`,
    rotationTitle: "تناوب ٤ مطابخ",
    rotationWeek: (n: number) => `الأسبوع ${n} من التناوب`,
    build: "اختر تناوب ٤ مطابخ",
  },
};

export default function MySubscriptionsPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "subscriptions"],
    queryFn: listMySubscriptions,
  });
  const items = q.data?.items ?? [];
  const rq = useQuery({
    queryKey: ["me", "rotations"],
    queryFn: listMyRotations,
  });
  const rotations = rq.data?.items ?? [];
  const singles = items.filter((s) => s.rotation_id == null);
  const money = (amount: number, currency: string) =>
    formatMoney({ amount, currency }, locale);
  const status = (st: string) => (
    <span className={`ms-status ms-status-${st}`}>
      {STATUS[locale][st] ?? st}
    </span>
  );

  return (
    <CustomerShell locale={locale}>
      <h1>{t.title}</h1>
      <p>
        <Link href={`/${locale}/invoices`}>{t.invoices}</Link>
      </p>
      <InviteCard locale={locale} />
      {rotations.length > 0 && (
        <section>
          <h2>{t.rotations}</h2>
          <ul className="ms-subs">
            {rotations.map((r) => (
              <li key={r.id} className="ms-rot-card">
                <Link
                  href={`/${locale}/rotations/${r.id}`}
                  className="ms-rot-head"
                >
                  <span>
                    <strong>{t.rotationTitle}</strong>
                    <span className="ms-sub">
                      {formatRange(locale, r.start_date, r.end_date)}
                    </span>
                  </span>
                  <span className="ms-sub-side">
                    {status(r.status)}
                    <span>{money(r.amount_minor, r.currency)}</span>
                  </span>
                </Link>
                <ol className="ms-rot-weeks">
                  {r.weeks.map((w) => {
                    const label = (
                      <>
                        <span className="ms-week-no">
                          {t.weekShort(w.week)}
                        </span>
                        <span dir="auto">
                          {(locale === "ar" && w.kitchen_name_ar) ||
                            w.kitchen_name}
                        </span>
                        <span className="ms-sub">
                          {formatRange(locale, w.starts_on, w.ends_on)}
                        </span>
                      </>
                    );
                    return (
                      <li key={w.week}>
                        {w.subscription_id ? (
                          <Link
                            href={`/${locale}/subscriptions/${w.subscription_id}`}
                          >
                            {label}
                          </Link>
                        ) : (
                          <span>{label}</span>
                        )}
                      </li>
                    );
                  })}
                </ol>
              </li>
            ))}
          </ul>
        </section>
      )}
      {q.isPending || rq.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError || rq.isError ? (
        // Either list failing means the empty state would be a lie: someone
        // with only rotations would read "no subscriptions yet".
        <LoadError
          error={q.error ?? rq.error}
          onRetry={() => {
            if (q.isError) void q.refetch();
            if (rq.isError) void rq.refetch();
          }}
          locale={locale}
        />
      ) : items.length === 0 && rotations.length === 0 ? (
        <div className="ms-empty">
          <p>{t.empty}</p>
          <div className="ms-actions">
            <Link className="ms-cta" href={`/${locale}`}>
              {t.browse}
            </Link>
            <Link className="ms-cta ms-cta-quiet" href={`/${locale}/rotation`}>
              {t.build}
            </Link>
          </div>
        </div>
      ) : singles.length > 0 ? (
        <section>
          {rotations.length > 0 && <h2>{t.singles}</h2>}
          <ul className="ms-subs">
            {singles.map((s) => (
              <li key={s.id}>
                <Link
                  href={`/${locale}/subscriptions/${s.id}`}
                  className="ms-sub-card"
                >
                  <span>
                    <strong dir="auto">
                      {(locale === "ar" && s.kitchen_name_ar) || s.kitchen_name}
                    </strong>
                    <span className="ms-sub" dir="auto">
                      {(locale === "ar" && s.plan_name_ar) || s.plan_name} ·{" "}
                      {t.cycle(
                        s.cycle_no,
                        s.cycle_ends_on && formatDay(locale, s.cycle_ends_on),
                      )}
                    </span>
                  </span>
                  <span className="ms-sub-side">
                    {status(s.status)}
                    <span>{money(s.amount_minor, s.currency)}</span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <AccountSections locale={locale} />
      <DeleteAccount locale={locale} />
    </CustomerShell>
  );
}

const DELETE = {
  en: {
    title: "Delete account",
    body: "Your meal plans are cancelled and unused days are credited back. Your name, phone and addresses are removed and you are signed out on every device. Invoices are kept, as the law requires. This cannot be undone.",
    start: "Delete my account",
    confirm: "Yes, delete my account",
    keep: "Keep my account",
    busy: "Deleting…",
    failed: "Your account could not be deleted. Try again.",
    support: "Contact support",
  },
  ar: {
    title: "حذف الحساب",
    body: "سيتم إلغاء خطط وجباتك وإرجاع قيمة الأيام غير المستخدمة. سيُحذف اسمك ورقم هاتفك وعناوينك، وسيتم تسجيل خروجك من كل الأجهزة. نحتفظ بالفواتير كما يتطلب القانون. لا يمكن التراجع عن ذلك.",
    start: "احذف حسابي",
    confirm: "نعم، احذف حسابي",
    keep: "الإبقاء على حسابي",
    busy: "جارٍ الحذف…",
    failed: "تعذّر حذف حسابك. حاول مرة أخرى.",
    support: "تواصل مع الدعم",
  },
};

function DeleteAccount({ locale }: { locale: "en" | "ar" }) {
  const t = DELETE[locale];
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [needsSupport, setNeedsSupport] = useState(false);
  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      await deleteAccount();
      await tokenStore.clear();
      window.location.href = `/${locale}?deleted=1`;
    } catch (e) {
      const coded = e instanceof ApiError && e.status === 409;
      setError(coded ? problemMessage(e, locale) : t.failed);
      setNeedsSupport(e instanceof ApiError && e.problem?.code === "owns_vendor");
      setBusy(false);
    }
  };
  return (
    <section className="ms-delete" aria-labelledby="ms-delete-title">
      <h2 id="ms-delete-title">{t.title}</h2>
      <p className="ms-sub">{t.body}</p>
      {error && (
        <p className="ms-error" role="alert">
          {error}{" "}
          {needsSupport && <Link href={`/${locale}/support`}>{t.support}</Link>}
        </p>
      )}
      {confirming ? (
        <div className="ms-delete-row">
          <button type="button" className="ms-cta ms-cta-danger" disabled={busy} onClick={remove}>
            {busy ? t.busy : t.confirm}
          </button>
          <button type="button" className="ms-cta ms-cta-quiet" disabled={busy} onClick={() => setConfirming(false)}>
            {t.keep}
          </button>
        </div>
      ) : (
        <button type="button" className="ms-link-danger" onClick={() => setConfirming(true)}>
          {t.start}
        </button>
      )}
    </section>
  );
}

const INVITE = {
  en: {
    title: (reward: string) => `Invite friends, get ${reward}`,
    body: (friend: string, reward: string) =>
      `Friends get ${friend} off their first meal plan. You get a ${reward} voucher when their first meal is delivered.`,
    code: "Your invite code",
    copy: "Copy link",
    copied: "Link copied",
    whatsapp: "Share on WhatsApp",
    share: (friend: string, link: string) =>
      `Home-style meals delivered every day. Get ${friend} off your first plan with my link: ${link}`,
    stats: (p: number, r: number) => `${p} waiting for a first delivery · ${r} earned`,
    vouchers: "Your vouchers",
    voucherUse: "Enter it as a promo code at checkout.",
    until: (d: string) => `until ${d}`,
    used: "Used",
    expired: "Expired",
  },
  ar: {
    title: (reward: string) => `ادعُ أصدقاءك واحصل على ${reward}`,
    body: (friend: string, reward: string) =>
      `يحصل أصدقاؤك على خصم ${friend} على أول خطة وجبات، وتحصل أنت على قسيمة بقيمة ${reward} عند توصيل أول وجبة لهم.`,
    code: "رمز دعوتك",
    copy: "انسخ الرابط",
    copied: "تم نسخ الرابط",
    whatsapp: "شارك عبر واتساب",
    share: (friend: string, link: string) =>
      `وجبات بيتية تصلك كل يوم. احصل على خصم ${friend} على أول خطة عبر رابطي: ${link}`,
    stats: (p: number, r: number) => `${p} بانتظار أول توصيل · ${r} مكتسبة`,
    vouchers: "قسائمك",
    voucherUse: "أدخلها كرمز خصم عند الدفع.",
    until: (d: string) => `حتى ${d}`,
    used: "مستخدمة",
    expired: "منتهية",
  },
};

function InviteCard({ locale }: { locale: "en" | "ar" }) {
  const t = INVITE[locale];
  const [copied, setCopied] = useState(false);
  const q = useQuery({ queryKey: ["me", "referrals"], queryFn: getMyReferrals });
  const d = q.data;
  if (!d || (!d.active && d.vouchers.length === 0)) return null;
  const money = (amount: number) => formatMoney({ amount, currency: d.currency }, locale);
  const link =
    d.code && typeof window !== "undefined"
      ? `${window.location.origin}/${locale}?ref=${d.code}`
      : "";
  return (
    <section className="ms-invite" aria-labelledby="ms-invite-title">
      {d.active && d.code && (
        <>
          <h2 id="ms-invite-title">{t.title(money(d.reward_amount_minor))}</h2>
          <p>{t.body(money(d.friend_amount_minor), money(d.reward_amount_minor))}</p>
          <div className="ms-invite-row">
            <span className="ms-invite-code">
              <span className="ms-sub">{t.code}</span>
              <strong dir="ltr">{d.code}</strong>
            </span>
            <button
              type="button"
              className="ms-cta ms-cta-quiet"
              onClick={() => {
                navigator.clipboard?.writeText(link).then(() => setCopied(true));
              }}
            >
              {copied ? t.copied : t.copy}
            </button>
            <a
              className="ms-cta"
              target="_blank"
              rel="noopener noreferrer"
              href={`https://wa.me/?text=${encodeURIComponent(t.share(money(d.friend_amount_minor), link))}`}
            >
              {t.whatsapp}
            </a>
          </div>
          {(d.pending > 0 || d.rewarded > 0) && (
            <p className="ms-sub">{t.stats(d.pending, d.rewarded)}</p>
          )}
        </>
      )}
      {d.vouchers.length > 0 && (
        <div className="ms-vouchers">
          <h3 id={d.active ? undefined : "ms-invite-title"}>{t.vouchers}</h3>
          <p className="ms-sub">{t.voucherUse}</p>
          <ul>
            {d.vouchers.map((v) => (
              <li key={v.code} className={v.active ? "" : "ms-voucher-off"}>
                <strong dir="ltr">{v.code}</strong>
                <span>{money(v.amount_minor)}</span>
                <span className="ms-sub">
                  {v.used
                    ? t.used
                    : !v.active
                      ? t.expired
                      : v.ends_at
                        ? t.until(formatDay(locale, v.ends_at.slice(0, 10)))
                        : ""}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
