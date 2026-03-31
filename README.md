# NewAPI Checkin

这是一个使用 Go 编写的签到服务。用户通过 Linux Do OAuth2 登录后，服务会读取 PostgreSQL 中 `users` 表的 `linux_do_id` 与 `quota`，当额度低于阈值时开放签到；签到成功后，会向 `checkins` 表写入记录，并给 `users.quota` 增加固定额度。

程序启动时会优先读取项目根目录下的 `.env` 文件；如果某个变量已经在系统环境中显式设置，则系统环境变量优先。
前端采用单页面模式，用户侧始终停留在 `/`，由浏览器内部状态控制页面展示。根目录 `assets` 下的 HTML、CSS、JS 资源都会在编译时嵌入最终可执行文件，运行时不依赖外部模板或静态资源目录。

## 环境变量

可参考 `.env.example`：

- `DATABASE_URL`：PostgreSQL 连接串。
- `LINUXDO_CLIENT_ID`：Linux Do OAuth2 Client ID。
- `LINUXDO_CLIENT_SECRET`：Linux Do OAuth2 Client Secret。
- `LINUXDO_REDIRECT_URI`：OAuth2 回调地址。
- `JWT_SECRET`：签发登录态 Cookie 的密钥。
- `LISTEN_ADDR`：监听地址，默认 `:8080`。
- `QUOTA_THRESHOLD`：额度阈值，默认 `10000000`。
- `QUOTA_INCREMENT`：签到成功后增加的额度，默认 `10000000`。
- `CHECKIN_POW_ENABLED`：是否启用签到前的浏览器 PoW，默认 `true`。
- `CHECKIN_POW_DIFFICULTY`：PoW 难度，单位为前导零 bit，默认 `18`。
- `CHECKIN_POW_TTL_SECONDS`：PoW 挑战有效期，默认 `300` 秒。

## 编译

```powershell
go build
```

启动后访问 `http://127.0.0.1:8080`。

## 前后端接口

- `GET /`：返回内嵌的单页面入口。
- `GET /assets/*`：返回内嵌静态资源。
- `GET /api/info`：返回当前登录态、用户信息、签到资格、PoW 难度和页面提示。
- `POST /api/checkin/task`：点击签到后即时下发 PoW 任务。
- `POST /api/checkin`：提交 PoW 解并执行签到。
- `POST /api/logout`：清理当前登录态。
- `GET /login`：跳转到 Linux Do OAuth2 授权页。
- `GET /auth/callback`：处理 OAuth2 回调，结束后重定向回 `/`。

## 数据库约定

- `users` 表至少包含 `id`、`linux_do_id`、`quota`。
- `checkins` 表复用现有结构：
  - `user_id bigint`
  - `checkin_date varchar(10)`
  - `quota_awarded bigint`
  - `created_at bigint`
- `logs` 表至少包含以下字段：
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
3. 当 `CHECKIN_POW_ENABLED=true` 且用户可签到时，页面会提前展示 PoW 难度，但只会在用户点击签到后才获取带时效的 PoW 任务并开始计时。
4. 当前前端不会跳转到独立内部页面，而是通过 `/api/info` 和前端状态变量切换页面展示。
5. 当 `quota >= QUOTA_THRESHOLD` 时，不允许签到。
6. 当日未签到且 `quota < QUOTA_THRESHOLD` 时，写入 `checkins`，并将 `users.quota` 增加 `QUOTA_INCREMENT`。
7. 签到成功后，会额外向 `logs` 表写入一条 `type = 4` 的日志，内容格式为 `用户签到，获得额度 ¥20.000000 额度`。
