"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useRef, useState } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale } from "@/components/meals/shell";
import { getMyTicket, replyToTicket } from "@/lib/customer-api";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

import { SUPPORT_COPY } from "../copy";
import { formatWhen, useDraft } from "../draft";
import { FieldError, SendError } from "../send-error";

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
  const [body, setBody, clearBody] = useDraft(`reply:${p.ticketId}`);
  const [missing, setMissing] = useState(false);
  const idem = useIdempotencyKey();
  const busy = useRef(false);
  const reply = useMutation({
    networkMode: "always",
    mutationFn: () =>
      replyToTicket(p.ticketId, body.trim(), { headers: idem.headers() }),
    onSuccess: (thread) => {
      idem.reset();
      clearBody();
      qc.setQueryData(key, thread);
      qc.invalidateQueries({ queryKey: ["me", "support"], exact: true });
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const here = `/${locale}/support/${p.ticketId}`;
  const signedOut = q.error instanceof ApiError && q.error.status === 401;
  const notFound = q.error instanceof ApiError && q.error.status === 404;

  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/support`, label: t.back }}
    >
      <div className="ms-support">
        {signedOut ? (
          <p>
            <Link
              className="ms-btn"
              href={`/${locale}/signin?next=${encodeURIComponent(here)}`}
            >
              {t.signIn}
            </Link>
          </p>
        ) : notFound ? (
          <p className="ms-empty" role="alert">
            {t.notFound} <Link href={`/${locale}/support`}>{t.back}</Link>
          </p>
        ) : q.isError ? (
          <LoadError
            error={q.error}
            onRetry={() => void q.refetch()}
            locale={locale}
          />
        ) : q.isPending ? (
          <p className="ms-note" aria-busy="true">
            {t.loading}
          </p>
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
                    {formatWhen(locale, m.created_at, true)}
                  </small>
                  <p dir="auto">{m.body}</p>
                </li>
              ))}
            </ol>
            <form
              className="ms-support-form"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                if (busy.current || reply.isPending) return;
                setMissing(!body.trim());
                if (!body.trim()) return;
                busy.current = true;
                reply.mutate();
              }}
            >
              <label>
                {t.reply}
                <textarea
                  value={body}
                  maxLength={4000}
                  rows={4}
                  onChange={(e) => {
                    setBody(e.target.value);
                    idem.reset();
                  }}
                  aria-invalid={missing || undefined}
                />
                <FieldError
                  error={reply.error}
                  field="body"
                  local={missing ? t.needMessage : null}
                  locale={locale}
                />
              </label>
              <SendError
                error={reply.error}
                paused={reply.isPaused}
                locale={locale}
                next={here}
                fields={["body"]}
              />
              <button
                className="ms-btn"
                type="submit"
                disabled={reply.isPending && !reply.isPaused}
              >
                {reply.isPending && !reply.isPaused ? t.sending : t.send}
              </button>
            </form>
          </>
        ) : null}
      </div>
    </CustomerShell>
  );
}
