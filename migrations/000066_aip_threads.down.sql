-- US-279 down: drop messages first to satisfy the FK, then threads.

DROP TABLE IF EXISTS aip_messages;
DROP TABLE IF EXISTS aip_threads;
