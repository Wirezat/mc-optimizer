ALTER TABLE machine_groups
    DROP COLUMN IF EXISTS exact_count_num,
    DROP COLUMN IF EXISTS exact_count_den;
