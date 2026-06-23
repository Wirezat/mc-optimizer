-- Persist the fractional exact machine count per group so that upgrade edits can recompute
-- the count from the true required rate (lossless) instead of from the rounded count.
-- Stored as a rational (num/den), mirroring solver.MachineGroupDraft.ExactCount.
ALTER TABLE machine_groups
    ADD COLUMN IF NOT EXISTS exact_count_num BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS exact_count_den BIGINT NOT NULL DEFAULT 1;
