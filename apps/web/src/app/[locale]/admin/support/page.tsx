"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { QueuePanel, Tag, since, useQueueData } from "@/components/admin/table";
import {
  getSupportQueue,
  getSupportTicket,
  replySupportTicket,
  setSupportStatus,
  type AdminSupportTicket,
} from "@/lib/admin-api";

/**
 * Customer help tickets. Open (customer waiting) sorts first, oldest first;
 * a reply marks the ticket answered and pushes the customer.
 */
export default function SupportQueue() {
  const { data, isPending, error } = useQueueData("support", () =>
    getSupportQueue(),
  );
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Open tickets are customers waiting on us. Reply here; the customer gets
        a push and sees the reply in the app.
      </p>
      <QueuePanel
        title="Tickets"
        hint="open first, oldest first"
        columns={[
          { label: "Subject", className: "grow" },
          { label: "Customer" },
          { label: "Status" },
          { label: "Updated", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="No open tickets."
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
  const reply = useMutation({
    mutationFn: () => replySupportTicket(ticket.id, body),
    onSuccess: (thread) => {
      setBody("");
      qc.setQueryData(key, thread);
      void qc.invalidateQueries({
        queryKey: ["admin", "support"],
        exact: true,
      });
    },
  });
  const close = useMutation({
    mutationFn: () => setSupportStatus(ticket.id, "closed"),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["admin"] });
      onClose();
    },
  });

  return (
    <div className="console-thread">
      {q.data?.messages.map((m) => (
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
          if (body.trim()) reply.mutate();
        }}
      >
        <textarea
          value={body}
          rows={4}
          maxLength={4000}
          placeholder="Reply to the customer"
          onChange={(e) => setBody(e.target.value)}
        />
        {reply.error ? (
          <p className="console-error">{reply.error.message}</p>
        ) : null}
        <button type="submit" disabled={reply.isPending || !body.trim()}>
          Send reply
        </button>
        <button
          type="button"
          onClick={() => close.mutate()}
          disabled={close.isPending}
        >
          Close ticket
        </button>
      </form>
    </div>
  );
}
