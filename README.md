# 标签驱动的待办与工时管理

一个自托管、单用户的待办、时间追踪和分析应用。Go 服务与原生前端一起打包为单二进制，数据保存在本地 SQLite 文件中。

## 当前需求与业务规则

### 标签

- 标签是唯一的分类方式：名称、说明、颜色，以及“是否计入标签分布统计”。
- 普通标签默认计入统计；系统状态标签“进行中”“已完成”默认不计入。
- 一个任务可拥有多个标签；任务内标签顺序保存在 `todo_tags.tag_order`。
- 第一个标签是主标签，时间轴中关联该任务的记录优先显示主标签颜色。

### 待办

- 支持任务、任意层级子任务、优先级、截止日期、待办/进行中/已完成状态。
- 勾选完成时自动追加“已完成”标签；重新打开任务时自动移除该标签。
- “今日任务”是一个标签视图：仅展示带“进行中”标签的任务。
  - 加入进行中：添加“进行中”标签。
  - 移出进行中：删除“进行中”标签。
- 待办页支持多标签筛选：
  - 已选标签按全局 **AND** 或 **OR** 匹配；
  - 标签再次点击会变成 **NOT** 条件；
  - NOT 标签命中的任务会被排除。

### 时间追踪

- 可开始/停止计时，也可补录开始与结束时间。
- 时间记录可关联任务；关联任务时自动使用其主标签作为默认标签与时间轴颜色。
- 顶部导航栏会在计时中显示任务名称、实时经过时间和结束按钮；未计时时自动隐藏。
- 支持日时间轴、周时间轴，以及进行中的实时计时显示。
- 今日页的“今日记录”使用单日纵向 24 小时时间轴，初始定位在 10:00；任务名称在色块中居中显示，点击色块可编辑记录。
- 时间记录编辑弹窗按“时间 / 任务与标签 / 备注”分区，展示关联任务的标签，并支持直接编辑任务；备注为三行文本框。

### 分析与总结

- 每日分析：工时、完成任务、最长专注、昨日对比、任务/标签占比、时间轴。
- 每周分析：每日趋势、最高产日、上周对比、任务/标签占比、周时间轴。
- 每日总结只有一个 `content` 字段；每周总结同样使用单一 `content` 字段。
- 标签分布统计仅计算 `include_in_stats=true` 的标签，因此“进行中”“已完成”等状态标签不会干扰工时分类。
- 今日概览每次进入今日页都会重新加载任务、活动计时和分析数据；停留在今日页时每分钟重新计算一次。

## 数据模型

| 表 | 核心字段 | 用途 |
|---|---|---|
| `tags` | `name`, `description`, `color`, `include_in_stats` | 标签定义与统计开关 |
| `todos` | `parent_id`, `title`, `status`, `priority`, `due_date` | 任务与子任务 |
| `todo_tags` | `todo_id`, `tag_id`, `tag_order` | 多标签关系和主标签顺序 |
| `time_entries` | `todo_id`, `tag_id`, `start_time`, `end_time`, `note` | 时间记录与关联标签 |
| `daily_summaries` | `date`, `content` | 每日总结 |
| `weekly_summaries` | `week`, `content` | 每周总结 |

应用不依赖数据库外键；删除标签、任务等关联数据由应用逻辑处理。这避免了旧 `groups` 结构迁移至 `tags` 时的外键冲突。

## 快速开始

```bash
docker compose up -d --build
```

访问 `http://localhost:8080`。数据文件默认是 `./data/todo.db`，备份该文件及其可能存在的 `-wal`、`-shm` 文件即可。

本地开发：

```bash
go run .
go test ./...
```

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `DB_PATH` | 容器 `/data/todo.db`；本地 `./data/todo.db` | SQLite 文件路径 |
| `PORT` | `8080` | HTTP 监听端口 |
| `TZ` | `Asia/Shanghai` | 今天与自然周的判定时区 |

## API

主要接口均以 `/api` 为前缀：

- `GET/POST/PUT/DELETE /api/tags`：标签管理
- `GET/POST /api/todos`、`PUT/DELETE /api/todos/{id}`、`PATCH /api/todos/{id}/status`
- `GET/POST/PUT/DELETE /api/time-entries`、`POST /api/time-entries/start|stop`
- `GET/PUT /api/summaries/daily?date=YYYY-MM-DD`
- `GET/PUT /api/summaries/weekly?week=YYYY-Www`
- `GET /api/analysis/daily?date=YYYY-MM-DD`
- `GET /api/analysis/weekly?week=YYYY-Www`

`/api/groups` 仍保留为旧客户端兼容路由；新功能应使用 `/api/tags`。

## 优化建议

### 近期：收敛数据模型

1. 已完成 `time_entries.group_id → tag_id` 的物理迁移；下一步为现有数据库重建 `todos` 与 `todo_tags`，物理删除遗留的 `groups` 表、`todos.group_id`、`todos.today_date` 及旧外键定义。
2. 将 `Group`、`ListGroups` 等内部类型/函数重命名为 `Tag`、`ListTags`，删除 `/api/groups` 兼容路由的约定日期。
3. 给标签筛选写前后端测试，覆盖 AND、OR、NOT、空条件、子任务继承标签与状态标签排除统计。

### 中期：体验与可靠性

1. 给标签筛选增加可分享/可保存的 URL 查询参数，便于在多个常用视图间切换。
2. 为标签和任务删除提供“受影响任务数”预览，以及可恢复的软删除或回收站。
3. 增加数据库迁移版本表，替代依赖 `ALTER TABLE` 错误文本判断的迁移方式。
4. 为长时间运行、跨日时间记录、并发开始计时和时区切换补充集成测试。

### 后续：功能扩展

1. 引入项目/归档标签、周期性任务、截止提醒。
2. 提供 CSV/JSON 导出和完整导入，方便备份与迁移。
3. 如发展为多人使用，再加入登录、所有者字段、权限和审计日志；当前单 SQLite 文件模型适合个人使用。

## 项目结构

```text
internal/db        SQLite 初始化与迁移
internal/models    API/领域数据结构
internal/store     数据访问与业务数据维护
internal/handlers  HTTP API
internal/analysis  每日、每周统计
web/               原生单页前端
```
