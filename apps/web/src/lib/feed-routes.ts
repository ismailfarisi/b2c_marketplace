/**
 * Where a resolved route goes on the web: absolute URLs as-is, paths under the
 * reader's locale. The resolver speaks the apps' route names; the two that have
 * a different web page are mapped here — a branch is a kitchen page, and an
 * inquiry carries the reel and action so the lead is attributed.
 */
export function hrefFor(
  route: string,
  locale: "en" | "ar",
  source?: { reelId: string; actionId: string },
): string {
  if (/^https?:\/\//.test(route)) return route;
  let path = route.replace(/^\/(en|ar)(?=\/|$)/, "");
  if (!path.startsWith("/")) path = `/${path}`;
  path = path.replace(/^\/branches\/([^/?#]+)/, "/kitchens/$1");
  if (source && /^\/listings\/[^/?#]+\/inquire$/.test(path)) {
    path += `?reel=${encodeURIComponent(source.reelId)}&action=${encodeURIComponent(source.actionId)}`;
  }
  return `/${locale}${path}`;
}

/** Pages that need an account; a signed-out tap goes to sign-in first. */
export const NEEDS_ACCOUNT = /^\/(en|ar)\/(listings\/[^/]+\/inquire|messages)(\/|\?|$)/;
