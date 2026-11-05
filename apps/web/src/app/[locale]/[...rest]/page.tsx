import { notFound } from "next/navigation";

/**
 * Any URL under a locale that no route matches. The locale layout is the root
 * layout, so without this Next renders its own unstyled 404 rather than
 * [locale]/not-found.tsx; calling notFound() here hands it to that page.
 */
export default function UnknownPage() {
  notFound();
}
