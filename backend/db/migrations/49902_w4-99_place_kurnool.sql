-- Kurnool joins the curated places (founder, 26 Oct): the first shop and its customers are there,
-- and outside the three seed cities the reverse geocode answered 404 "No place is known near this
-- point yet", so the location bar said "Current location". The all-India import (02 §6) still
-- replaces the curated set later; same id scheme as 10601, so that import can match it.

-- +goose Up
INSERT INTO places (id, kind, name, names, location)
VALUES (md5('todayz:place:city:Kurnool')::uuid, 'city', 'Kurnool',
        '{"en":"Kurnool","hi":"कुरनूल","te":"కర్నూలు"}'::jsonb,
        ST_SetSRID(ST_MakePoint(78.0373, 15.8281), 4326)::geography)
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM places WHERE id = md5('todayz:place:city:Kurnool')::uuid;
