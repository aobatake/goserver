-- +goose Up
CREATE TABLE refresh_tokens (
  token TEXT PRIMARY KEY, 
  created_at timestamp NOT NULL,
  updated_at timestamp NOT NULL,
  user_id UUID NOT NULL,
  expires_at timestamp NOT NULL,
  revoked_at timestamp,
  CONSTRAINT fk_user 
    FOREIGN KEY(user_id)
      REFERENCES users(id)
        ON DELETE CASCADE

);

-- +goose Down
DROP TABLE refresh_tokens;
