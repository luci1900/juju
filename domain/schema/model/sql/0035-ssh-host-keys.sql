-- machine_virtual_ssh_host_key stores the virtual SSH host key for a
-- machine routing target. There is at most one row per machine. The key
-- is an ED25519 private key in PEM format and is used by the sshsession
-- worker to present a stable host identity to clients connecting via the
-- SSH jump server.
CREATE TABLE machine_virtual_ssh_host_key (
    machine_uuid TEXT NOT NULL PRIMARY KEY,
    private_key  TEXT NOT NULL,
    CONSTRAINT fk_machine_virtual_ssh_host_key_machine
    FOREIGN KEY (machine_uuid)
    REFERENCES machine (uuid)
);

-- unit_virtual_ssh_host_key stores the virtual SSH host key for a unit
-- routing target. There is at most one row per unit. Kept separate from
-- machine_virtual_ssh_host_key to allow strict FK constraints to the
-- respective entity tables and to align with K8s semantics where units
-- are not backed by IAAS machines.
CREATE TABLE unit_virtual_ssh_host_key (
    unit_uuid   TEXT NOT NULL PRIMARY KEY,
    private_key TEXT NOT NULL,
    CONSTRAINT fk_unit_virtual_ssh_host_key_unit
    FOREIGN KEY (unit_uuid)
    REFERENCES unit (uuid)
);
