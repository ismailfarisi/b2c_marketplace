"use client";

import { formatMoney } from "@marketplace/api-client";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { formatRange, type Locale } from "@/components/meals/shell";
import {
  dismissGuestWeek,
  startGuestWeek,
  type GuestWeekOption,
  type MySubscriptionDetail,
} from "@/lib/customer-api";
import { tagLabel } from "@/components/meals/kitchen-card";
import { problemMessage } from "@/lib/problem";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

// The guest week in My meals: the offer (once per cycle, dismissable), the
// week itself with both kitchens named, and afterwards a nudge towards a
// rotation — because one good guest week is the rotation's best argument.

export const GUEST_WEEK_ANCHOR = "guest-week";

const COPY = {
  en: {
    offer: "Try a different kitchen next week",
    offerLede: (range: string, credit: string) =>
      `Bored of the same menu? ${range}: pick another kitchen for one week. Your paused week is credited (${credit}) and pays for it; the difference is charged or stays as credit.`,
    sameDays: "Same days, same slots, same address.",
    perMeal: (m: string) => `${m} / meal`,
    week: (m: string) => `${m} for the week`,
    covered: "Covered by your credit",
    due: (m: string) => `${m} to pay`,
    leftover: (m: string) => `${m} stays as credit`,
    start: "Try this kitchen",
    starting: "Setting up…",
    notNow: "Not now",
    pickOne: "Pick a kitchen first.",
    done: (k: string) => `Done — ${k} cooks for you next week.`,
    pay: "Pay the difference",
    guestWeek: "Guest week",
    guestLede: (guest: string, range: string, own: string) =>
      `${range}: ${guest} cooks instead of ${own}. Your usual meals resume right after.`,
    guestPending: "Waiting for payment of the difference.",
    seeGuest: "See the guest week",
    guestOf: (own: string) => `A one-week guest from your ${own} subscription.`,
    backToOwn: "Back to my subscription",
    makeRotation: "Liked the change? Make it a rotation",
    makeRotationLede:
      "Four kitchens, each cooking one week — never the same menu twice in a month.",
    build: "Build my rotation",
  },
  ar: {
    offer: "جرّب مطبخًا مختلفًا الأسبوع القادم",
    offerLede: (range: string, credit: string) =>
      `مللت من القائمة نفسها؟ ${range}: اختر مطبخًا آخر لأسبوع واحد. يُحتسب أسبوعك الموقوف كرصيد (${credit}) ويدفع ثمنه؛ والفرق يُحتسب عليك أو يبقى رصيدًا.`,
    sameDays: "الأيام نفسها، الأوقات نفسها، العنوان نفسه.",
    perMeal: (m: string) => `${m} / وجبة`,
    week: (m: string) => `${m} للأسبوع`,
    covered: "مغطّى من رصيدك",
    due: (m: string) => `${m} للدفع`,
    leftover: (m: string) => `${m} يبقى رصيدًا`,
    start: "جرّب هذا المطبخ",
    starting: "جارٍ التجهيز…",
    notNow: "ليس الآن",
    pickOne: "اختر مطبخًا أولًا.",
    done: (k: string) => `تم — ${k} يطبخ لك الأسبوع القادم.`,
    pay: "ادفع الفرق",
    guestWeek: "أسبوع الضيف",
    guestLede: (guest: string, range: string, own: string) =>
      `${range}: ${guest} يطبخ بدلًا من ${own}. تعود وجباتك المعتادة بعده مباشرة.`,
    guestPending: "بانتظار دفع الفرق.",
    seeGuest: "عرض أسبوع الضيف",
    guestOf: (own: string) => `ضيف لأسبوع واحد من اشتراكك في ${own}.`,
    backToOwn: "العودة إلى اشتراكي",
    makeRotation: "أعجبك التغيير؟ اجعله تناوبًا",
    makeRotationLede:
      "أربعة مطابخ، كل منها يطبخ أسبوعًا — لا تتكرر القائمة مرتين في الشهر.",
    build: "أنشئ تناوبي",
  },
};

const name = (locale: Locale, en: string, ar: string | null) =>
  (locale === "ar" && ar) || en;

export function GuestWeekCard({
  s,
  locale,
  onDone,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
  onDone: () => Promise<unknown>;
}) {
  const t = COPY[locale];
  const money = (n: number, c: string) =>
    formatMoney({ amount: n, currency: c }, locale);
  const own = name(locale, s.kitchen_name, s.kitchen_name_ar);

  if (s.guest_of) {
    const g = s.guest_of;
    return (
      <section className="ms-callout ms-guest" id={GUEST_WEEK_ANCHOR}>
        <strong>{t.guestWeek}</strong>
        <p>{t.guestOf(name(locale, g.kitchen_name, g.kitchen_name_ar))}</p>
        <Link
          className="ms-link"
          href={`/${locale}/subscriptions/${g.subscription_id}`}
        >
          {t.backToOwn}
        </Link>
      </section>
    );
  }

  if (s.guest_week) {
    const g = s.guest_week;
    const guest = name(locale, g.kitchen_name, g.kitchen_name_ar);
    return (
      <section className="ms-callout ms-guest" id={GUEST_WEEK_ANCHOR}>
        <strong>{t.guestWeek}</strong>
        <p>
          {t.guestLede(
            guest,
            formatRange(locale, g.week_start, g.week_end),
            own,
          )}
        </p>
        {g.status === "pending_payment" && (
          <p className="ms-sub">{t.guestPending}</p>
        )}
        <div className="ms-actions">
          <Link
            className="ms-cta ms-cta-quiet"
            href={`/${locale}/subscriptions/${g.guest_subscription_id}`}
          >
            {g.status === "pending_payment" ? t.pay : t.seeGuest}
          </Link>
        </div>
        {g.completed && (
          <div className="ms-guest-next">
            <strong>{t.makeRotation}</strong>
            <p className="ms-sub">{t.makeRotationLede}</p>
            <Link className="ms-cta" href={`/${locale}/rotation`}>
              {t.build}
            </Link>
          </div>
        )}
      </section>
    );
  }

  const offer = s.guest_week_offer;
  if (!offer?.eligible || !offer.week_start || !offer.week_end) return null;
  return <Offer s={s} locale={locale} onDone={onDone} money={money} />;
}

function Offer({
  s,
  locale,
  onDone,
  money,
}: {
  s: MySubscriptionDetail;
  locale: Locale;
  onDone: () => Promise<unknown>;
  money: (n: number, c: string) => string;
}) {
  const t = COPY[locale];
  const offer = s.guest_week_offer!;
  const [planId, setPlanId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [doneWith, setDoneWith] = useState<string | null>(null);
  const key = useIdempotencyKey();
  const start = useMutation({
    mutationFn: (id: string) =>
      startGuestWeek(s.id, id, { headers: key.headers() }),
    onSuccess: async (_res, id) => {
      key.reset();
      const o = offer.options.find((x) => x.plan_id === id);
      setDoneWith(o ? name(locale, o.kitchen_name, o.kitchen_name_ar) : "");
      await onDone();
    },
    onError: (err) => setError(problemMessage(err, locale)),
  });
  const dismiss = useMutation({
    mutationFn: () => dismissGuestWeek(s.id),
    onSuccess: onDone,
    onError: (err) => setError(problemMessage(err, locale)),
  });
  const busy = start.isPending || dismiss.isPending;
  const range = formatRange(locale, offer.week_start!, offer.week_end!);

  if (doneWith !== null) {
    return (
      <section className="ms-callout ms-guest" id={GUEST_WEEK_ANCHOR}>
        <p className="ms-ok">{t.done(doneWith)}</p>
      </section>
    );
  }
  return (
    <section className="ms-callout ms-guest" id={GUEST_WEEK_ANCHOR}>
      <strong>{t.offer}</strong>
      <p>{t.offerLede(range, money(offer.credit_minor, offer.currency))}</p>
      <p className="ms-sub">{t.sameDays}</p>
      <ul
        className="ms-list ms-guest-options"
        role="radiogroup"
        aria-label={t.offer}
      >
        {offer.options.map((o) => (
          <li key={o.plan_id}>
            <button
              type="button"
              role="radio"
              aria-checked={planId === o.plan_id}
              className={`ms-option${planId === o.plan_id ? " on" : ""}`}
              onClick={() => setPlanId(o.plan_id)}
              disabled={busy}
            >
              <span>
                <strong dir="auto">
                  {name(locale, o.kitchen_name, o.kitchen_name_ar)}
                </strong>
                {o.cuisines.length > 0 && (
                  <span className="ms-sub">
                    {" "}
                    · {o.cuisines.map((c) => tagLabel(c, locale)).join(" · ")}
                  </span>
                )}
                <span className="ms-sub">
                  <br />
                  {t.perMeal(money(o.per_meal_minor, o.currency))} ·{" "}
                  {t.week(money(o.week_minor, o.currency))}
                </span>
                <span className="ms-sub">
                  <br />
                  <Split o={o} t={t} money={money} />
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
      <div className="ms-actions">
        <button
          type="button"
          className="ms-cta"
          disabled={busy || !planId}
          title={planId ? undefined : t.pickOne}
          onClick={() => {
            if (!planId) return;
            setError(null);
            start.mutate(planId);
          }}
        >
          {start.isPending ? t.starting : t.start}
        </button>
        <button
          type="button"
          className="ms-cta ms-cta-quiet"
          disabled={busy}
          onClick={() => dismiss.mutate()}
        >
          {t.notNow}
        </button>
      </div>
    </section>
  );
}

function Split({
  o,
  t,
  money,
}: {
  o: GuestWeekOption;
  t: (typeof COPY)["en"];
  money: (n: number, c: string) => string;
}) {
  if (o.due_minor > 0) return <>{t.due(money(o.due_minor, o.currency))}</>;
  if (o.leftover_credit_minor > 0)
    return <>{t.leftover(money(o.leftover_credit_minor, o.currency))}</>;
  return <>{t.covered}</>;
}
