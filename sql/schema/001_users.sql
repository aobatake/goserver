-- +goose Up
CREATE TABLE users (
  id uuid PRIMARY KEY,
  created_at timestamp NOT NULL,
  updated_at timestamp not null,
  email text not null unique
);

-- +goose Down
DROP TABLE users;
