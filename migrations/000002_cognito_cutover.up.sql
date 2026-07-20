-- Pre-launch clean cutover (spec Section 2): existing rows are demo users
-- with no Cognito identity. user_stats cascades; moves.player_id has no FK.
DELETE FROM users;

ALTER TABLE users ADD COLUMN cognito_sub VARCHAR(64) UNIQUE NOT NULL;
ALTER TABLE users DROP COLUMN password_hash;
