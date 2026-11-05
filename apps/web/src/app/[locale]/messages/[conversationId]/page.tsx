"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useEffect, useRef, useState } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale, signInHref } from "@/components/meals/shell";
import { CHAT_COPY } from "@/components/chat-copy";
import {
  listMessages,
  markConversationRead,
  sendMessage,
} from "@/lib/chat-api";
import { networkKind, problemMessage } from "@/lib/problem";

/** One buyer ↔ seller conversation. The draft survives a failed send. */
export default function ConversationPage({
  params,
}: {
  params: Promise<{ locale: string; conversationId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = CHAT_COPY[locale];
  const qc = useQueryClient();
  const key = ["conversation", p.conversationId];
  const q = useQuery({
    queryKey: key,
    queryFn: () => listMessages(p.conversationId),
    retry: false,
  });
  const [body, setBody] = useState("");
  const [notice, setNotice] = useState<string | null>(null);
  // isPending only disables the button after a re-render, so a double-click
  // would post the message twice; the ref closes that gap synchronously.
  const busy = useRef(false);

  useEffect(() => {
    if (q.data)
      void markConversationRead(p.conversationId).catch(() => undefined);
  }, [q.data, p.conversationId]);

  const send = useMutation({
    // Fail fast offline instead of pausing and posting later, after the
    // person has given up and typed it again.
    networkMode: "always",
    mutationFn: () => sendMessage(p.conversationId, body.trim()),
    onSuccess: (m) => {
      setBody("");
      setNotice(
        m.state === "held"
          ? t.held
          : m.state === "redacted"
            ? t.redacted
            : null,
      );
      void qc.invalidateQueries({ queryKey: key });
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const network = networkKind(send.error);

  const here = `/${locale}/messages/${p.conversationId}`;
  const signedOut = q.error instanceof ApiError && q.error.status === 401;
  const notFound = q.error instanceof ApiError && q.error.status === 404;
  const items = q.data?.items ?? [];

  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/feed`, label: t.backToFeed }}
    >
      <div className="ms-support">
        <h1>{t.threadTitle}</h1>
        {signedOut ? (
          <p>
            <Link className="ms-btn" href={signInHref(locale, here)}>
              {t.signIn}
            </Link>
          </p>
        ) : notFound ? (
          <p className="ms-empty" role="alert">
            {t.notFound}
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
            {items.length === 0 ? <p className="ms-note">{t.empty}</p> : null}
            <ol className="ms-support-thread">
              {items.map((m) => (
                <li key={m.id} className={m.mine ? "is-me" : "is-staff"}>
                  <small>
                    {m.mine ? t.you : t.seller}
                    {m.state === "held" ? ` · ${t.heldShort}` : ""}
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
                if (busy.current || !body.trim()) return;
                busy.current = true;
                send.mutate();
              }}
            >
              <label>
                {t.message}
                <textarea
                  value={body}
                  maxLength={4000}
                  rows={3}
                  onChange={(e) => setBody(e.target.value)}
                />
              </label>
              {send.error ? (
                <p className="ms-error" role="alert">
                  {network
                    ? t[network]
                    : send.error instanceof ApiError && send.error.status < 500
                      ? problemMessage(send.error, locale)
                      : t.sendFailed}
                </p>
              ) : null}
              {notice ? (
                <p className="ms-note" role="status">
                  {notice}
                </p>
              ) : null}
              <button
                className="ms-btn"
                type="submit"
                disabled={send.isPending || !body.trim()}
              >
                {send.isPending ? t.sending : t.send}
              </button>
            </form>
          </>
        ) : null}
      </div>
    </CustomerShell>
  );
}
