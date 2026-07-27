-- +goose Up
-- Zusatz-Eigenschaften für Custom-Fields (v.a. Typ list):
--   managed         = agent-verwaltet -> im Editor schreibgeschützt
--   link            = Listen-Einträge als klickbare Links rendern
--   selection_field = Name des Begleit-Listenfelds; gesetzt => Quell-Liste ist
--                     „auswählbar" (Checkboxen), Auswahl landet in diesem Feld
ALTER TABLE custom_fields ADD COLUMN managed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE custom_fields ADD COLUMN link BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE custom_fields ADD COLUMN selection_field TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE custom_fields DROP COLUMN selection_field;
ALTER TABLE custom_fields DROP COLUMN link;
ALTER TABLE custom_fields DROP COLUMN managed;
