-- Settling a won lot, and telling people they have been outbid.
--
-- The auction vertical shipped able to run a lot and decide a winner, and then
-- stopped: `settled` was a status nothing set, and being outbid was visible
-- only to someone happening to look at the screen. Both are the difference
-- between a demo and a product — an auction nobody is told they lost is an
-- auction with one bid.

-- ── settlement ───────────────────────────────────────────────────────────────
--
-- # Why this is not an `orders` row
--
-- The order path in this product is built for fulfilment: it draws capacity,
-- schedules a delivery, moves money to a vendor and issues an invoice on the
-- supplier's TRN. A plate transfer does none of that. The platform is not the
-- supplier, there is nothing to deliver, and the transfer itself happens at an
-- RTA counter that this system will never see.
--
-- What is owed is a *fee* — the platform's commission on a completed auction —
-- and the plate's price is settled directly between the two parties at the
-- counter. Modelling that as an order would mean an order whose total is not
-- what anyone pays, an invoice for a supply that did not happen, and a
-- fulfilment record with no fulfilment. This is a smaller, honest thing.

CREATE TABLE auction_settlements (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auction_id    uuid NOT NULL UNIQUE REFERENCES plate_auctions(id),

    seller_id     uuid NOT NULL REFERENCES users(id),
    winner_id     uuid NOT NULL REFERENCES users(id),

    -- What the lot went for. Copied rather than joined: the auction row can be
    -- corrected by an admin, and a settlement is a record of what was agreed at
    -- the moment the hammer fell.
    hammer_minor  bigint NOT NULL CHECK (hammer_minor > 0),
    currency      text NOT NULL DEFAULT 'AED',

    -- The platform's cut, and who owes it.
    --
    -- Charged to the *seller*, which is the convention at every plate auction in
    -- this market and is also the only workable one here: the winner's
    -- obligation is to the seller, not to us, and a buyer's premium would mean
    -- collecting from someone who has no account balance and no reason to keep
    -- one.
    commission_bp     integer NOT NULL CHECK (commission_bp >= 0),
    commission_minor  bigint NOT NULL CHECK (commission_minor >= 0),
    vat_minor         bigint NOT NULL DEFAULT 0 CHECK (vat_minor >= 0),

    -- due     — the hammer fell, nobody has paid.
    -- paid    — the commission has been collected.
    -- waived  — an operator wrote it off; the reason is required.
    -- void    — the sale fell through. Distinct from waived: nothing is owed
    --           because nothing happened, rather than because we forgave it.
    status        text NOT NULL DEFAULT 'due'
                  CHECK (status IN ('due', 'paid', 'waived', 'void')),
    reason        text,

    -- When the seller must have paid by. A deadline rather than a state,
    -- because "overdue" is a fact about the clock and storing it would need a
    -- sweep to keep it true.
    due_at        timestamptz NOT NULL,
    settled_at    timestamptz,

    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT auction_settlements_written_off_has_reason CHECK (
        status NOT IN ('waived', 'void') OR (reason IS NOT NULL AND reason <> '')
    ),
    CONSTRAINT auction_settlements_paid_has_time CHECK (
        status <> 'paid' OR settled_at IS NOT NULL
    )
);

-- The collections queue: what is owed, oldest first.
CREATE INDEX auction_settlements_due
    ON auction_settlements (due_at) WHERE status = 'due';

CREATE INDEX auction_settlements_seller
    ON auction_settlements (seller_id, created_at DESC);

CREATE TRIGGER auction_settlements_updated_at
    BEFORE UPDATE ON auction_settlements
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ── notifications ────────────────────────────────────────────────────────────
--
-- Four templates, and the first is the one that makes an auction an auction.
--
-- Being outbid is only actionable if you are told. A lot that runs for nine
-- days and notifies nobody is a lot with one bid on it — whoever happened to be
-- looking. Push rather than SMS: it is frequent, it is not a security event,
-- and an SMS per outbid on a contested lot would cost more than the commission.
INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('auction.outbid', 'push', 'en', 'transactional', 'You have been outbid',
     'Someone bid {{amount}} on {{plate}}. Bid {{ask}} to lead again — closes {{closes}}.'),
    ('auction.outbid', 'push', 'ar', 'transactional', 'تمت المزايدة عليك',
     'زايد شخص بمبلغ {{amount}} على {{plate}}. زايد بـ {{ask}} لتتصدر مجدداً — ينتهي {{closes}}.'),

    ('auction.won', 'push', 'en', 'transactional', 'You won',
     'You won {{plate}} at {{amount}}. The seller will be in touch to arrange the transfer.'),
    ('auction.won', 'push', 'ar', 'transactional', 'لقد فزت',
     'لقد فزت بـ {{plate}} بمبلغ {{amount}}. سيتواصل معك البائع لترتيب نقل الملكية.'),

    -- Sent to the seller, and it says what they owe as well as what they got.
    -- A seller who learns about the commission on the invoice rather than in
    -- the moment of winning has been ambushed.
    ('auction.sold', 'push', 'en', 'transactional', 'Your lot sold',
     '{{plate}} sold for {{amount}}. Our fee is {{fee}}, due {{due}}.'),
    ('auction.sold', 'push', 'ar', 'transactional', 'تم بيع رقمك',
     'تم بيع {{plate}} بمبلغ {{amount}}. رسومنا {{fee}}، مستحقة {{due}}.'),

    -- Unsold is news too, and it is the one a seller most needs: the lot did
    -- not fail, the reserve was not met, and relisting lower is a decision they
    -- can make today.
    ('auction.unsold', 'push', 'en', 'transactional', 'Your lot did not sell',
     '{{plate}} closed at {{amount}}, below your reserve. You can relist it.'),
    ('auction.unsold', 'push', 'ar', 'transactional', 'لم يُبع رقمك',
     'أُغلق {{plate}} عند {{amount}}، أقل من سعرك الأدنى. يمكنك إعادة إدراجه.')
ON CONFLICT (key, channel, locale, version) DO NOTHING;
