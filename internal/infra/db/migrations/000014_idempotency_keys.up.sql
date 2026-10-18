-- 000014 idempotency keys
-- A client that queues an action while it is offline sends the same Idempotency-Key with every try, and a try of an action
-- that already worked gets the first response again instead of doing the work twice. A row is claimed before the work
-- starts, so status_code stays empty while the first try runs and holds the response once it worked.
CREATE TABLE idempotency_keys (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(64) NOT NULL,
    fingerprint BYTEA NOT NULL,
    status_code SMALLINT,
    response BYTEA,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, idempotency_key)
);

CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys(created_at);
