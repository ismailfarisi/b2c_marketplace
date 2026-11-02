"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useState } from "react";

import { CustomerShell, asLocale, formatDay } from "@/components/meals/shell";
import { listMyTickets, openTicket } from "@/lib/customer-api";

import { SUPPORT_COPY } from "./copy";

export default function SupportPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = SUPPORT_COPY[locale];
  const router = useRouter();
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["me", "support"],
    queryFn: listMyTickets,
    retry: false,
  });
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const open = useMutation({
    mutationFn: () => openTicket({ subject, body }),
    onSuccess: (thread) => {
      qc.invalidateQueries({ queryKey: ["me", "support"] });
      router.push(`/${locale}/support/${thread.id}`);
    },
  });
  const signedOut = q.error instanceof ApiError && q.error.status === 401;

  return (
    <CustomerShell locale={locale}>
      <div className="ms-support">
        <h1>{t.title}</h1>
        <p>{t.lede}</p>
        {signedOut ? (
          <Link
            className="ms-btn"
            href={`/${locale}/signin?next=/${locale}/support`}
          >
            {t.signIn}
          </Link>
        ) : (
          <>
            <form
              className="ms-support-form"
              onSubmit={(e) => {
                e.preventDefault();
                if (subject.trim() && body.trim()) open.mutate();
              }}
            >
              <h2>{t.newTitle}</h2>
              <label>
                {t.subject}
                <input
                  value={subject}
                  maxLength={200}
                  onChange={(e) => setSubject(e.target.value)}
                  required
                />
              </label>
              <label>
                {t.message}
                <textarea
                  value={body}
                  maxLength={4000}
                  rows={5}
                  onChange={(e) => setBody(e.target.value)}
                  required
                />
              </label>
              {open.error ? (
                <p className="ms-error">{open.error.message}</p>
              ) : null}
              <button
                className="ms-btn"
                type="submit"
                disabled={open.isPending}
              >
                {open.isPending ? t.sending : t.send}
              </button>
            </form>

            <h2>{t.mine}</h2>
            {q.isError ? <p className="ms-error">{t.failed}</p> : null}
            {q.data && q.data.items.length === 0 ? <p>{t.empty}</p> : null}
            <ul className="ms-support-list">
              {q.data?.items.map((tk) => (
                <li key={tk.id}>
                  <Link href={`/${locale}/support/${tk.id}`}>
                    <strong dir="auto">{tk.subject}</strong>
                    <span className={`ms-support-status is-${tk.status}`}>
                      {t.status[tk.status]}
                    </span>
                    <small>
                      {formatDay(locale, tk.updated_at.slice(0, 10))}
                    </small>
                  </Link>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </CustomerShell>
  );
}
