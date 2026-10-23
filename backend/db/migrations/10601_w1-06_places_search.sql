-- W1-06 places, discovery and search (03 §Places, §Search; 01 §11; 02 §7;
-- D-42, D-59, D-63, D-99, D-101).
--
-- places: PIN codes, localities and cities for the location picker and the
-- reverse geocode. The weekend slice loads a small hand-curated set for the 3
-- seed cities below (Bengaluru, Hyderabad, Chennai): the repo has no India
-- Post or OSM extract and the build has no network. The coordinates are
-- approximate locality centroids, good enough for "nearest locality", not a
-- survey. source = 'curated' marks them; the all-India import (India Post PIN
-- directory + OSM place names, 02 §6) replaces them later (W1-06 report).
--
-- product_synonyms: query-time expansion for common Indian names ("dahi" ↔
-- "curd", "atta" ↔ "wheat flour", transliterations). Rows are normalised
-- (lower case, single spaces), so the service compares them directly. A row
-- with product_id names that master product's own synonym (D-99: "the
-- normalised query equals one of its synonyms"). The hi, kn, te and ta rows
-- are drafts waiting for a native speaker's review, like W1-05's category
-- names.
--
-- search_log: one row per search the API answered (zero results too), daily
-- partitions on created_at. Never the coordinates: geo_cell is the H3 res 8
-- cell (D-59), left NULL until an H3 library is in go.mod (W1-06 report).
-- No foreign keys: a log row must never fail because of another table.

-- +goose Up

------------------------------------------------------------------------------
-- places
------------------------------------------------------------------------------
CREATE TABLE places (
  id         uuid PRIMARY KEY,
  kind       text NOT NULL,
  name       text NOT NULL,
  names      jsonb NOT NULL DEFAULT '{}'::jsonb,
  pincode    text,
  city_id    uuid,
  location   geography(Point, 4326) NOT NULL,
  bbox       geography(Polygon, 4326),
  source     text NOT NULL DEFAULT 'curated',
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_places_city_id FOREIGN KEY (city_id) REFERENCES places (id),
  CONSTRAINT ck_places_kind CHECK (kind IN ('pincode', 'locality', 'city')),
  CONSTRAINT ck_places_name CHECK (char_length(name) BETWEEN 1 AND 80),
  CONSTRAINT ck_places_names CHECK (jsonb_typeof(names) = 'object'),
  CONSTRAINT ck_places_pincode CHECK (pincode IS NULL OR pincode ~ '^[1-9][0-9]{5}$'),
  CONSTRAINT ck_places_city CHECK ((kind = 'city') = (city_id IS NULL)),
  CONSTRAINT ck_places_source CHECK (source IN ('curated', 'india_post', 'osm'))
);
CREATE UNIQUE INDEX ux_places_natural ON places (kind, coalesce(city_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name), coalesce(pincode, ''));
CREATE INDEX ix_places_location ON places USING gist (location);
CREATE INDEX ix_places_name_trgm ON places USING gin (name gin_trgm_ops);
CREATE INDEX ix_places_pincode ON places (pincode text_pattern_ops) WHERE pincode IS NOT NULL;
CREATE INDEX ix_places_city_id ON places (city_id) WHERE city_id IS NOT NULL;
REVOKE TRUNCATE ON places FROM todayz_app;

INSERT INTO places (id, kind, name, names, location)
SELECT md5('todayz:place:city:' || v.name)::uuid, 'city', v.name, v.names::jsonb,
       ST_SetSRID(ST_MakePoint(v.lng, v.lat), 4326)::geography
FROM (VALUES
  ('Bengaluru', '{"en":"Bengaluru","hi":"बेंगलुरु","kn":"ಬೆಂಗಳೂರು"}', 12.9716, 77.5946),
  ('Hyderabad', '{"en":"Hyderabad","hi":"हैदराबाद","te":"హైదరాబాద్"}', 17.3850, 78.4867),
  ('Chennai',   '{"en":"Chennai","hi":"चेन्नई","ta":"சென்னை"}',          13.0827, 80.2707)
) AS v(name, names, lat, lng);

INSERT INTO places (id, kind, name, pincode, city_id, location)
SELECT md5('todayz:place:locality:' || v.city || ':' || v.name)::uuid, 'locality', v.name, v.pincode, c.id,
       ST_SetSRID(ST_MakePoint(v.lng, v.lat), 4326)::geography
FROM (VALUES
  -- Bengaluru
  ('Bengaluru', 'Indiranagar',         '560038', 12.9719, 77.6412),
  ('Bengaluru', 'HAL 2nd Stage',       '560008', 12.9610, 77.6440),
  ('Bengaluru', 'Halasuru',            '560008', 12.9817, 77.6286),
  ('Bengaluru', 'Domlur',              '560071', 12.9610, 77.6387),
  ('Bengaluru', 'Jeevan Bhima Nagar',  '560075', 12.9640, 77.6590),
  ('Bengaluru', 'CV Raman Nagar',      '560093', 12.9855, 77.6630),
  ('Bengaluru', 'Murugeshpalya',       '560017', 12.9585, 77.6533),
  ('Bengaluru', 'Koramangala',         '560034', 12.9352, 77.6245),
  ('Bengaluru', 'Ejipura',             '560047', 12.9435, 77.6280),
  ('Bengaluru', 'HSR Layout',          '560102', 12.9116, 77.6389),
  ('Bengaluru', 'BTM Layout',          '560076', 12.9166, 77.6101),
  ('Bengaluru', 'Bommanahalli',        '560068', 12.9018, 77.6246),
  ('Bengaluru', 'Bellandur',           '560103', 12.9304, 77.6784),
  ('Bengaluru', 'Sarjapur Road',       '560035', 12.9010, 77.6870),
  ('Bengaluru', 'Marathahalli',        '560037', 12.9569, 77.7011),
  ('Bengaluru', 'Whitefield',          '560066', 12.9698, 77.7500),
  ('Bengaluru', 'Mahadevapura',        '560048', 12.9915, 77.7060),
  ('Bengaluru', 'KR Puram',            '560036', 13.0076, 77.6964),
  ('Bengaluru', 'Electronic City',     '560100', 12.8452, 77.6602),
  ('Bengaluru', 'Jayanagar',           '560011', 12.9250, 77.5938),
  ('Bengaluru', 'JP Nagar',            '560078', 12.9063, 77.5857),
  ('Bengaluru', 'Basavanagudi',        '560004', 12.9417, 77.5755),
  ('Bengaluru', 'Banashankari',        '560050', 12.9255, 77.5468),
  ('Bengaluru', 'Wilson Garden',       '560027', 12.9490, 77.5968),
  ('Bengaluru', 'Richmond Town',       '560025', 12.9634, 77.6010),
  ('Bengaluru', 'MG Road',             '560001', 12.9755, 77.6070),
  ('Bengaluru', 'Shivajinagar',        '560051', 12.9857, 77.6057),
  ('Bengaluru', 'Frazer Town',         '560005', 12.9980, 77.6150),
  ('Bengaluru', 'Gandhinagar',         '560009', 12.9767, 77.5713),
  ('Bengaluru', 'Chickpet',            '560053', 12.9698, 77.5779),
  ('Bengaluru', 'Malleshwaram',        '560003', 13.0031, 77.5643),
  ('Bengaluru', 'Sadashivanagar',      '560080', 13.0068, 77.5813),
  ('Bengaluru', 'Rajajinagar',         '560010', 12.9982, 77.5530),
  ('Bengaluru', 'Vijayanagar',         '560040', 12.9719, 77.5355),
  ('Bengaluru', 'Yeshwanthpur',        '560022', 13.0280, 77.5409),
  ('Bengaluru', 'Peenya',              '560058', 13.0329, 77.5273),
  ('Bengaluru', 'Hebbal',              '560024', 13.0358, 77.5970),
  ('Bengaluru', 'RT Nagar',            '560032', 13.0213, 77.5951),
  ('Bengaluru', 'Kalyan Nagar',        '560043', 13.0221, 77.6403),
  ('Bengaluru', 'Banaswadi',           '560043', 13.0141, 77.6518),
  ('Bengaluru', 'Kammanahalli',        '560084', 13.0159, 77.6379),
  ('Bengaluru', 'Yelahanka',           '560064', 13.1007, 77.5963),
  ('Bengaluru', 'Kengeri',             '560060', 12.9177, 77.4838),
  -- Hyderabad
  ('Hyderabad', 'Banjara Hills',       '500034', 17.4156, 78.4347),
  ('Hyderabad', 'Jubilee Hills',       '500033', 17.4326, 78.4071),
  ('Hyderabad', 'Madhapur',            '500081', 17.4483, 78.3915),
  ('Hyderabad', 'Hitech City',         '500081', 17.4474, 78.3762),
  ('Hyderabad', 'Gachibowli',          '500032', 17.4401, 78.3489),
  ('Hyderabad', 'Kondapur',            '500084', 17.4699, 78.3578),
  ('Hyderabad', 'Miyapur',             '500049', 17.4960, 78.3570),
  ('Hyderabad', 'Kukatpally',          '500072', 17.4849, 78.4138),
  ('Hyderabad', 'Manikonda',           '500089', 17.4045, 78.3865),
  ('Hyderabad', 'Tolichowki',          '500008', 17.3986, 78.4170),
  ('Hyderabad', 'Mehdipatnam',         '500028', 17.3916, 78.4383),
  ('Hyderabad', 'Masab Tank',          '500028', 17.4004, 78.4530),
  ('Hyderabad', 'Ameerpet',            '500016', 17.4375, 78.4482),
  ('Hyderabad', 'Begumpet',            '500016', 17.4447, 78.4664),
  ('Hyderabad', 'Somajiguda',          '500082', 17.4260, 78.4580),
  ('Hyderabad', 'Punjagutta',          '500082', 17.4254, 78.4510),
  ('Hyderabad', 'Khairatabad',         '500004', 17.4123, 78.4627),
  ('Hyderabad', 'Himayatnagar',        '500029', 17.4029, 78.4867),
  ('Hyderabad', 'Abids',               '500001', 17.3926, 78.4770),
  ('Hyderabad', 'Nampally',            '500001', 17.3890, 78.4680),
  ('Hyderabad', 'Koti',                '500095', 17.3858, 78.4867),
  ('Hyderabad', 'Charminar',           '500002', 17.3616, 78.4747),
  ('Hyderabad', 'Secunderabad',        '500003', 17.4399, 78.4983),
  ('Hyderabad', 'Tarnaka',             '500017', 17.4282, 78.5387),
  ('Hyderabad', 'Malkajgiri',          '500047', 17.4500, 78.5300),
  ('Hyderabad', 'Uppal',               '500039', 17.4058, 78.5591),
  ('Hyderabad', 'Dilsukhnagar',        '500060', 17.3688, 78.5247),
  ('Hyderabad', 'LB Nagar',            '500074', 17.3457, 78.5522),
  ('Hyderabad', 'Kompally',            '500014', 17.5360, 78.4860),
  -- Chennai
  ('Chennai',   'T. Nagar',            '600017', 13.0418, 80.2341),
  ('Chennai',   'West Mambalam',       '600033', 13.0380, 80.2210),
  ('Chennai',   'Kodambakkam',         '600024', 13.0521, 80.2255),
  ('Chennai',   'Vadapalani',          '600026', 13.0500, 80.2121),
  ('Chennai',   'Ashok Nagar',         '600083', 13.0370, 80.2120),
  ('Chennai',   'KK Nagar',            '600078', 13.0411, 80.1993),
  ('Chennai',   'Saidapet',            '600015', 13.0213, 80.2231),
  ('Chennai',   'Guindy',              '600032', 13.0067, 80.2206),
  ('Chennai',   'Nungambakkam',        '600034', 13.0569, 80.2425),
  ('Chennai',   'Alwarpet',            '600018', 13.0339, 80.2525),
  ('Chennai',   'Mylapore',            '600004', 13.0339, 80.2619),
  ('Chennai',   'Royapettah',          '600014', 13.0545, 80.2632),
  ('Chennai',   'Triplicane',          '600005', 13.0587, 80.2756),
  ('Chennai',   'Egmore',              '600008', 13.0732, 80.2609),
  ('Chennai',   'Kilpauk',             '600010', 13.0825, 80.2420),
  ('Chennai',   'Purasawalkam',        '600007', 13.0870, 80.2540),
  ('Chennai',   'Anna Nagar',          '600040', 13.0850, 80.2101),
  ('Chennai',   'Mogappair',           '600037', 13.0837, 80.1767),
  ('Chennai',   'Perambur',            '600011', 13.1210, 80.2330),
  ('Chennai',   'George Town',         '600001', 13.0910, 80.2870),
  ('Chennai',   'Royapuram',           '600013', 13.1137, 80.2954),
  ('Chennai',   'Adyar',               '600020', 13.0012, 80.2565),
  ('Chennai',   'Besant Nagar',        '600090', 13.0002, 80.2668),
  ('Chennai',   'Thiruvanmiyur',       '600041', 12.9830, 80.2594),
  ('Chennai',   'Velachery',           '600042', 12.9815, 80.2180),
  ('Chennai',   'Perungudi',           '600096', 12.9654, 80.2461),
  ('Chennai',   'Sholinganallur',      '600119', 12.9010, 80.2279),
  ('Chennai',   'Porur',               '600116', 13.0382, 80.1565),
  ('Chennai',   'Valasaravakkam',      '600087', 13.0403, 80.1723),
  ('Chennai',   'Pallavaram',          '600043', 12.9675, 80.1491),
  ('Chennai',   'Chromepet',           '600044', 12.9516, 80.1462),
  ('Chennai',   'Tambaram',            '600045', 12.9249, 80.1200)
) AS v(city, name, pincode, lat, lng)
JOIN places c ON c.kind = 'city' AND c.name = v.city;

------------------------------------------------------------------------------
-- product_synonyms
------------------------------------------------------------------------------
CREATE TABLE product_synonyms (
  id         uuid PRIMARY KEY,
  term       text NOT NULL,
  synonym    text NOT NULL,
  lang       text NOT NULL,
  product_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT fk_product_synonyms_product_id FOREIGN KEY (product_id) REFERENCES products (id),
  CONSTRAINT ck_product_synonyms_term CHECK (char_length(term) BETWEEN 1 AND 80 AND term = lower(term) AND term = btrim(term)),
  CONSTRAINT ck_product_synonyms_synonym CHECK (char_length(synonym) BETWEEN 1 AND 80 AND synonym = lower(synonym) AND synonym = btrim(synonym)),
  CONSTRAINT ck_product_synonyms_distinct CHECK (term <> synonym),
  CONSTRAINT ck_product_synonyms_lang CHECK (lang IN ('en', 'hi', 'kn', 'te', 'ta'))
);
CREATE UNIQUE INDEX ux_product_synonyms_pair ON product_synonyms (term, synonym, coalesce(product_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX ix_product_synonyms_synonym ON product_synonyms (synonym);
CREATE INDEX ix_product_synonyms_product_id ON product_synonyms (product_id) WHERE product_id IS NOT NULL;
REVOKE TRUNCATE ON product_synonyms FROM todayz_app;

INSERT INTO product_synonyms (id, term, synonym, lang)
SELECT md5('todayz:synonym:' || v.term || ':' || v.synonym)::uuid, v.term, v.synonym, v.lang
FROM (VALUES
  ('curd', 'dahi', 'hi'), ('curd', 'दही', 'hi'), ('curd', 'mosaru', 'kn'), ('curd', 'ಮೊಸರು', 'kn'),
  ('curd', 'perugu', 'te'), ('curd', 'పెరుగు', 'te'), ('curd', 'thayir', 'ta'), ('curd', 'தயிர்', 'ta'),
  ('curd', 'yogurt', 'en'),
  ('butter', 'makhan', 'hi'), ('butter', 'makkhan', 'hi'), ('butter', 'मक्खन', 'hi'), ('butter', 'benne', 'kn'),
  ('butter', 'ಬೆಣ್ಣೆ', 'kn'), ('butter', 'venna', 'te'), ('butter', 'వెన్న', 'te'), ('butter', 'vennai', 'ta'),
  ('butter', 'வெண்ணெய்', 'ta'),
  ('milk', 'doodh', 'hi'), ('milk', 'दूध', 'hi'), ('milk', 'haalu', 'kn'), ('milk', 'ಹಾಲು', 'kn'),
  ('milk', 'paalu', 'te'), ('milk', 'పాలు', 'te'), ('milk', 'paal', 'ta'), ('milk', 'பால்', 'ta'),
  ('ghee', 'tuppa', 'kn'), ('ghee', 'neyyi', 'te'), ('ghee', 'nei', 'ta'), ('ghee', 'घी', 'hi'),
  ('atta', 'wheat flour', 'en'), ('atta', 'आटा', 'hi'), ('atta', 'godhi hittu', 'kn'), ('atta', 'chakki atta', 'hi'),
  ('rice', 'chawal', 'hi'), ('rice', 'चावल', 'hi'), ('rice', 'akki', 'kn'), ('rice', 'ಅಕ್ಕಿ', 'kn'),
  ('rice', 'biyyam', 'te'), ('rice', 'arisi', 'ta'), ('rice', 'அரிசி', 'ta'),
  ('sugar', 'cheeni', 'hi'), ('sugar', 'chini', 'hi'), ('sugar', 'sakkare', 'kn'), ('sugar', 'ಸಕ್ಕರೆ', 'kn'),
  ('salt', 'namak', 'hi'), ('salt', 'uppu', 'kn'),
  ('oil', 'tel', 'hi'), ('oil', 'enne', 'kn'), ('oil', 'nune', 'te'), ('oil', 'ennai', 'ta'),
  ('eggs', 'anda', 'hi'), ('eggs', 'motte', 'kn'), ('eggs', 'guddu', 'te'), ('eggs', 'muttai', 'ta'),
  ('onion', 'pyaz', 'hi'), ('onion', 'pyaaz', 'hi'), ('onion', 'eerulli', 'kn'), ('onion', 'ullipaya', 'te'),
  ('onion', 'vengayam', 'ta'),
  ('tomato', 'tamatar', 'hi'), ('potato', 'aloo', 'hi'), ('potato', 'aalu', 'hi'),
  ('paneer', 'cottage cheese', 'en'),
  ('besan', 'gram flour', 'en'),
  ('rava', 'sooji', 'hi'), ('rava', 'suji', 'hi'), ('rava', 'semolina', 'en'),
  ('poha', 'flattened rice', 'en'), ('poha', 'avalakki', 'kn'), ('poha', 'atukulu', 'te'), ('poha', 'aval', 'ta'),
  ('jaggery', 'gud', 'hi'), ('jaggery', 'bella', 'kn'), ('jaggery', 'bellam', 'te'), ('jaggery', 'vellam', 'ta'),
  ('turmeric', 'haldi', 'hi'), ('turmeric', 'arishina', 'kn'), ('turmeric', 'pasupu', 'te'), ('turmeric', 'manjal', 'ta'),
  ('chilli', 'mirchi', 'hi'), ('chilli', 'chili', 'en'),
  ('coriander', 'dhaniya', 'hi'), ('coriander', 'kottambari', 'kn'), ('coriander', 'kothamalli', 'ta'),
  ('toor dal', 'arhar dal', 'hi'), ('toor dal', 'tur dal', 'en'), ('dal', 'dhal', 'en'), ('dal', 'daal', 'en'),
  ('tea', 'chai', 'hi'), ('coffee', 'kaapi', 'kn'),
  ('detergent', 'washing powder', 'en'), ('soap', 'sabun', 'hi')
) AS v(term, synonym, lang);

------------------------------------------------------------------------------
-- search_log (partitioned daily)
------------------------------------------------------------------------------
CREATE TABLE search_log (
  id            uuid NOT NULL,
  created_at    timestamptz NOT NULL,
  query_norm    text NOT NULL,
  results_count int NOT NULL,
  store_count   int NOT NULL,
  radius_m      int NOT NULL,
  geo_cell      bigint,  -- H3 resolution 8 (D-59); never coordinates
  category      text,    -- the category filter's slug
  product_id    uuid,    -- D-99: exact barcode, name or synonym, else NULL
  user_id       uuid,    -- the signed-in customer, NULL when anonymous
  source        text NOT NULL DEFAULT 'typed',
  CONSTRAINT pk_search_log PRIMARY KEY (id, created_at),
  CONSTRAINT ck_search_log_query_norm CHECK (char_length(query_norm) BETWEEN 1 AND 200),
  CONSTRAINT ck_search_log_counts CHECK (results_count >= 0 AND store_count >= 0 AND store_count <= results_count),
  CONSTRAINT ck_search_log_radius CHECK (radius_m BETWEEN 1 AND 50000),
  CONSTRAINT ck_search_log_category CHECK (category IS NULL OR (category ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(category) <= 64)),
  CONSTRAINT ck_search_log_source CHECK (source IN ('typed', 'voice'))
) PARTITION BY RANGE (created_at);

CREATE TABLE search_log_default PARTITION OF search_log DEFAULT;
CREATE INDEX brin_search_log_created_at ON search_log USING brin (created_at);
CREATE INDEX ix_search_log_product_id ON search_log (product_id, created_at) WHERE product_id IS NOT NULL;

-- The API inserts; account deletion (W1-01) may clear user_id, so UPDATE
-- stays. Nobody truncates. ensure_partitions copies these privileges.
REVOKE TRUNCATE ON search_log, search_log_default FROM todayz_app;

INSERT INTO partition_policies (parent, granularity, ahead) VALUES ('search_log', 'day', 7);
SELECT ensure_partitions('search_log', now());

-- +goose Down
DELETE FROM partition_policies WHERE parent = 'search_log';
DROP TABLE IF EXISTS search_log;
DROP TABLE IF EXISTS product_synonyms;
DROP TABLE IF EXISTS places;
