"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { messageOf } from "@/components/admin/detail";
import { QueuePanel, Tag, since, useQueueData } from "@/components/admin/table";
import {
  getSupportQueue,
  getSupportTicket,
  replySupportTicket,
  setSupportStatus,
  type AdminSupportTicket,
} from "@/lib/admin-api";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

/**
 * Customer help tickets. Open (customer waiting) sorts first, oldest first;
 * a reply marks the ticket answered and pushes the customer.
 */
export default function SupportQueue() {
  const [filter, setFilter] = useState<"" | "closed">("");
  const { data, isPending, error, refetch } = useQueueData(
    filter ? `support-${filter}` : "support",
    () => getSupportQueue(filter),
  );
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Open tickets are customers waiting on us. Reply here; the customer gets
        a push and sees the reply in the app.
      </p>
      <p className="console-lede">
        <label>
          Show{" "}
          <select
            value={filter}
            onChange={(e) => setFilter(e.target.value as "" | "closed")}
          >
            <option value="">Open and answered</option>
            <option value="closed">Closed</option>
          </select>
        </label>
      </p>
      <QueuePanel
        title="Tickets"
        hint={filter ? "closed, most recent first" : "open first, oldest first"}
        columns={[
          { label: "Subject", className: "grow" },
          { label: "Customer" },
          { label: "Status" },
          { label: "Updated", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        onRetry={() => void refetch()}
        empty={filter ? "No closed tickets." : "No open tickets."}
        rowKey={(t) => t.id}
        renderRow={(t) => (
          <>
            <td className="grow" dir="auto">
              {t.subject}
            </td>
            <td className="nowrap mono">{t.phone}</td>
            <td className="nowrap">
              <Tag tone={t.status === "open" ? "danger" : undefined}>
                {t.status}
              </Tag>
            </td>
            <td className="num nowrap">{since(t.updated_at)}</td>
          </>
        )}
        renderDetail={(t, close) => <Ticket ticket={t} onClose={close} />}
      />
    </>
  );
}

function Ticket({
  ticket,
  onClose,
}: {
  ticket: AdminSupportTicket;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const key = ["admin", "support", ticket.id];
  const q = useQuery({
    queryKey: key,
    queryFn: () => getSupportTicket(ticket.id),
  });
  const [body, setBody] = useState("");
  const idem = useIdempotencyKey();
  const busy = useRef(false);
  const reply = useMutation({
    networkMode: "always",
    mutationFn: () => replySupportTicket(ticket.id, body.trim(), idem.headers()),
    onSuccess: (thread) => {
      idem.reset();
      setBody("");
      qc.setQueryData(key, thread);
      void qc.invalidateQueries({ queryKey: ["admin", "support"], exact: true });
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const status = useMutation({
    networkMode: "always",
    mutationFn: (to: "open" | "closed") => setSupportStatus(ticket.id, to),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["admin"] });
      onClose();
    },
  });
  const closed = (q.data?.status ?? ticket.status) === "closed";

  if (q.isPending) {
    return (
      <div className="console-thread" aria-busy="true">
        <p className="console-lede">Loading thread…</p>
      </div>
    );
  }
  if (q.isError) {
    return (
      <div className="console-thread">
        <div className="state error" role="alert">
          <div className="headline">Could not load this ticket.</div>
          {messageOf(q.error)}{" "}
          <button type="button" onClick={() => void q.refetch()}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="console-thread">
      {q.data.messages.map((m) => (
        <div
          key={m.id}
          className={m.from_staff ? "console-msg is-staff" : "console-msg"}
        >
          <small>
            {m.from_staff ? "Support" : "Customer"} · {since(m.created_at)}
          </small>
          <p dir="auto">{m.body}</p>
        </div>
      ))}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (busy.current || reply.isPending || !body.trim()) return;
          busy.current = true;
          reply.mutate();
        }}
      >
        <textarea
          value={body}
          rows={4}
          maxLength={4000}
          placeholder="Reply to the customer"
          onChange={(e) => {
            setBody(e.target.value);
            idem.reset();
          }}
        />
        {reply.error ? (
          <p className="console-error" role="alert">
            {messageOf(reply.error)}
          </p>
        ) : null}
        {status.error ? (
          <p className="console-error" role="alert">
            Could not {closed ? "reopen" : "close"} the ticket: {messageOf(status.error)}
          </p>
        ) : null}
        <button type="submit" disabled={reply.isPending || !body.trim()}>
          {reply.isPending ? "Sending…" : "Send reply"}
        </button>
        {closed ? (
          <button
            type="button"
            onClick={() => status.mutate("open")}
            disabled={status.isPending}
          >
            Reopen ticket
          </button>
        ) : (
          <button
            type="button"
            onClick={() => {
              if (window.confirm("Close this ticket? The customer can still reply to reopen it."))
                status.mutate("closed");
            }}
            disabled={status.isPending}
          >
            Close ticket
          </button>
        )}
      </form>
    </div>
  );
}
