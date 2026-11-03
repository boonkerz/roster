-- +goose Up
-- Who-Data (wer hat die Datei geändert: Benutzer, Prozess) und drei Richtlinienschalter:
-- fim_whodata (auditd / SACL+4663), state_monitor (Systemzustand), rootcheck (Rootkit/Malware).
ALTER TABLE fim_events ADD COLUMN who_user TEXT NOT NULL DEFAULT '';
ALTER TABLE fim_events ADD COLUMN who_process TEXT NOT NULL DEFAULT '';
ALTER TABLE policy_security ADD COLUMN fim_whodata BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE policy_security ADD COLUMN state_monitor BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE policy_security ADD COLUMN rootcheck BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE policy_security DROP COLUMN rootcheck;
ALTER TABLE policy_security DROP COLUMN state_monitor;
ALTER TABLE policy_security DROP COLUMN fim_whodata;
ALTER TABLE fim_events DROP COLUMN who_process;
ALTER TABLE fim_events DROP COLUMN who_user;
