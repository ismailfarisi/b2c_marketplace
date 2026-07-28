"use client";

import {
  decideKyc,
  getKycQueue,
  getPayoutAccountQueue,
  verifyPayoutAccount,
} from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Identity checks and bank accounts.
 *
 * Both on one screen because they gate the same thing — money leaving the
 * platform — and because approving one without the other achieves nothing. A
 * vendor with a verified identity and an unverified account cannot be paid, and
 * a reviewer who does not see both halves cannot tell which half is missing.
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
  const accountItems = accounts.data?.items ?? [];

  return (
    <>
      <h1>Identity checks</h1>
      <p className="muted">
        Two-day SLA. This blocks a vendor&rsquo;s first payout, so a case sitting
        here is a vendor who has delivered orders and not been paid for them.
      </p>

      <QueueState
        isPending={cases.isPending}
        error={cases.error}
        count={caseItems.length}
        empty="No cases awaiting review."
      />

      <ul className="stack">
        {caseItems.map((c) => {
          const expired = c.documents.filter((d) => d.expired);
          const unverified = c.documents.filter((d) => d.status !== "verified");

          return (
            <li key={c.id} className="card">
              <div className="row spread">
                <strong dir="auto">{c.subject_name || c.subject_id}</strong>
                <div className="row">
                  <span className="pill">{c.subject_type}</span>
                  <span className="pill">{c.tier}</span>
                </div>
              </div>

              {expired.length > 0 && (
                <p className="bad">
                  {expired.length === 1 ? "A document has" : `${expired.length} documents have`}{" "}
                  expired. Approving sets the case expiry to the earliest document
                  expiry, so this case would be approved and immediately lapsed.
                </p>
              )}

              <div className="scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Document</th>
                      <th>Last 4</th>
                      <th>Expires</th>
                      <th>Status</th>
                      <th>Reference</th>
                    </tr>
                  </thead>
                  <tbody>
                    {c.documents.map((d) => (
                      <tr key={d.id}>
                        <td>{d.kind.replace(/_/g, " ")}</td>
                        <td className="mono">{d.number_last4 ?? "—"}</td>
                        <td className={d.expired ? "bad" : ""}>{d.expires_on ?? "—"}</td>
                        <td>{d.status}</td>
                        {/* A reference into object storage, never an image src.
                            A scan of an Emirates ID is the highest-value object
                            in this system to an attacker; it is fetched through
                            a short-lived signed link, and that endpoint is not
                            built — see the note below. */}
                        <td className="mono muted">{d.document_ref ?? "—"}</td>
                      </tr>
                    ))}
                    {c.documents.length === 0 && (
                      <tr>
                        <td colSpan={5} className="muted">
                          No documents uploaded. There is no upload endpoint yet —
                          documents arrive out of band.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>

              {unverified.length > 0 && (
                <p className="warn">
                  {unverified.length} document
                  {unverified.length === 1 ? " is" : "s are"} not marked verified.
                </p>
              )}

              <Decide onDecide={(approve, reason) => decideKyc(c.id, approve, reason)} />
            </li>
          );
        })}
      </ul>

      <h2>Bank accounts</h2>
      <p className="muted">
        Every IBAN change re-enters verification, deliberately. &ldquo;The vendor
        changed their bank details&rdquo; is the most common payout-fraud pattern
        there is, and an account that inherited the previous one&rsquo;s verified
        state is exactly what an attacker wants.
      </p>

      <QueueState
        isPending={accounts.isPending}
        error={accounts.error}
        count={accountItems.length}
        empty="No accounts awaiting verification."
      />

      <ul className="stack">
        {accountItems.map((a) => (
          <li key={a.id} className="card">
            <div className="row spread">
              <span className="pill">{a.owner_type}</span>
              <span className="mono">{a.iban}</span>
            </div>

            <div className="scroll">
              <table>
                <tbody>
                  <tr>
                    <th>Legal name on record</th>
                    <td dir="auto">{a.owner_name || <span className="muted">unknown</span>}</td>
                  </tr>
                  <tr>
                    <th>Name on the account</th>
                    <td dir="auto">{a.holder_name}</td>
                  </tr>
                  <tr>
                    <th>Bank</th>
                    <td>{a.bank_name ?? "—"}</td>
                  </tr>
                </tbody>
              </table>
            </div>

            <Decide
              reasonRequired={false}
              approveLabel="Names match — verify"
              rejectLabel="Reject"
              onDecide={(approve) => verifyPayoutAccount(a.id, approve)}
            />
          </li>
        ))}
      </ul>
    </>
  );
}
