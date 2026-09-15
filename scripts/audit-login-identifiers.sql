-- Read-only: includes case/space duplicates and username-to-email collisions.
SELECT identifier, array_agg(DISTINCT id ORDER BY id) AS user_ids
FROM (
    SELECT id, lower(btrim(username)) AS identifier FROM users
    UNION
    SELECT id, lower(btrim(email)) FROM users WHERE btrim(email) <> ''
) identities
GROUP BY identifier
HAVING count(DISTINCT id) > 1
ORDER BY identifier;
