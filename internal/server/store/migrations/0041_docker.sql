-- +goose Up
-- Docker-Inventar je Gerät (Container + lokale Images). Wird bei jedem Checkin
-- vollständig ersetzt (Momentaufnahme, keine Historie) – analog listen_ports.
CREATE TABLE docker_containers (
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    container_id TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    image        TEXT NOT NULL DEFAULT '',
    state        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT '',
    ports        TEXT NOT NULL DEFAULT '',
    compose      TEXT NOT NULL DEFAULT '',
    created      TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMP NOT NULL
);
CREATE INDEX idx_docker_containers_device ON docker_containers(device_id);

CREATE TABLE docker_images (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    repository TEXT NOT NULL DEFAULT '',
    tag        TEXT NOT NULL DEFAULT '',
    image_id   TEXT NOT NULL DEFAULT '',
    size       TEXT NOT NULL DEFAULT '',
    created    TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_docker_images_device ON docker_images(device_id);

-- +goose Down
DROP TABLE docker_images;
DROP TABLE docker_containers;
