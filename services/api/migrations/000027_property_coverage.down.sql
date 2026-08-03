-- Drop the property branch, restoring the resolver to vehicles only.
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
