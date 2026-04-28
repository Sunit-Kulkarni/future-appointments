INSERT INTO trainers (id, name, email, timezone) VALUES
    (1, 'Alice Johnson', 'alice@future.co', 'America/Los_Angeles'),
    (2, 'Bob Smith',     'bob@future.co',   'America/Los_Angeles'),
    (3, 'Carol Davis',   'carol@future.co', 'America/Los_Angeles')
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('trainers', 'id'),
              GREATEST((SELECT MAX(id) FROM trainers), 1));

INSERT INTO users (id, name, email) VALUES
    (1,  'User 1',  'user1@example.com'),
    (2,  'User 2',  'user2@example.com'),
    (3,  'User 3',  'user3@example.com'),
    (4,  'User 4',  'user4@example.com'),
    (5,  'User 5',  'user5@example.com'),
    (6,  'User 6',  'user6@example.com'),
    (7,  'User 7',  'user7@example.com'),
    (8,  'User 8',  'user8@example.com'),
    (9,  'User 9',  'user9@example.com'),
    (10, 'User 10', 'user10@example.com')
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('users', 'id'),
              GREATEST((SELECT MAX(id) FROM users), 1));

INSERT INTO availability (trainer_id, weekday, start_time, end_time)
SELECT t.id, w.weekday, w.start_time, w.end_time
FROM trainers t
CROSS JOIN (VALUES
    (0::SMALLINT, NULL::TIME,        NULL::TIME),
    (1::SMALLINT, '08:00'::TIME,     '17:00'::TIME),
    (2::SMALLINT, '08:00'::TIME,     '17:00'::TIME),
    (3::SMALLINT, '08:00'::TIME,     '17:00'::TIME),
    (4::SMALLINT, '08:00'::TIME,     '17:00'::TIME),
    (5::SMALLINT, '08:00'::TIME,     '17:00'::TIME),
    (6::SMALLINT, NULL::TIME,        NULL::TIME)
) AS w(weekday, start_time, end_time)
WHERE NOT EXISTS (
    SELECT 1 FROM availability a
    WHERE a.trainer_id = t.id AND a.weekday = w.weekday
);
