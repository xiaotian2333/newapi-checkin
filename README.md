# NewAPI Checkin

这是一个使用 Go 编写的签到服务。用户通过 Linux Do OAuth2 登录后，服务会读取 PostgreSQL 中 `users` 表的 `linux_do_id` 与 `quota`，当额度低于阈值时开放签到；签到成功后，会向 `checkins` 表写入记录，并给 `users.quota` 增加固定额度。

程序启动时会优先读取项目根目录下的 `.env` 文件；如果某个变量已经在系统环境中显式设置，则系统环境变量优先。
HTML 模板会在编译时嵌入最终可执行文件，运行时不依赖外部模板目录。

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

## 运行

```powershell
go mod tidy
go test ./...
go run .
```

启动后访问 `http://127.0.0.1:8080`。

现在可以直接在项目根目录执行：

```powershell
go build
```

如果你不希望自动联网或构建，可以只先执行：

```powershell
go mod tidy
go test ./...
```

确认通过后再手动运行服务。

## 数据库约定

- `users` 表至少包含 `id`、`linux_do_id`、`quota`。
- `checkins` 表复用现有结构：
  - `user_id bigint`
  - `checkin_date varchar(10)`
  - `quota_awarded bigint`
  - `created_at bigint`

## 业务规则

1. 登录成功后，使用 Linux Do 用户信息中的不可变 `id` 匹配 `users.linux_do_id`。
2. 若匹配不到用户或匹配到多条用户，直接报错。
3. 当 `quota >= QUOTA_THRESHOLD` 时，不允许签到。
4. 当日未签到且 `quota < QUOTA_THRESHOLD` 时，写入 `checkins`，并将 `users.quota` 增加 `QUOTA_INCREMENT`。
