DROP TABLE IF EXISTS cluster_node_tunnel_attachments;
ALTER TABLE cluster_nodes DROP COLUMN IF EXISTS fqdn;
ALTER TABLE cluster_nodes DROP COLUMN IF EXISTS hostname;
