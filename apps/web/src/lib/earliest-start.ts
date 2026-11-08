import { ApiError } from "@marketplace/api-client";

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
    Date.UTC(
      Number(parts.year),
      Number(parts.month) - 1,
      Number(parts.day) + days,
    ),
  );
  return d.toISOString().slice(0, 10);
}

/**
 * The first allowed start date a `too_soon` refusal names, or null. The server
 * is the authority on cutoffs; a checkout that was refused moves its date here
 * instead of leaving the customer stuck on a day that can no longer be cooked.
 */
export function earliestFromTooSoon(err: unknown): string | null {
  if (!(err instanceof ApiError)) return null;
  const field = err.problem?.errors?.find((e) => e.code === "too_soon");
  if (!field) return null;
  return /\d{4}-\d{2}-\d{2}/.exec(field.message ?? "")?.[0] ?? null;
}
