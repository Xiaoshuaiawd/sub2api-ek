# 智商分流规则管理 API

管理员可以使用现有管理员 API Key，或具有 `intelligence_rules.read` / `intelligence_rules.write` 权限的受限管理员密钥，读取和单项修改问题库。密钥通过 `x-api-key` 请求头传递，不放入 URL；远程调用应使用 HTTPS 或可信的 SSH 隧道。

所有接口路径相对于服务地址。请求和响应使用 JSON，响应采用项目现有的 `{code,message,data}` 格式。每次修改前读取当前版本；任何写入都会改变版本，旧版本提交会返回 HTTP 409，应重新读取后重试。PATCH 请求体最大 4 MiB。当前网页保存也会校验版本；为兼容旧客户端，不带版本的旧式整库 PUT 仍可能覆盖较新的配置。

## 读取规则

`GET /api/v1/admin/settings/intelligence-rules`

返回 `data.revision`、自定义 `rules` 数组、默认规则 `builtins`、已禁用的默认规则 ID、默认规则扩展和条件树。自定义规则的数组下标从 **0** 开始，下标只对本次读取返回的 `revision` 有效。默认规则以 `builtins[].id` 标识。

## 单项修改

`PATCH /api/v1/admin/settings/intelligence-rules`

每次提交一个操作及读取到的 `revision`。例：新增自定义规则：

```json
{
  "revision": "替换为读取返回的 revision",
  "operation": "add_rule",
  "rule": {"name": "自定义题", "type": "contains", "pattern": "题目关键词", "enabled": true}
}
```

- `add_rule`：提供 `rule`；追加一条完整自定义规则。
- `update_rule`：提供 `index` 和完整 `rule`；替换指定自定义规则。
- `delete_rule`：提供 `index`；删除指定自定义规则。
- `update_builtin`：提供 `builtin_id`，以及需要修改的 `enabled`、`condition`、`reset_condition` 或 `extensions`。`condition` 是 GET 返回的条件树；`reset_condition: true` 恢复原始条件。提供 `extensions` 时会替换该默认规则的全部附加条件。
- `add_builtin_extension`：提供 `builtin_id` 和 `extension`（例如 `{"type":"regex","pattern":"鹈鹕.*自行车"}`），追加一个附加条件。
- `update_builtin_extension`：提供 `builtin_id`、`index` 和 `extension`，替换指定附加条件。
- `delete_builtin_extension`：提供 `builtin_id` 和 `index`，删除指定附加条件。

`contains` 和 `regex` 使用已有规则校验。无效正则、匹配空字符串的正则、未知默认规则或越界下标会返回 HTTP 400，原配置不会被修改。保存成功后返回完整规则与新的 `revision`，可用于下一次修改。

请求示例（`SUB2API_URL` 为服务 HTTPS 地址，`SUB2API_ADMIN_KEY` 为管理员密钥）：

```sh
curl "$SUB2API_URL/api/v1/admin/settings/intelligence-rules" \
  -H "x-api-key: $SUB2API_ADMIN_KEY"

curl -X PATCH "$SUB2API_URL/api/v1/admin/settings/intelligence-rules" \
  -H "x-api-key: $SUB2API_ADMIN_KEY" -H 'Content-Type: application/json' \
  --data '{"revision":"替换为读取返回的 revision","operation":"add_rule","rule":{"name":"自定义题","type":"contains","pattern":"题目关键词","enabled":true}}'
```

修改接口只更改规则配置，不会返回智商上下文中的请求体。其他现有配置、默认规则和附加条件在单项操作中保留。
