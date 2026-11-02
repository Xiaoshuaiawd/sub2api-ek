# OpenAI WARP 出口

OpenAI 账号的服务端模型请求、WebSocket、账号测试、令牌交换、用量和后台登录请求使用 WARP。请求不再使用账号代理、备用代理或直连作为退路；WARP 不可用时请求失败。其他平台保留原有代理策略。

`docker-compose.yml`、`docker-compose.local.yml`、`docker-compose.standalone.yml` 和 `docker-compose.dev.yml` 包含 `warp-proxy`。服务默认通过 `socks5h://warp-proxy:1080` 访问本节点 sidecar。Linux 主机需要 `/dev/net/tun` 和允许容器使用 `NET_ADMIN`；WARP 注册数据保存在 `warp-data` 卷。`.env` 中的 `WARP_LICENSE_KEY` 可选。运行 `docker compose ... ps warp-proxy` 查看状态；sidecar 的健康检查会检查 Cloudflare trace 的 `warp=on` 或 `warp=plus`。

`warp-entrypoint.sh` 在启动代理之前设置容器内 nftables 出口限制：代理进程的新外连只能使用 `CloudflareWARP` 接口，并禁止其使用 Docker 内置 DNS。域名解析由 `warp-dns.py` 通过该代理发送加密 DNS 请求；代理或 DNS 不可用时请求失败。规则无法安装时容器直接启动失败。上线前应在目标主机验证 WARP 正常时的出口，以及断开 WARP 后请求确实失败；仅检查容器健康状态不足以验证断线行为。

升级已有部署时，必须把新版 Compose 文件、`warp-entrypoint.sh` 和 `warp-dns.py` 一起放在同一目录，再启动新版服务；只拉取应用镜像不会自动更新旧部署目录。`docker-deploy.sh` 新安装时会同时下载这些文件。

非 Compose 安装需自行运行可用的 WARP SOCKS 代理，并设置 `OPENAI_WARP_PROXY_URL`。部署人员必须确认该地址确实使用 WARP；URL 本身无法证明出口身份。

独立登录 Worker 必须能连接同一个 WARP 出口。若其网络无法解析应用容器中的 `warp-proxy`，设置服务端 `OPENAI_WARP_WORKER_PROXY_URL` 为 Worker 可访问的地址，两个地址应指向同一 sidecar，避免登录与令牌交换使用不同出口。Docker 配置默认不向宿主机或公网发布该代理端口；跨主机访问需要由部署人员自行设置受限的网络通路。

Worker 会向其外部登录协议程序传递必填代理地址，但这些外部程序不由主服务控制。必须在 Worker 所在主机或容器另行限制其出站网络，使其不能绕过 WARP 代理；完成隔离后，在 Worker 进程环境中设置 `OPENAI_WARP_WORKER_NETWORK_ISOLATED=true`，否则登录、会话导出及 TOTP 任务会被拒绝。对于主程序托管的 Worker，该环境变量需设置在主程序环境中。旧版 Worker 不具备该确认能力，需要升级后才能领取任务。该变量是管理员的隔离确认，不会自动配置或验证防火墙；服务端 sidecar 的防直连规则也不覆盖独立 Worker 的网络命名空间。

第三方 OpenAI OAuth 插件的独立进程不能由主程序证明遵守代理配置，因此当前会拒绝该路径。Prism 浏览器适配器和 Session Studio 的远端浏览器同样无法在本项目中证明出口，相关请求会明确失败。用户本机浏览器中的 OAuth 授权页面仍使用用户设备的网络。

Codex 票据与出口绑定。旧出口生成的票据不能在 WARP 出口复用，需重新获取；WARP 出口 IP 改变时也可能需要刷新票据。代理、令牌和票据设置不会被自动迁移到新的出口。

如果多台 Sub2API 实例共享票据数据库，单纯在每台机器上使用相同的 `warp-proxy` 主机名并不代表出口相同。需要让这些实例使用同一个可达的 WARP 代理出口，或避免跨不同出口共享票据；否则票据可能因出口不一致被上游拒绝。
