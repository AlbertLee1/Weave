DROP TRIGGER IF EXISTS system_graphs_write_history_trg ON system_graphs;
DROP FUNCTION IF EXISTS system_graphs_write_history();
DROP INDEX IF EXISTS system_graph_versions_graph_idx;
DROP TABLE IF EXISTS system_graph_versions;
DROP INDEX IF EXISTS system_graphs_ontology_idx;
DROP TABLE IF EXISTS system_graphs;
