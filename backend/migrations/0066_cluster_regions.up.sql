ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS region_id BIGINT REFERENCES regions(id) ON DELETE SET NULL;

ALTER TABLE database_clusters
    ADD COLUMN IF NOT EXISTS region_id BIGINT REFERENCES regions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_clusters_region_id ON clusters(region_id);
CREATE INDEX IF NOT EXISTS idx_database_clusters_region_id ON database_clusters(region_id);

-- Backfill only when every recorded node for an existing resource belongs to
-- the same region. Cross-region legacy resources remain unassigned.
UPDATE clusters c
SET region_id = owned.region_id
FROM (
    SELECT cn.cluster_id, MIN(rn.region_id) AS region_id
    FROM cluster_nodes cn
    JOIN proxmox_nodes pn ON pn.node_name = cn.proxmox_node_name
    JOIN region_nodes rn ON rn.proxmox_node_id = pn.id
    GROUP BY cn.cluster_id
    HAVING COUNT(DISTINCT rn.region_id) = 1
) owned
WHERE c.id = owned.cluster_id AND c.region_id IS NULL;

UPDATE database_clusters d
SET region_id = owned.region_id
FROM (
    SELECT dn.database_cluster_id, MIN(rn.region_id) AS region_id
    FROM database_cluster_nodes dn
    JOIN proxmox_nodes pn ON pn.node_name = dn.proxmox_node_name
    JOIN region_nodes rn ON rn.proxmox_node_id = pn.id
    GROUP BY dn.database_cluster_id
    HAVING COUNT(DISTINCT rn.region_id) = 1
) owned
WHERE d.id = owned.database_cluster_id AND d.region_id IS NULL;
