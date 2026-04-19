CREATE TABLE users (
    id VARCHAR(64) PRIMARY KEY,
    username VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    email VARCHAR(255) UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE user_stats (
    user_id VARCHAR(64) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    games_played INT DEFAULT 0,
    games_won INT DEFAULT 0,
    total_points DOUBLE PRECISION DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE games (
    id VARCHAR(64) PRIMARY KEY,
    status VARCHAR(32) NOT NULL,
    version BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE hands (
    id VARCHAR(64) PRIMARY KEY,
    game_id VARCHAR(64) NOT NULL REFERENCES games(id),
    hand_no INT NOT NULL,
    dealer_seat INT NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE moves (
    id BIGSERIAL PRIMARY KEY,
    game_id VARCHAR(64) NOT NULL REFERENCES games(id),
    player_id VARCHAR(64) NOT NULL,
    seat_no INT NOT NULL,
    version BIGINT NOT NULL,
    client_version BIGINT NOT NULL,
    move_type VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_moves_game_id ON moves(game_id);
CREATE INDEX idx_moves_player_id ON moves(player_id);
CREATE INDEX idx_games_status ON games(status);
CREATE INDEX idx_hands_game_id_hand_no ON hands(game_id, hand_no);
