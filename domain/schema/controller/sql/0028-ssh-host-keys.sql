-- controller_ssh_host_key stores the SSH host key for the controller jump
-- host. This is an ED25519 private key in PEM format. There is exactly one
-- row in this table; the key is set at bootstrap and read by the sshserver
-- worker.
CREATE TABLE controller_ssh_host_key (
    -- id is a fixed sentinel value (0) so there is always exactly one row.
    id INTEGER NOT NULL PRIMARY KEY CHECK (id = 0),
    private_key TEXT NOT NULL
);
