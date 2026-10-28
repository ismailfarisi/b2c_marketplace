type Locale = "en" | "ar";

/** Rating from verified subscribers; renders nothing until a kitchen has one. */
export function RatingBadge({
  rating,
  count,
  locale,
}: {
  rating: number | null;
  count: number;
  locale: Locale;
}) {
  if (rating == null || count === 0) return null;
  const n = rating.toLocaleString(locale === "ar" ? "ar" : "en", {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  });
  const label =
    locale === "ar"
      ? `${n} من 5، ${count} ${count === 1 ? "تقييم" : "تقييمات"} من مشتركين`
      : `${n} out of 5 from ${count} subscriber ${count === 1 ? "rating" : "ratings"}`;
  return (
    <span className="ms-rating" aria-label={label} title={label}>
      <span aria-hidden="true">★</span> {n}{" "}
      <span className="ms-rating-count">({count})</span>
    </span>
  );
}

export function SafetyBadge({ locale }: { locale: Locale }) {
  return (
    <span className="ms-safety">
      {locale === "ar" ? "سلامة الغذاء موثّقة" : "Food safety verified"}
    </span>
  );
}
