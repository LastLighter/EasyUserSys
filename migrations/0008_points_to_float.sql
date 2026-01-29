-- 将积分相关字段从 INT 改为 DOUBLE PRECISION 以支持按量计费（浮点数积分）

-- plans 表: grant_points
ALTER TABLE plans ALTER COLUMN grant_points TYPE DOUBLE PRECISION;

-- balance_buckets 表: total_points, remaining_points
ALTER TABLE balance_buckets ALTER COLUMN total_points TYPE DOUBLE PRECISION;
ALTER TABLE balance_buckets ALTER COLUMN remaining_points TYPE DOUBLE PRECISION;

-- billing_ledger 表: delta_points
ALTER TABLE billing_ledger ALTER COLUMN delta_points TYPE DOUBLE PRECISION;

-- usage_records 表: cost_points
ALTER TABLE usage_records ALTER COLUMN cost_points TYPE DOUBLE PRECISION;

-- orders 表: points
ALTER TABLE orders ALTER COLUMN points TYPE DOUBLE PRECISION;
