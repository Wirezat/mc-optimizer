-- Persist the original SolveRequest with each production line so it can be re-solved later
-- (e.g. "more output": re-run the solver at a higher target rate, reusing the same recipe
-- choices). Stop points etc. are implied by the stored request, so the request is sufficient.
ALTER TABLE production_lines
    ADD COLUMN IF NOT EXISTS solve_request JSONB;
