-- US-443 GDPR data export + deletion proof hash.
--
-- Adds a deterministic proof-of-erasure digest to gdpr_erasure_jobs so
-- auditors can verify a deletion claim without re-walking every step
-- result. The hash commits to the canonical (userId, status, ordered
-- step outcomes, errorMessage, requestedBy) tuple computed by
-- pkg/gdpr.ComputeProofHash. Empty string until the job reaches a
-- terminal state.

ALTER TABLE gdpr_erasure_jobs
    ADD COLUMN IF NOT EXISTS proof_hash TEXT NOT NULL DEFAULT '';
