# mars_ai

Go 语言股票元数据同步工具，当前用于从同花顺抓取：

- 同花顺行业列表，写入 `stock_sector`
- 行业详情页下的个股列表，写入 `stock`

## 目录结构

```text
cmd/stock-sector/        命令行入口
config/config.json       数据库和同花顺抓取配置
internal/config/         配置读取
internal/db/             MySQL 连接
internal/spider/         同花顺抓取、解析、入库逻辑
```

## 配置

配置文件：`config/config.json`

```json
{
  "mysql": {
    "host": "127.0.0.1",
    "port": 3306,
    "username": "root",
    "password": "fxtkyzwx2021",
    "database": "mars",
    "charset": "utf8mb4"
  },
  "tonghuashun": {
    "sector_url": "https://q.10jqka.com.cn/thshy/",
    "stock_url_template": "https://q.10jqka.com.cn/thshy/detail/code/%d/field/199112/order/desc/page/%d/ajax/1/",
    "user_agent": "Mozilla/5.0 ...",
    "v_js_path": "/etc/v.js",
    "timeout_seconds": 20,
    "stock_max_pages": 80,
    "request_sleep_min_ms": 3000,
    "request_sleep_max_ms": 7000,
    "retry_count": 3,
    "retry_sleep_ms": 10000
  }
}
```

程序会读取 `tonghuashun.v_js_path` 指向的 JS 文件，执行其中的 `v()` 方法，并自动设置请求 Cookie：`v=<生成值>`。

行业详情页的个股分页接口比行业列表页更严格。如果执行 `--stock-only` 时看到：

```text
tonghuashun unauthorized
tonghuashun returned verification page
```

通常需要确认 `v_js_path` 文件存在且其中包含可执行的 `v()` 方法。

验证 `v` Cookie 是否可用：

```bash
go run ./cmd/stock-sector --check-cookie
```

## 常用命令

下载依赖：

```bash
go mod tidy
```

编译检查：

```bash
go test ./...
```

只抓行业并打印，不写库：

```bash
go run ./cmd/stock-sector --dry-run --sector-only
```

抓行业和个股并打印，不写库：

```bash
go run ./cmd/stock-sector --dry-run
```

行业已存在时，只抓个股并打印，不写库：

```bash
go run ./cmd/stock-sector --dry-run --stock-only
```

只写入行业表：

```bash
go run ./cmd/stock-sector --sector-only
```

行业已存在时，只写入股票表：

```bash
go run ./cmd/stock-sector --stock-only
```

写入股票时会按页执行：每成功抓取一页，就立刻写入 `stock`，并更新 `stock_crawl_progress`。如果中途失败，下次继续执行 `--stock-only` 会从上次成功页的下一页继续。

写入行业表和股票表：

```bash
go run ./cmd/stock-sector
```

## 日志

程序使用 Go 标准库 `log` 输出关键执行日志：

- `[crawl] request ...`：请求同花顺接口
- `[crawl] response ...`：同花顺接口响应状态
- `[crawl] unauthorized body preview=...`：同花顺 401 响应体预览
- `[crawl] blocked retry ...`：触发同花顺校验后的退避重试
- `[crawl] sleep ...`：页间/行业间随机等待
- `[crawl] parsed ...`：解析出的行业或股票数量
- `[crawl] stock item ...`：逐条打印当前页解析出的股票
- `[db] insert ...`：插入数据库
- `[db] update ...`：更新数据库
- `[db] saved ...`：本次保存总数

## 数据写入规则

`stock_sector`：

- `sector_id`：同花顺行业代码，例如 `881121`
- `sector_name`：行业名称，例如 `半导体`

`stock`：

- `stock_code`：股票代码
- `stock_name`：股票名称
- `region`：交易所前缀，按代码规则写入 `SH` / `SZ` / `BJ`
- `sector_id`：同花顺行业代码，也就是 `stock_sector.sector_id`
- `sector_name`：行业名称，也就是 `stock_sector.sector_name`

个股分页会优先解析同花顺页面里的总页数；如果页面没有明确总页数，则使用第一页数量作为页大小，后续页面数量小于页大小时认为已到末页，停止继续请求。

当前表没有唯一索引，因此入库时会先查再写：

- `stock_sector` 按 `sector_id` 或 `sector_name` 查已有记录
- `stock` 按 `stock_code` 查已有记录

`stock_crawl_progress`：

- 程序自动创建
- `sector_id`：同花顺行业代码
- `last_success_page`：该行业最后成功写入的页码
- `finished`：该行业是否已经爬完
- `last_error`：最后一次失败原因
