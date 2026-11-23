-- 只读盘点：找出被「买套餐2 却只重置了套餐1 额度」bug 打坏的历史订阅。
-- 判定依据：已完成的订阅订单携带套餐快照（subscription_limits_snapshot = true），
-- 但订单上的 plan_id 与订阅上的 plan_id 不一致，说明履约时只改了租期。
-- 本文件只执行 SELECT，不做任何写操作。

-- 1) 汇总：受影响订阅数与用户数
WITH latest_order AS (
    SELECT DISTINCT ON (po.subscription_id)
           po.subscription_id,
           po.id            AS order_id,
           po.user_id,
           po.plan_id,
           po.subscription_group_ids,
           po.subscription_daily_limit_usd,
           po.subscription_weekly_limit_usd,
           po.subscription_monthly_limit_usd,
           po.updated_at
    FROM payment_orders po
    WHERE po.order_type = 'subscription'
      AND po.status = 'COMPLETED'
      AND po.subscription_limits_snapshot = TRUE
      AND po.subscription_id IS NOT NULL
    ORDER BY po.subscription_id, po.updated_at DESC, po.id DESC
)
SELECT COUNT(DISTINCT lo.subscription_id) AS affected_subscriptions,
       COUNT(DISTINCT lo.user_id)          AS affected_users
FROM latest_order lo
JOIN user_subscriptions us ON us.id = lo.subscription_id
WHERE us.deleted_at IS NULL
  AND lo.plan_id IS NOT NULL
  AND us.plan_id IS DISTINCT FROM lo.plan_id;

-- 2) 明细：逐单对比「已付套餐」与「订阅当前套餐/额度」
WITH latest_order AS (
    SELECT DISTINCT ON (po.subscription_id)
           po.subscription_id,
           po.id            AS order_id,
           po.user_id,
           po.plan_id,
           po.subscription_group_ids,
           po.subscription_daily_limit_usd,
           po.subscription_weekly_limit_usd,
           po.subscription_monthly_limit_usd,
           po.updated_at
    FROM payment_orders po
    WHERE po.order_type = 'subscription'
      AND po.status = 'COMPLETED'
      AND po.subscription_limits_snapshot = TRUE
      AND po.subscription_id IS NOT NULL
    ORDER BY po.subscription_id, po.updated_at DESC, po.id DESC
)
SELECT lo.order_id,
       lo.user_id,
       u.email,
       lo.subscription_id,
       lo.updated_at                        AS order_completed_at,
       lo.plan_id                           AS paid_plan_id,
       us.plan_id                           AS current_plan_id,
       lo.subscription_group_ids            AS paid_group_ids,
       us.group_ids                         AS current_group_ids,
       lo.subscription_daily_limit_usd      AS paid_daily_limit,
       us.daily_limit_usd                   AS current_daily_limit,
       lo.subscription_weekly_limit_usd     AS paid_weekly_limit,
       us.weekly_limit_usd                  AS current_weekly_limit,
       lo.subscription_monthly_limit_usd    AS paid_monthly_limit,
       us.monthly_limit_usd                 AS current_monthly_limit,
       us.expires_at
FROM latest_order lo
JOIN user_subscriptions us ON us.id = lo.subscription_id
LEFT JOIN users u ON u.id = lo.user_id
WHERE us.deleted_at IS NULL
  AND lo.plan_id IS NOT NULL
  AND us.plan_id IS DISTINCT FROM lo.plan_id
ORDER BY lo.updated_at DESC;

-- 3) 同一分组重复订阅检查（换套餐时未合并分组可能留下的并存订阅）
SELECT a.user_id,
       a.id AS subscription_a,
       b.id AS subscription_b,
       a.group_id,
       a.plan_id AS plan_a,
       b.plan_id AS plan_b
FROM user_subscriptions a
JOIN user_subscriptions b
  ON b.user_id = a.user_id
 AND b.id > a.id
 AND b.deleted_at IS NULL
 AND a.deleted_at IS NULL
 AND (b.group_id = a.group_id
      OR b.group_ids @> jsonb_build_array(a.group_id)
      OR a.group_ids @> jsonb_build_array(b.group_id)
      OR EXISTS (
          SELECT 1
          FROM jsonb_array_elements(a.group_ids) AS ag(group_id)
          JOIN jsonb_array_elements(b.group_ids) AS bg(group_id) USING (group_id)
      ))
WHERE a.status = 'active'
  AND b.status = 'active'
  AND a.expires_at > NOW()
  AND b.expires_at > NOW();
