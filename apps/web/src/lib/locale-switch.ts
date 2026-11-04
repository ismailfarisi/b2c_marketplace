/**
 * The same page in the other language, keeping the query string. A `next`
 * (where sign-in returns to) is moved to the other language too, so EN→AR on
 * /en/signin?next=/en/rotation lands on /ar/signin?next=/ar/rotation.
 */
export function switchLocaleHref(
  path: string,
  search: string,
  to: "en" | "ar",
): string {
  const swap = (p: string) => p.replace(/^\/(en|ar)(?=\/|$)/, `/${to}`);
  const params = new URLSearchParams(search);
  const next = params.get("next");
  if (next) params.set("next", swap(next));
  const qs = params.toString();
  return swap(path) + (qs ? `?${qs}` : "");
}
