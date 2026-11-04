/**
 * The first start date whose delivery cutoff is still ahead, in the kitchen's
 * timezone (Asia/Dubai unless given), as YYYY-MM-DD.
 *
 * Mirrors the API's subscriptions.EarliestStart and the apps' MealsViewModel:
 * a day is open while now is before its midnight minus the cutoff. Computed in
 * the kitchen's zone, not the browser's, so a visitor abroad gets the same day.
 */
export function earliestStart(
  cutoffHours: number,
  timeZone = "Asia/Dubai",
  now: Date = new Date(),
): string {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23",
    })
      .formatToParts(now)
      .map((p) => [p.type, p.value]),
  );
  const minutes = Number(parts.hour) * 60 + Number(parts.minute);
  const days = Math.floor((cutoffHours * 60 + minutes) / 1440) + 1;
  const d = new Date(
    Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day) + days),
  );
  return d.toISOString().slice(0, 10);
}
