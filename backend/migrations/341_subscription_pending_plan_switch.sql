-- 341: 订阅换套餐的「下个周期生效」载体（issue #6）。
--
-- 口径：降级时当前周期保留旧额度，新套餐从下个周期起生效。
-- 没有这四个列就没地方存「已付钱、但按口径要等窗口滚动才生效」的额度：
-- 订阅行的 daily/weekly/monthly_limit_usd 只能表达一套当前生效的额度。
--
-- 只加可空列，不 backfill、不写任何历史行：
-- 应用回滚可以保留列和待生效数据。确需删列时，在旧代码运行后手动使用
-- backend/scripts/rollback-341-subscription-pending-plan-switch.sql。
ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS pending_plan_id          BIGINT          DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS pending_daily_limit_usd  DECIMAL(20, 8)  DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS pending_weekly_limit_usd DECIMAL(20, 8)  DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS pending_monthly_limit_usd DECIMAL(20, 8) DEFAULT NULL;

COMMENT ON COLUMN user_subscriptions.pending_plan_id IS '已切换套餐 ID，尚有额度等待各自窗口滚动';
COMMENT ON COLUMN user_subscriptions.pending_daily_limit_usd IS '待生效的日额度，下一次日窗口滚动时落地';
COMMENT ON COLUMN user_subscriptions.pending_weekly_limit_usd IS '待生效的周额度，下一次周窗口滚动时落地';
COMMENT ON COLUMN user_subscriptions.pending_monthly_limit_usd IS '待生效的月额度，下一次月窗口滚动时落地';
