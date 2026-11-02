"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { CustomerShell, asLocale } from "@/components/meals/shell";
import { getMyTicket, replyToTicket } from "@/lib/customer-api";

import { SUPPORT_COPY } from "../copy";

export default function TicketPage({
  params,
}: {
  params: Promise<{ locale: string; ticketId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = SUPPORT_COPY[locale];
  const qc = useQueryClient();
  const key = ["me", "support", p.ticketId];
  const q = useQuery({
    queryKey: key,
    queryFn: () => getMyTicket(p.ticketId),
    retry: false,
  });
  const [body, setBody] = useState("");
  const reply = useMutation({
    mutationFn: () => replyToTicket(p.ticketId, body),
    onSuccess: (thread) => {
      setBody("");
      qc.setQueryData(key, thread);
      qc.invalidateQueries({ queryKey: ["me", "support"], exact: true });
    },
  });
  const signedOut = q.error instanceof ApiError && q.error.status === 401;
  const time = new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-AE", {
    dateStyle: "medium",
    timeStyle: "short",
  });

  return (
    <CustomerShell locale={locale}>
      <div className="ms-support">
        <Link href={`/${locale}/support`}>← {t.back}</Link>
        {signedOut ? (
          <p>
            <Link
              className="ms-btn"
              href={`/${locale}/signin?next=/${locale}/support/${p.ticketId}`}
            >
              {t.signIn}
            </Link>
          </p>
        ) : null}
        {q.isError && !signedOut ? (
          <p className="ms-error">{t.failed}</p>
        ) : null}
        {q.data ? (
          <>
            <h1 dir="auto">{q.data.subject}</h1>
            <span className={`ms-support-status is-${q.data.status}`}>
              {t.status[q.data.status]}
            </span>
            <ol className="ms-support-thread">
              {q.data.messages.map((m) => (
                <li key={m.id} className={m.from_staff ? "is-staff" : "is-me"}>
                  <small>
                    {m.from_staff ? t.staff : t.you} ·{" "}
                    {time.format(new Date(m.created_at))}
                  </small>
                  <p dir="auto">{m.body}</p>
                </li>
              ))}
            </ol>
            <form
              className="ms-support-form"
              onSubmit={(e) => {
                e.preventDefault();
                if (body.trim()) reply.mutate();
              }}
            >
              <label>
                {t.reply}
                <textarea
                  value={body}
                  maxLength={4000}
                  rows={4}
                  onChange={(e) => setBody(e.target.value)}
                  required
                />
              </label>
              {reply.error ? (
                <p className="ms-error">{reply.error.message}</p>
              ) : null}
              <button
                className="ms-btn"
                type="submit"
                disabled={reply.isPending}
              >
                {reply.isPending ? t.sending : t.send}
              </button>
            </form>
          </>
        ) : null}
      </div>
    </CustomerShell>
  );
}
