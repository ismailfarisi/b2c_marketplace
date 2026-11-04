import type { MyDelivery } from "@/lib/customer-api";

import { formatDay, type Locale } from "./shell";

const LABEL: Record<Locale, Record<string, string>> = {
  en: {
    scheduled: "Scheduled",
    dispatched: "Picked up",
    en_route: "Out for delivery",
    arrived: "Arrived",
    completed: "Delivered",
    failed: "Delivery failed",
  },
  ar: {
    scheduled: "مجدولة",
    dispatched: "استلمها السائق",
    en_route: "في الطريق إليك",
    arrived: "وصلت",
    completed: "تم التوصيل",
    failed: "تعذّر التوصيل",
  },
};

const COPY = {
  en: { recent: "Recent deliveries", unknown: "Updating" },
  ar: { recent: "التوصيلات الأخيرة", unknown: "قيد التحديث" },
};

// Reuses the subscription status colours: green done, accent moving, red failed.
const TONE: Record<string, string> = {
  completed: "active",
  dispatched: "pending_payment",
  en_route: "pending_payment",
  arrived: "pending_payment",
  failed: "cancelled",
};

export function deliveryLabel(status: string, locale: Locale): string {
  return LABEL[locale][status] ?? COPY[locale].unknown;
}

/** One meal's delivery state, once a delivery exists for it. */
export function DeliveryStatus({
  status,
  locale,
}: {
  status: string;
  locale: Locale;
}) {
  const tone = TONE[status];
  return (
    <span className={`ms-status${tone ? ` ms-status-${tone}` : ""}`}>
      {deliveryLabel(status, locale)}
    </span>
  );
}

/** The last two weeks' deliveries and how each went. */
export function RecentDeliveries({
  items,
  locale,
  slotLabel,
}: {
  items: MyDelivery[];
  locale: Locale;
  slotLabel: (slot: string) => string;
}) {
  if (items.length === 0) return null;
  return (
    <section>
      <h2>{COPY[locale].recent}</h2>
      <ul className="ms-pauses">
        {items.map((d) => (
          <li key={`${d.date}-${d.meal_slot}`}>
            {formatDay(locale, d.date)} · {slotLabel(d.meal_slot)}{" "}
            <DeliveryStatus status={d.status} locale={locale} />
          </li>
        ))}
      </ul>
    </section>
  );
}
