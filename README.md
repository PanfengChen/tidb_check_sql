# check_sql — TiDB SQL 兼容性检查工具

> Go 版 · 单一静态二进制 · 零依赖 · 离线可用

## 功能

从 MyBatis Mapper XML 文件中提取 SQL 语句，在 TiDB 中执行 `EXPLAIN` 检查兼容性。对不兼容的 SQL 自动给出改写建议（支持 MySQL 和 Oracle 语法）。

**主要特性**：
- 覆盖 19 种 MyBatis 动态标签（含自定义标签扩展）
- 39 条兼容性检测规则（MySQL + Oracle → TiDB）
- 结构化输出：HTML / JSON / CSV / 终端
- 结果表兼容 Perl 版 `check_sql`

---

## 快速开始

```bash
# 1. 上传二进制到 Linux 服务器
scp check_sql user@your-server:/usr/local/bin/

# 2. 赋予执行权限
chmod +x /usr/local/bin/check_sql

# 3. 准备 XML 文件目录
mkdir -p ~/check_demo/xmls
cp /path/to/your/mapper/*.xml ~/check_demo/xmls/

# 4. 运行
check_sql ~/check_demo/xmls/ -host 10.0.0.1 -port 4000 -user root -schema test

# 5. 查看 HTML 报告
# 默认生成 check_report_YYYYMMDD_HHMMSS/ 目录，打开 index.html
```

---

## 用法说明

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-dir` | `.` | XML 文件目录 |
| `-host` | `127.0.0.1` | TiDB 服务器 IP |
| `-port` | `4000` | TiDB 端口 |
| `-user` | `root` | 数据库用户 |
| `-password` | `""` | 数据库密码 |
| `-schema` | `test` | 目标数据库 |
| `-table` | `test.all_sqls` | 结果存储表 |
| `-sys` | `default` | 业务系统名称 |
| `-output` | `html` | 输出格式：`html` / `json` / `csv` / `excel` / `terminal` |
| `-config` | — | TOML 配置文件路径 |
| `-version` | — | 打印版本信息（版本号 / commit / 构建时间）并退出 |

### 查看版本

```bash
check_sql -version
# check_sql v1.0.0
#   commit: f4b41a6
#   built:  2026-08-14T02:18:10Z
```

版本号在编译时通过 `-ldflags` 注入，默认值为 `dev`。正式发布请用 `make build` 或 `make linux`（见下方「编译（开发者）」章节）。

### 基本用法

```bash
# 最简用法（位置参数兼容 Perl 版）
check_sql ./xmls/

# 指定 TiDB 连接
check_sql ./xmls/ -host 10.0.0.1 -port 4000 -user root -schema mydb

# 终端输出
check_sql ./xmls/ -host 10.0.0.1 -port 4000 -output terminal

# JSON 输出
check_sql ./xmls/ -output json
```

### 配置文件模式

创建 `db.toml`：

```toml
[database]
host = "10.0.0.1"
port = 4000
user = "root"
password = ""
schema = "test"
table = "test.all_sqls"

[output]
format = "html"
verbose = true
```

```bash
check_sql -config ./db.toml -dir ./xmls/
```

> CLI 参数优先级高于配置文件。

---

## 输出说明

### HTML 报告（默认）

运行后在当前目录自动创建 `check_report_YYYYMMDD_HHMMSS/` 目录，包含：

```
check_report_20260804_150000/
└── index.html    # 自包含单文件，双击即可在浏览器打开
```

报告包含：
- 统计卡片（总数 / 通过 / 失败 / 通过率）
- 通过率进度条（绿 ≥90% / 黄 ≥60% / 红 <60%）
- 全部 / 失败 / 通过三 Tab 筛选
- 失败 SQL 默认展开，包含：
  - 📄 原始 SQL（从 XML 提取，语法高亮）
  - 🧪 测试 SQL（参数替换后发送给 TiDB 的 SQL）
  - ❌ 错误信息（TiDB 返回的错误码和消息）
  - 💡 改写建议（蓝色卡片，含分类标签 + 改写示例 SQL + 参考文档链接）

### JSON 输出

```bash
check_sql ./xmls/ -output json
# 生成 check_report_XXX/report.json
```

`details` 中每条 SQL 的 `suggestions` 字段是结构化的对象数组，每个对象拆分为 `category` / `title` / `suggestion` / `rewrite` / `reference` 五个字段。

> ⚠️ **Breaking change**：本次把原来的 `suggestion`（单个 JSON 字符串）字段重命名为 `suggestions`（对象数组）。消费旧 JSON 的下游脚本/看板需同步更新字段名。

```json
{
  "modname": "OrderMapper",
  "sqlid": "findById",
  "oldsql": "select * from orders",
  "testsql": "select * from orders",
  "matched": "F",
  "errmsg": "Error 1045 ...",
  "suggestions": [
    {
      "category": "syntax-removal",
      "title": "SQL_CALC_FOUND_ROWS 不支持",
      "suggestion": "去掉 SQL_CALC_FOUND_ROWS ...",
      "rewrite": "移除 SQL_CALC_FOUND_ROWS",
      "reference": ""
    }
  ]
}
```

### Excel 输出

```bash
check_sql ./xmls/ -output excel
# 生成 check_report_XXX/report.xlsx
```

Excel 表格中 `suggestion` 拆分为 `category` / `title` / `suggestion` / `rewrite` / `reference` 五列，一条 SQL 有多条建议时展开为多行，便于筛选和排序。首行表头冻结、加粗高亮，单元格自动换行。

### CSV 输出

```bash
check_sql ./xmls/ -output csv
# 生成 check_report_XXX/report.csv
```

### 终端输出

```bash
check_sql ./xmls/ -output terminal
# 输出到 stdout，适合 CI/CD 管道
```

---

## 结果表说明

工具自动在 TiDB 中创建结果表（默认 `test.all_sqls`），结构如下：

| 列名 | 类型 | 说明 |
|------|------|------|
| `sys` | VARCHAR(10) | 业务系统名称 |
| `modname` | VARCHAR(100) | XML 文件名（模块名） |
| `sqlid` | VARCHAR(100) | SQL 语句 ID |
| `oldsql` | MEDIUMTEXT | 原始 SQL（从 XML 提取） |
| `testsql` | MEDIUMTEXT | 测试 SQL（参数替换后） |
| `errmsg` | VARCHAR(512) | EXPLAIN 错误信息 |
| `matched` | CHAR(2) | `T`=兼容，`F`=不兼容 |
| `remark` | VARCHAR(512) | 备注 |
| `suggestion` | TEXT | 改写建议（JSON 格式，Perl 版忽略此列） |

> **升级提示**：`suggestion` 列是后加的。用旧版本建过结果表的，新版本启动时会先查 `information_schema`，**只在缺列时**执行一次 `ALTER TABLE ... ADD COLUMN suggestion TEXT`；列已存在则完全不碰 DDL，所以常规运行只需要 `SELECT/INSERT/UPDATE/CREATE`。仅当确实要从旧表升级、且账号没有 `ALTER` 权限时，需要手工加列或删除旧表重建。

查询示例：

```sql
-- 查看全部结果
SELECT sys, modname, sqlid, matched, errmsg FROM test.all_sqls;

-- 查看不兼容的 SQL（含改写建议）
SELECT sys, modname, sqlid, testsql, errmsg, suggestion
FROM test.all_sqls
WHERE matched = 'F';

-- 按系统统计兼容率
SELECT sys,
       COUNT(*) AS total,
       SUM(CASE WHEN matched='T' THEN 1 ELSE 0 END) AS pass,
       SUM(CASE WHEN matched='F' THEN 1 ELSE 0 END) AS fail
FROM test.all_sqls
GROUP BY sys;
```

---

## MyBatis XML 支持范围

### SQL 语句类型

`<select>` / `<insert>` / `<update>` / `<delete>` / `<sql>` / `<dynamicselect>` / `<dynamicupdate>` / `<dynamicinsert>`

### 动态 SQL 标签

`<where>` / `<set>` / `<if>` / `<choose>` / `<when>` / `<otherwise>` / `<foreach>` / `<trim>` / `<bind>` / `<include>` / `<and>` / `<str>`

### iBATIS 2.x 遗留标签（v1.1.0 起）

兼容老项目常用的 iBATIS 动态写法，条件一律视为 true 展开（与 `<if>` 策略一致），每个展开块恰好产出一个连接词，杜绝 `AND AND` / `WHERE AND` / `in` 后缺括号等畸形 SQL：

- `<dynamic prepend="...">` — 容器；`prepend="WHERE"` 独立充当 WHERE，`prepend="AND"` 接在 `where 1=1` 后
- 条件标签族（14 个）：`<isNotEmpty>` `<isEmpty>` `<isNotNull>` `<isNull>` `<isEqual>` `<isNotEqual>` `<isGreaterThan>` `<isGreaterEqual>` `<isLessThan>` `<isLessEqual>` `<isPropertyAvailable>` `<isNotPropertyAvailable>` `<isParameterPresent>` `<isNotParameterPresent>`
- `<iterate open close conjunction>` — 展开为单元素哑值列表（如 `in ('20230302')`），属性为 `conjunction`（非 MyBatis 的 separator）

### 参数占位符

`#{param}` / `#{param,jdbcType=XXX}` / `${param}` / `#{item.field}` / `#param#` / `'#param#'`

分页位置按上下文类型化：`LIMIT #n#` → `LIMIT 100`、`OFFSET #m#` / `OFFSET #{m}` → `OFFSET 10`、`ROWNUM BETWEEN #a# AND #b#` → `ROWNUM BETWEEN 1 AND 20`、`ROWNUM <= #n#` / `< #n#` → `ROWNUM <= 10`（比较符保留）、`LIMIT #s#,#c#` → `LIMIT 10, 10`、`FETCH FIRST #c# ROW(S) ONLY` → `FETCH FIRST 100 ROWS ONLY`，避免 OFFSET 后跟日期字符串的假报错。

### 展开策略

采用保守展开——让 EXPLAIN 覆盖尽可能多的 SQL 路径：
- `<if>` → 视为 true 展开
- `<choose>` → 优先 otherwise，否则第一个 when
- `<foreach>` → 展开一次哑值迭代
- `<include>` → 同文件内 `<sql>` 片段内联替换

### `<iterate>` 两种语义（v1.1.0+）

iBATIS `<iterate>` 有两种用法，按 **`open`/`close` 是否成对出现**区分：

| 形态 | 判定 | 展开 |
|------|------|------|
| 值列表 | `open="(" close=")"` 成对 | body 是参数列表 → 展开为单元素哑值 `in ('20230302')` |
| 语句级/批量 | 无 open/close（常 `conjunction=";"`） | body 就是 SQL（set 片段或整条语句）→ 原样保留一次（条件视为 true），参数照常替换，产出单条可 EXPLAIN 的代表 UPDATE，不模拟多语句批 |

---

## 兼容性规则一览

### 通用 MySQL 规则（16 条）

| # | 规则 |
|---|------|
| 1 | `SYSDATE()` → `NOW()` |
| 2 | `SQL_CALC_FOUND_ROWS` 移除 |
| 3 | `INSERT DELAYED` 移除 |
| 4 | `FULLTEXT` 索引不支持 |
| 5 | `SPATIAL` 索引不支持 |
| 6 | 存储过程/函数/触发器不支持 |
| 7 | `FOR UPDATE NOWAIT` 版本要求 |
| 8 | `LOCK IN SHARE MODE` → `FOR SHARE` |
| 9 | `WITH ROLLUP` 支持有限 |
| 10 | `PARTITION BY` 差异 |
| 11 | `REPLACE INTO` 行为差异 |
| 12 | 窗口函数兼容性 |
| 13 | CTE 版本要求 |
| 14 | MySQL 版本条件注释 |
| 15 | `ON DUPLICATE KEY UPDATE` 行为差异 |
| 16 | 用户自定义变量 `@var` 不支持 |

### Oracle 专属规则（23 条）

| # | 规则 |
|---|------|
| 17 | `TO_CHAR` / `TO_NUMBER` → `CONVERT` |
| 18 | `TO_DATE` → `STR_TO_DATE`（格式大小写敏感） |
| 19 | `NVL` → `IFNULL` / `NVL2` → `IF` |
| 20 | `DECODE` → `CASE WHEN` |
| 21 | `(+)` 外连接 → `LEFT JOIN` |
| 22 | `ROWNUM` → `LIMIT` / `ROW_NUMBER()` |
| 23 | `CONNECT BY` → `WITH RECURSIVE` |
| 24 | `MINUS` → `EXCEPT` |
| 25 | `||` 拼接 → `CONCAT` |
| 26 | `DELETE` 不支持表别名 |
| 27 | `MERGE INTO` → `INSERT ON DUPLICATE KEY UPDATE` |
| 28 | `FULL OUTER JOIN` → `LEFT JOIN UNION RIGHT JOIN` |
| 29 | `LENGTH()` → `CHAR_LENGTH()`（字节/字符差异） |
| 30 | 空串 `''` ≠ `NULL`（Oracle/TiDB 差异） |
| 31 | `SUBSTR` 下标从 1 开始（非 0） |
| 32 | `LISTAGG` → `GROUP_CONCAT` |
| 33 | `TRUNC` → `TRUNCATE` / `CAST AS DATE` |
| 34 | `ADD_MONTHS` → `DATE_ADD`（月末行为差异） |
| 35 | `SYSTIMESTAMP` → `CURRENT_TIMESTAMP(6)` |
| 36 | `SYS_GUID()` → `UUID()` |
| 37 | `NULLS FIRST/LAST` 排序差异 |
| 38 | 派生表必须加别名 |
| 39 | 同表 `INSERT...SELECT` 需别名 |

---

## 编译（开发者）

```bash
git clone <repo>
cd check_sql_go

# 编译 Linux 静态二进制
make linux
# → check_sql (约 8 MB, statically linked, stripped)
# 版本号通过 -ldflags 注入，默认 v1.0.0，可用 VERSION 覆盖：
make linux VERSION=v1.1.0

# 本地开发编译
make build

# 运行测试
make test
```

编译要求：
- Go 1.24+
- `CGO_ENABLED=0`（强制纯静态编译）
- 驱动：`go-sql-driver/mysql`（纯 Go 实现）

验证二进制：

```bash
file check_sql          # → statically linked, stripped
ldd check_sql           # → not a dynamic executable
```

---

## 与 Perl 版对比

| 项目 | Perl 版 | Go 版 |
|------|---------|-------|
| 部署 | Perl + 6 模块 + C 库 | 1 个文件 (~5.8 MB 静态) |
| 动态标签 | 8 种 | 19 种 |
| 改写建议 | 无 | 39 条规则 |
| 输出格式 | 终端 | HTML / JSON / CSV / 终端 |
| 结果表 | 7 列 | 8 列（+suggestion，兼容 Perl 版） |

---

## 常见问题

### Q: 需要安装 Go 吗？

**不需要。** `check_sql` 是静态编译的二进制文件，拷贝到 Linux 服务器直接运行。只有从源码编译时才需要 Go。

### Q: 需要安装数据库驱动吗？

**不需要。** 驱动已静态编译进二进制。`ldd check_sql` 返回 `not a dynamic executable`。

### Q: 如何连接 TiDB？

```bash
check_sql ./xmls/ -host <tidb_ip> -port 4000 -user root -password ''
```

确保 TiDB 端已创建目标数据库：`CREATE DATABASE IF NOT EXISTS test;`

### Q: 工具会修改数据库数据吗？

**不会。** 工具仅执行 `EXPLAIN`，不发送 `SELECT`/`INSERT`/`UPDATE`/`DELETE` 等实际执行语句。结果表 `all_sqls` 是唯一的写入目标。

### Q: 如何检查多个系统的 XML？

**当前版本每次运行只检查一个系统。** 一个系统写一份 TOML，用数组表语法声明：

```toml
[[check]]
name = "order_sys"
xml_dir = "./xmls/order"
schema = "order_db"
```

然后按系统分别运行：

```bash
check_sql -config ./order.toml
check_sql -config ./user.toml
```

> ⚠️ 两点容易踩：
> - 不能写成 `[check]` + `systems = [...]`——该字段是数组表（`[[check]]`），写成表会让 TOML 解析直接失败。
> - 配置文件里声明**多个** `[[check]]` 时，当前版本会明确报错退出，而不是只跑第一个（以前的版本会静默丢弃其余系统，并把所有结果记到同一个 `sys` 下，跨系统同名 SQL 会互相覆盖）。
>
> 命令行上的 `-sys` / `-dir` / `-schema` 优先级高于配置文件；未显式指定时以配置文件为准。`-sys` 必须是配置里已声明的系统名称（没有配置文件时可用它给默认系统命名）；配置文件里有多个 `[[check]]` 时必须用 `-sys` 指定本次跑哪个，`-dir` / `-schema` 只能配合 `-sys` 使用。同名系统会被拒绝加载。

### Q: 改写建议的准确性如何保证？

改写建议基于 39 条规则匹配 SQL 错误码和语法模式。部分建议标注了行为差异（如 NULL 处理、自增 ID 分配等），实际改写时需结合业务逻辑确认。建议始终在测试环境验证改写后的 SQL。

---

## 测试现状

当前仓库含 **62 个测试**（`advisor` / `checker` / `parser` / `report` / `sql` 五个包），覆盖 iBATIS 遗留标签解析、`<iterate>` 批量迭代、分页位参数替换，以及 `checker` 的错误消息截断（含多字节边界）、结果表 schema 预检等高风险路径。

相对约 3600 行 Go 代码（其中非测试代码约 2600 行），**测试覆盖仍然偏薄**，`checker` 的核心判定路径（EXPLAIN 结果归类、错误码映射）依赖实机 TiDB，尚无单测。按优先级补充：

- **P0 `checker` 剩余纯函数**：错误消息归类与 `truncateErr` 已覆盖；下一步把 EXPLAIN 结果 → matched 状态判定抽成纯函数，用表驱动测试锁住（成本最低、收益最高，因为判对判错都落在这里）
- **P1 `config`**：TOML 解析的缺省值、非法配置与多系统配置
- **P2 端到端**：以 `xmls/` 下的真实 mapper 做金标准快照测试；TiDB 连通性需实机环境

> 验证方式：`go build ./... && go vet ./... && go test ./...`（本次在 go1.24.7 上全绿；`go.mod` 声明 `go 1.24.0`，同一 minor 线）。
