"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { use, type ReactNode } from "react";

/**
 * The staff shell.
 *
 * **English only, and `dir="ltr"` regardless of the locale segment.** That
 * deserves defending, because everything else in this app is bilingual by
 * construction.
 *
 * The customer surfaces are Arabic-first because customers are. This one is used
 * by a moderation team whose working language is the platform's, and whose job
 * is judging content that is *often* Arabic. Those are different requirements:
 * the chrome should be stable and familiar so attention goes to the content, and
 * the content itself renders in whatever language it was written in — a caption
 * inside a card here is `dir="auto"`, which is what makes an Arabic caption read
 * correctly inside an English frame.
 *
 * Localising the chrome would be a translation project with no user asking for
 * it. Localising the *content* was never optional and is done per element.
 */
const SECTIONS = [
  { href: "", label: "Overview" },
  { href: "/reels", label: "Reels" },
  { href: "/comments", label: "Comments" },
  { href: "/messages", label: "Messages" },
  { href: "/listings", label: "Listings" },
  { href: "/appeals", label: "Appeals" },
  { href: "/kyc", label: "KYC" },
  { href: "/campaigns", label: "Campaigns" },
  { href: "/disputes", label: "Disputes" },
];

export default function AdminLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const pathname = usePathname();
  const base = `/${locale}/admin`;

  return (
    <div dir="ltr" className="admin">
      <nav className="admin-nav">
        <strong>Ops</strong>
        {SECTIONS.map((s) => {
          const href = `${base}${s.href}`;
          const active = s.href === "" ? pathname === base : pathname.startsWith(href);
          return (
            <Link key={s.href} href={href} className={active ? "active" : ""}>
              {s.label}
            </Link>
          );
        })}
      </nav>
      <main className="page wide">{children}</main>
    </div>
  );
}
