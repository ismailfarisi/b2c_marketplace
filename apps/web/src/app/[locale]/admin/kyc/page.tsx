"use client";

import {
  decideKyc,
  getKycQueue,
  getPayoutAccountQueue,
  verifyPayoutAccount,
} from "@/lib/admin-api";
import {
  CardState,
  Decide,
  QueuePanel,
  Tag,
  since,
  useQueueData,
} from "@/components/admin/table";

/**
 * Identity checks and bank accounts.
 *
 * Both on one screen because they gate the same thing — money leaving the
 * platform — and because approving one without the other achieves nothing. A
 * vendor with a verified identity and an unverified account cannot be paid, and
 * a reviewer who does not see both halves cannot tell which half is missing.
 *
 * **Cases are cards; accounts are a table.** A case is a set of documents read
 * together — expiry dates against each other, names against a company record —
 * and collapsing that into a row loses the comparison. An account is two names
 * and an IBAN, which is a row.
 *
 * Two guards are rendered rather than computed here:
 *
 * **Expired documents are flagged by the server.** An approval taken against a
 * trade licence that lapsed last week is the failure this exists to make
 * impossible to miss, and it is a date comparison the client should not be the
 * only one making.
 *
 * **The two names sit side by side, uncompared.** Deciding whether "Karama
 * Kitchen L.L.C." and "KARAMA KITCHEN LLC" are the same entity is the reviewer's
 * whole job. A fuzzy match that is right 90% of the time would be wrong exactly
 * where it matters: payout redirection is the most common fraud against a
 * marketplace, and the attacker chooses a name that nearly matches.
 */
export default function KycQueue() {
  const cases = useQueueData("kyc", getKycQueue);
  const accounts = useQueueData("payout-accounts", getPayoutAccountQueue);
  const caseItems = cases.data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Two-day SLA on identity. This blocks a vendor&rsquo;s first payout, so a
        case sitting here is a vendor who has delivered orders and not been paid
        for them.
      </p>

      <CardState
        isPending={cases.isPending}
        error={cases.error}
        count={caseItems.length}
        empty="No cases awaiting review."
      />

      {caseItems.map((c) => {
        const expired = c.documents.filter((d) => d.expired);
        const unverified = c.documents.filter((d) => d.status !== "verified");

        return (
          <section key={c.id} className="panel">
            <div className="panel-head">
              <h2 dir="auto">{c.subject_name || c.subject_id}</h2>
              <Tag>{c.subject_type}</Tag>
              <Tag>{c.tier}</Tag>
              <div style={{ flex: 1 }} />
              <span className="hint">waiting {since(c.created_at)}</span>
            </div>

            {expired.length > 0 && (
              <div className="decide" style={{ borderInlineStartColor: "var(--a-danger)" }}>
                <p className="note danger" style={{ margin: 0 }}>
                  {expired.length === 1
                    ? "A document has expired."
                    : `${expired.length} documents have expired.`}{" "}
                  Approving sets the case expiry to the earliest document expiry,
                  so this case would be approved and immediately lapsed.
                </p>
              </div>
            )}

            <div className="table-wrap">
              <table className="data">
                <thead>
                  <tr>
                    <th>Document</th>
                    <th>Last 4</th>
                    <th>Expires</th>
                    <th>Status</th>
                    <th className="grow">Reference</th>
                  </tr>
                </thead>
                <tbody>
                  {c.documents.map((d) => (
                    <tr key={d.id}>
                      <td className="nowrap">{d.kind.replace(/_/g, " ")}</td>
                      <td className="mono">{d.number_last4 ?? "—"}</td>
                      <td className={d.expired ? "danger nowrap" : "nowrap"}>
                        {d.expires_on ?? "—"}
                      </td>
                      <td>
                        <Tag tone={d.status === "verified" ? "ok" : "warn"}>{d.status}</Tag>
                      </td>
                      {/* A reference into object storage, never an image src. A
                          scan of an Emirates ID is the highest-value object in
                          this system to an attacker; it is fetched through a
                          short-lived signed link, and that endpoint is not
                          built. */}
                      <td className="grow mono faint">{d.document_ref ?? "—"}</td>
                    </tr>
                  ))}
                  {c.documents.length === 0 && (
                    <tr>
                      <td colSpan={5} className="faint">
                        No documents uploaded. There is no upload endpoint yet —
                        documents arrive out of band.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            <Decide onDecide={(approve, reason) => decideKyc(c.id, approve, reason)}>
              {unverified.length > 0 && (
                <p className="note">
                  {unverified.length} document
                  {unverified.length === 1 ? " is" : "s are"} not marked verified.
                </p>
              )}
            </Decide>
          </section>
        );
      })}

      <QueuePanel
        title="Bank accounts awaiting verification"
        hint="every IBAN change re-enters verification"
        columns={[
          { label: "Owner" },
          { label: "Legal name on record", className: "grow" },
          { label: "Name on the account", className: "grow" },
          { label: "IBAN" },
          { label: "Bank" },
          { label: "Waiting", className: "num" },
        ]}
        rows={accounts.data?.items ?? []}
        isPending={accounts.isPending}
        error={accounts.error}
        empty="No accounts awaiting verification."
        rowKey={(a) => a.id}
        renderRow={(a) => (
          <>
            <td className="nowrap">
              <Tag>{a.owner_type}</Tag>
            </td>
            {/* Both names, uncompared and adjacent. The reviewer's entire job is
                deciding whether they are the same entity, and no code here
                should pre-empt that with a fuzzy match. */}
            <td className="grow" dir="auto">
              {a.owner_name || <span className="faint">unknown</span>}
            </td>
            <td className="grow" dir="auto">
              {a.holder_name}
            </td>
            <td className="mono nowrap">{a.iban}</td>
            <td className="nowrap">{a.bank_name ?? "—"}</td>
            <td className="num nowrap">{since(a.created_at)}</td>
          </>
        )}
        renderDetail={(a, close) => (
          <Decide
            reasonRequired={false}
            approveLabel="Names match — verify"
            rejectLabel="Reject"
            onDone={close}
            onDecide={(approve) => verifyPayoutAccount(a.id, approve)}
          >
            <p className="note">
              &ldquo;The vendor changed their bank details&rdquo; is the most
              common payout-fraud pattern there is. An account that inherited the
              previous one&rsquo;s verified state is exactly what an attacker
              wants, which is why every change lands back here.
            </p>
          </Decide>
        )}
      />
    </>
  );
}
