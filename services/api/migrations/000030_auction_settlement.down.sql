DELETE FROM notification_templates WHERE key IN
    ('auction.outbid', 'auction.won', 'auction.sold', 'auction.unsold');
DROP TABLE IF EXISTS auction_settlements;
