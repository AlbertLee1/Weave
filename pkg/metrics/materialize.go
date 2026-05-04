package metrics

import "time"

// MaterializeFileWritten records a single parquet file successfully written
// by the materializer (US-409). lag is the wall-clock delta between the
// originating EditBatch.Timestamp and the moment the file landed on disk;
// sizeBytes is the file's size in bytes after the atomic rename.
//
// All three labels are partitioned by ontology + object type so dashboards
// can isolate a misbehaving ObjectType without aggregate masking. Negative
// lag values (clock skew, pinned timestamps in tests that pre-date now) are
// clamped to zero before observation so the histogram stays non-negative.
func MaterializeFileWritten(ontology, objectType string, lag time.Duration, sizeBytes int64) {
	parquetFilesTotal.WithLabelValues(ontology, objectType).Inc()
	if sizeBytes > 0 {
		parquetSizeBytes.WithLabelValues(ontology, objectType).Observe(float64(sizeBytes))
	}
	seconds := lag.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	materializeLagSeconds.WithLabelValues(ontology, objectType).Observe(seconds)
}
