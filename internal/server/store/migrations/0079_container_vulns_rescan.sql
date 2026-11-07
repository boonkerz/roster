-- +goose Up
-- „Behoben in“ kam bei Container-Images teils aus einem anderen Release (Debian 14 statt 13);
-- alle Images einmal neu gegen OSV abgleichen.
UPDATE docker_image_scans SET vuln_scanned_at = NULL;

-- +goose Down
SELECT 1;
