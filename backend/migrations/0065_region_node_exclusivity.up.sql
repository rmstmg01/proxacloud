-- A Proxmox node can belong to exactly one region. Older installations may
-- still contain the original default-region assignment plus a newer explicit
-- assignment. Keep the newest non-default assignment and remove the copies.
WITH ranked_assignments AS (
    SELECT
        rn.region_id,
        rn.proxmox_node_id,
        ROW_NUMBER() OVER (
            PARTITION BY rn.proxmox_node_id
            ORDER BY r.is_default ASC, rn.created_at DESC, rn.region_id ASC
        ) AS assignment_rank
    FROM region_nodes rn
    JOIN regions r ON r.id = rn.region_id
)
DELETE FROM region_nodes rn
USING ranked_assignments ranked
WHERE rn.region_id = ranked.region_id
  AND rn.proxmox_node_id = ranked.proxmox_node_id
  AND ranked.assignment_rank > 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'region_nodes_proxmox_node_id_unique'
          AND conrelid = 'region_nodes'::regclass
    ) THEN
        ALTER TABLE region_nodes
            ADD CONSTRAINT region_nodes_proxmox_node_id_unique UNIQUE (proxmox_node_id);
    END IF;
END
$$;
