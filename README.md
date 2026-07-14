# 工时管理 · Todo & Timeline

一个自托管的个人工时与待办管理系统：管理待办（支持分组与子任务）、按"几点几分到几点几分"记录工作、在 **24 小时时间轴** 上可视化，并自动统计每日 / 每周工作情况，记录每日提升。

- **极简部署**：单二进制 + 单个 SQLite 文件，`docker compose up` 即用
- **不安装数据库**：SQLite 以文件形式内嵌，无需独立数据库服务
- **省资源**：Go 静态二进制，运行内存约 15–30MB；Docker 镜像基于 `scratch`，约 15MB
- **离线可用**：前端单页 HTML 原生 JS，无 CDN 依赖，打包进二进制

## 功能

| 模块 | 说明 |
|---|---|
| 待办管理 | 分组、子任务（多级）、状态（待办/进行中/完成）、优先级、截止日期 |
| 时间追踪 | 一键开始/停止计时器；也可事后补录起止时间 |
| 24h 时间轴 | 按分组着色的一天时间块，悬停查看详情，"今天"叠加当前时刻线 |
| 每日分析 | 总工时、分组占比、最长专注块、时段分布（上午/下午/晚上/深夜）、环比昨日 |
| 每周分析 | 每日趋势、最高产的一天、分组分布、环比上周 |
| 每日提升 | 记录当天收获与反思，随日分析一起保存 |

分析全部基于本地规则统计，**按需计算、零外部依赖、零成本、数据不出本地**。

## 快速开始

```bash
# 在项目目录下
docker compose up -d --build
# 若未安装 compose 插件，改用： docker-compose up -d --build
```

打开浏览器访问 `http://localhost:8080` 即可。数据文件保存在 `./data/todo.db`，可随目录整体备份。

> 不想用 compose？也可以直接 `docker run`：
> ```bash
> mkdir -p data
> docker build -t todo .
> docker run -d --name todo -p 8080:8080 -v "$PWD/data:/data" todo
> ```

## 时区

容器默认时区 `Asia/Shanghai`。如需修改，编辑 `docker-compose.yml` 里的 `TZ` 环境变量（如 `UTC`、`America/New_York`），重建容器即可。时区影响"今天/本周"的判定。

## 本地开发（不用 Docker）

```bash
go run .                       # 默认 DB_PATH=./data/todo.db  PORT=8080
DB_PATH=./data/todo.db PORT=8080 go run .
```

运行测试：

```bash
go test ./...
```

## 配置（环境变量）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DB_PATH` | `/data/todo.db`（容器） / `./data/todo.db`（本地） | SQLite 文件路径 |
| `PORT` | `8080` | 监听端口 |
| `TZ` | `Asia/Shanghai` | 时区 |

## API 一览

所有接口前缀 `/api`，JSON 交互。

- `GET/POST/PUT/DELETE /api/groups`
- `GET/POST /api/todos` · `PUT/DELETE /api/todos/{id}` · `PATCH /api/todos/{id}/status`
- `GET /api/time-entries?date=YYYY-MM-DD` · `POST /api/time-entries` · `PUT/DELETE /api/time-entries/{id}`
- `POST /api/time-entries/start` · `POST /api/time-entries/stop` · `GET /api/time-entries/active`
- `GET/PUT /api/summaries/daily?date=YYYY-MM-DD`
- `GET /api/analysis/daily?date=YYYY-MM-DD`
- `GET /api/analysis/weekly?week=YYYY-Www`

## 项目结构

```
todo/
├── main.go                # 入口，embed web 资源
├── internal/
│   ├── db/                # SQLite 连接 + schema（embed）
│   ├── models/            # 数据结构
│   ├── store/             # 数据访问层（groups/todos/time_entries/summaries）
│   ├── handlers/          # HTTP 处理器
│   ├── analysis/          # 每日/每周规则统计
│   ├── server/            # 路由 + 静态资源服务
│   └── tutil/             # 时间/格式工具
├── web/                   # 前端单页（embed 进二进制）
├── Dockerfile             # 多阶段构建 → scratch
└── docker-compose.yml
```

## 数据备份

只需备份 `./data/todo.db`（及可能的 `-wal`/`-shm`）。停止容器后复制文件即可。SQLite 单文件即全部数据。
