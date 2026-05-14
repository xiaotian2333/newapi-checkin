# NewAPI Checkin

这是一个使用 Go 编写的签到服务，支持 **PostgreSQL** 和 **MySQL** 双数据库驱动。用户通过 Linux Do OAuth2 登录后，服务会从 `users` 表读取 `linux_do_id` 与 `quota`，当额度低于阈值时开放签到；签到成功后，向 `checkins` 表写入记录，并给 `users.quota` 增加区间随机额度。

程序启动时会优先读取项目根目录下的 `.env` 文件；如果某个变量已经在系统环境中显式设置，则系统环境变量优先。
前端采用单页面模式，用户侧始终停留在 `/`，由浏览器内部状态控制页面展示。根目录 `assets` 下的 HTML、CSS、JS 资源都会在编译时嵌入最终可执行文件，运行时不依赖外部模板或静态资源目录。

## 环境变量

可参考 `.env.example`：

- `DATABASE_URL`：主库连接 URL，支持 `postgres://` 和 `mysql://` 协议，自动检测驱动。
- `LOGBASE_URL`：日志库连接 URL（可选）。为空时日志写入主库，可指定不同的数据库类型。
- `LINUXDO_CLIENT_ID`：Linux Do OAuth2 Client ID。
- `LINUXDO_CLIENT_SECRET`：Linux Do OAuth2 Client Secret。
- `LINUXDO_REDIRECT_URI`：OAuth2 回调地址。
- `JWT_SECRET`：签发登录态 Cookie 的密钥。
- `LISTEN_ADDR`：监听地址，默认 `:8080`。
- `QUOTA_THRESHOLD`：额度阈值，默认 `10000000`。
- `QUOTA_INCREMENT_MIN`：签到成功后增加额度的最小值，默认 `10000000`。
- `QUOTA_INCREMENT_MAX`：签到成功后增加额度的最大值，默认 `10000000`。
- `CHECKIN_POW_ENABLED`：是否启用签到前的浏览器 PoW，默认 `true`。
- `CHECKIN_POW_DIFFICULTY`：PoW 难度，单位为前导零 bit，默认 `18`。
- `CHECKIN_POW_TTL_SECONDS`：PoW 挑战有效期，默认 `300` 秒。
- `CHECKIN_TURNSTILE_ENABLED`：是否启用签到前的验证码，默认 `false`。
- `CHECKIN_TURNSTILE_TYPE`：验证码服务商类型，可选 `cloudflare` 或 `hcaptcha`，默认 `cloudflare`。
- `CHECKIN_TURNSTILE_SITE_KEY`：Cloudflare Turnstile 站点密钥。
- `CHECKIN_TURNSTILE_SECRET_KEY`：Cloudflare Turnstile 服务端密钥。
- `CHECKIN_CAPTCHA_SITE_KEY`：hCaptcha 站点密钥。
- `CHECKIN_CAPTCHA_SECRET_KEY`：hCaptcha 服务端密钥。
- `LEADERBOARD_LIMIT`：排行榜返回条数，默认 `10`。
- `TRUST_PROXY_HEADERS`：是否信任反向代理传递的客户端 IP 头，默认 `false`。

签到奖励始终按 `0.01 元` 为最小单位发放。按当前额度换算规则，随机结果一定是 `5000` 的倍数；如果配置区间内不存在这样的值，服务会在启动时报错。
当 `CHECKIN_TURNSTILE_ENABLED=true` 时，必须同时启用 `CHECKIN_POW_ENABLED=true`。

## 数据库支持

服务启动时会自动根据 `DATABASE_URL` 的协议前缀检测数据库类型：

| 协议前缀 | 驱动 |
|----------|------|
| `postgres://` / `postgresql://` | PostgreSQL |
| `mysql://` / `mysql+tcp://` | MySQL |

主库与日志库可以使用不同类型的数据库，例如主库用 PostgreSQL、日志库用 MySQL，只需配置对应的 URL 即可。

## 编译

```powershell
go build
```

启动后访问 `http://127.0.0.1:8080`。

## 前后端接口

- `GET /`：返回内嵌的单页面入口。
- `GET /assets/*`：返回内嵌静态资源。
- `GET /api/info`：返回当前登录态、用户信息、签到资格、PoW 难度、验证码配置、页面提示和当天签到金额排行榜。
- `POST /api/checkin/task`：先校验 `captcha_token`，成功后即时下发 PoW 任务。
- `POST /api/checkin`：提交 PoW 解并执行签到，成功后同步返回最新排行榜。
- `POST /api/logout`：清理当前登录态。
- `GET /login`：跳转到 Linux Do OAuth2 授权页。
- `GET /auth/callback`：处理 OAuth2 回调，结束后重定向回 `/`。

## 数据库约定

服务启动时会校验以下表结构，缺少必要字段会直接报错。

- `users` 表至少包含：
  - `id`（主键）
  - `linux_do_id`（用户标识，与 Linux Do 返回的不可变 ID 匹配）
  - `quota`（当前额度）
  - `username`（用户名）

- `checkins` 表至少包含：
  - `user_id bigint`
  - `checkin_date varchar(10)`
  - `quota_awarded bigint`
  - `created_at bigint`

- `logs` 表（主库或独立日志库均可）至少包含：
  - `user_id bigint`
  - `created_at bigint`
  - `type bigint`
  - `content text`
  - `username text`
  - `token_name text`
  - `model_name text`
  - `quota bigint`
  - `prompt_tokens bigint`
  - `completion_tokens bigint`
  - `use_time bigint`
  - `is_stream boolean`
  - `channel_id bigint`
  - `channel_name text`
  - `token_id bigint`
  - `group text`
  - `ip text`
  - `other text`
  - `request_id varchar(64)`

## 业务规则

1. 登录成功后，使用 Linux Do 用户信息中的不可变 `id` 匹配 `users.linux_do_id`。
2. 若匹配不到用户或匹配到多条用户，直接报错。
3. 当 `CHECKIN_TURNSTILE_ENABLED=true` 且用户可签到时，用户点击签到后会在按钮下方展开验证码（根据 `CHECKIN_TURNSTILE_TYPE` 加载 Cloudflare Turnstile 或 hCaptcha）；验证成功后前端会自动请求 PoW 任务。
4. 当 `CHECKIN_POW_ENABLED=true` 且用户可签到时，页面会提前展示 PoW 难度，但只会在用户通过验证码后才获取带时效的 PoW 任务并开始计时。
5. 当前前端不会跳转到独立内部页面，而是通过 `/api/info` 和前端状态变量切换页面展示。
6. 当 `quota >= QUOTA_THRESHOLD` 时，不允许签到。
7. 当日未签到且 `quota < QUOTA_THRESHOLD` 时，服务会在 `QUOTA_INCREMENT_MIN` 到 `QUOTA_INCREMENT_MAX` 之间随机出本次奖励额度，并保证结果是 `5000` 的倍数，然后写入 `checkins`，并将 `users.quota` 增加该实际值。
8. 签到成功后，接口会直接返回本次实际获得的额度，并在事务提交后异步向 `logs` 表写入一条 `type = 4` 的日志，内容格式为 `用户签到，获得额度 ￥20.000000 额度`。日志写入失败不阻塞签到主流程，仅记录警告日志。
9. 服务启动时会预热一次当天签到金额排行榜缓存；每次签到成功后会基于 `checkins` 表重新读取当天前 N 名（由 `LEADERBOARD_LIMIT` 配置）并刷新缓存。
10. 排行榜按 `quota_awarded` 降序排列；若签到金额相同，则按 `created_at` 升序排列，先签到的用户排在前面。
11. 服务端 500 错误不向客户端暴露具体错误详情，统一返回 `"服务内部错误"`，避免敏感信息泄露。

## 友情链接

- [Linux Do 社区](https://linux.do/)  
