"use client";

import { ApiError } from "@marketplace/api-client";
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useRef, useState } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale } from "@/components/meals/shell";
import { listMyTickets, openTicket } from "@/lib/customer-api";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

import { SUPPORT_COPY } from "./copy";
import { formatWhen, useDraft } from "./draft";
import { FieldError, SendError } from "./send-error";

export default function SupportPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = SUPPORT_COPY[locale];
  const router = useRouter();
  const qc = useQueryClient();
  const q = useInfiniteQuery({
    queryKey: ["me", "support"],
    queryFn: ({ pageParam }) => listMyTickets(pageParam || undefined),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    retry: false,
  });
  const [subject, setSubject, clearSubject] = useDraft("new:subject");
  const [body, setBody, clearBody] = useDraft("new:body");
  const [missing, setMissing] = useState<{ subject?: boolean; body?: boolean }>({});
  const idem = useIdempotencyKey();
  const busy = useRef(false);
  const open = useMutation({
    // Fail fast offline rather than pausing and firing later behind the
    // person's back, after they've given up and typed it again.
    networkMode: "always",
    mutationFn: () =>
      openTicket(
        { subject: subject.trim(), body: body.trim() },
        { headers: idem.headers() },
      ),
    onSuccess: (thread) => {
      idem.reset();
      clearSubject();
      clearBody();
      qc.invalidateQueries({ queryKey: ["me", "support"] });
      router.push(`/${locale}/support/${thread.id}`);
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const signedOut = q.error instanceof ApiError && q.error.status === 401;
  const tickets = q.data?.pages.flatMap((p) => p.items) ?? [];

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
        ) : q.isPending ? (
          <p className="ms-note" aria-busy="true">
            {t.loading}
          </p>
        ) : (
          <>
            <form
              className="ms-support-form"
              noValidate
              onSubmit={(e) => {
                e.preventDefault();
                if (busy.current || open.isPending) return;
                const m = { subject: !subject.trim(), body: !body.trim() };
                setMissing(m);
                if (m.subject || m.body) return;
                busy.current = true;
                open.mutate();
              }}
            >
              <h2>{t.newTitle}</h2>
              <label>
                {t.subject}
                <input
                  value={subject}
                  maxLength={200}
                  onChange={(e) => {
                    setSubject(e.target.value);
                    idem.reset();
                  }}
                  aria-invalid={missing.subject || undefined}
                />
                <FieldError
                  error={open.error}
                  field="subject"
                  local={missing.subject ? t.needSubject : null}
                  locale={locale}
                />
              </label>
              <label>
                {t.message}
                <textarea
                  value={body}
                  maxLength={4000}
                  rows={5}
                  onChange={(e) => {
                    setBody(e.target.value);
                    idem.reset();
                  }}
                  aria-invalid={missing.body || undefined}
                />
                <FieldError
                  error={open.error}
                  field="body"
                  local={missing.body ? t.needMessage : null}
                  locale={locale}
                />
              </label>
              <SendError
                error={open.error}
                paused={open.isPaused}
                locale={locale}
                next={`/${locale}/support`}
                fields={["subject", "body"]}
              />
              <button
                className="ms-btn"
                type="submit"
                disabled={open.isPending && !open.isPaused}
              >
                {open.isPending && !open.isPaused ? t.sending : t.send}
              </button>
            </form>

            <h2>{t.mine}</h2>
            {q.isError ? (
              <LoadError
                error={q.error}
                onRetry={() => void q.refetch()}
                locale={locale}
              />
            ) : null}
            {q.isSuccess && tickets.length === 0 ? <p>{t.empty}</p> : null}
            <ul className="ms-support-list">
              {tickets.map((tk) => (
                <li key={tk.id}>
                  <Link href={`/${locale}/support/${tk.id}`}>
                    <strong dir="auto">{tk.subject}</strong>
                    <span className={`ms-support-status is-${tk.status}`}>
                      {t.status[tk.status]}
                    </span>
                    <small>{formatWhen(locale, tk.updated_at)}</small>
                  </Link>
                </li>
              ))}
            </ul>
            {q.hasNextPage ? (
              <button
                type="button"
                className="ms-link"
                onClick={() => void q.fetchNextPage()}
                disabled={q.isFetchingNextPage}
              >
                {q.isFetchingNextPage ? t.loadingOlder : t.older}
              </button>
            ) : null}
            {q.isFetchNextPageError ? (
              <p className="ms-error">{t.failed}</p>
            ) : null}
          </>
        )}
      </div>
    </CustomerShell>
  );
}
