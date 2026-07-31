-- Teach the coverage resolver about property.
--
-- This is the one place adding a subject type is not purely additive Go, and it
-- is worth being precise about why. `resolve_reel_coverage` answers "who is
-- allowed to be shown this reel" — and for a user-authored reel the answer comes
-- from the *subject*, because a person has no service area. The function
-- therefore has to know which table to look in, which is a seam leak by the
-- letter of the rule.
--
-- It is tolerated rather than fixed, for the same reason the subject_type CHECK
-- is: the alternative is a resolver that trusts a caller to hand it a geometry,
-- and the failure mode of getting that wrong is a reel served to a city that
-- cannot buy the thing. A function that can only read tables that exist is a
-- narrower blast radius than a parameter anyone can pass.
--
-- Noting it explicitly so the next vertical's author knows to come here, rather
-- than discovering it as "my reel approves and then never publishes" — which is
-- precisely how it presents: Advance holds at `approved` waiting for coverage
-- that nothing will ever resolve.

CREATE OR REPLACE FUNCTION resolve_reel_coverage(p_reel_id uuid)
RETURNS boolean
LANGUAGE plpgsql
AS $$
DECLARE
    v_author_type  text;
    v_author_id    uuid;
    v_subject_type text;
    v_subject_id   uuid;
    v_coverage     geography;
    v_kind         text;
    v_origin       geography;
BEGIN
    SELECT author_type, author_id, subject_type, subject_id
      INTO v_author_type, v_author_id, v_subject_type, v_subject_id
      FROM reels WHERE id = p_reel_id;

    IF v_author_type IS NULL THEN
        RETURN false;
    END IF;

    IF v_author_type = 'vendor_branch' THEN
        SELECT b.service_area, b.point
          INTO v_coverage, v_origin
          FROM vendor_branches b
         WHERE b.id = v_author_id
           AND b.deleted_at IS NULL
           AND b.is_active;
        v_kind := 'service_area';

    ELSIF v_author_type = 'user' THEN
        -- A user-authored reel earns an audience from its subject, not from its
        -- author. A person has no service area, and a reel about nothing has no
        -- one to show it to — returning false here is what keeps general UGC out
        -- of the feed until a phase deliberately admits it.
        IF v_subject_type = 'vehicle_listing' AND v_subject_id IS NOT NULL THEN
            SELECT ST_Buffer(l.point, l.radius_m), l.point
              INTO v_coverage, v_origin
              FROM vehicle_listings l
             WHERE l.id = v_subject_id
               AND l.seller_id = v_author_id
               AND l.status IN ('draft', 'pending_review', 'active');
            v_kind := 'radius';

        ELSIF v_subject_type = 'property_listing' AND v_subject_id IS NOT NULL THEN
            -- A fixed radius rather than a seller-chosen one, which is the
            -- difference from a car.
            --
            -- A car's radius is "how far will a buyer travel to see it", and the
            -- seller is the one who knows. A home does not travel at all: the
            -- relevant audience is people who want to live in *that area*, and
            -- letting an advertiser set it to the whole country is how a
            -- Fujairah studio ends up in a Dubai Marina feed. Twenty-five
            -- kilometres is roughly a commute, which is the real question a
            -- renter is asking.
            SELECT ST_Buffer(p.point, 25000), p.point
              INTO v_coverage, v_origin
              FROM property_listings p
             WHERE p.id = v_subject_id
               AND p.advertiser_id = v_author_id
               AND p.status IN ('draft', 'pending_review', 'active');
            v_kind := 'radius';
        END IF;
    END IF;

    IF v_coverage IS NULL THEN
        RETURN false;
    END IF;

    UPDATE reels
       SET coverage             = v_coverage,
           coverage_kind        = v_kind,
           origin_point         = v_origin,
           coverage_resolved_at = now()
     WHERE id = p_reel_id;

    RETURN true;
END;
$$;
