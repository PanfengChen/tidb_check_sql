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
| `-output` | `html` | 输出格式：`html` / `json` / `csv` / `terminal` |
| `-config` | — | TOML 配置文件路径 |

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

### 参数占位符

`#{param}` / `#{param,jdbcType=XXX}` / `${param}` / `#{item.field}` / `#param#` / `'#param#'`

### 展开策略

采用保守展开——让 EXPLAIN 覆盖尽可能多的 SQL 路径：
- `<if>` → 视为 true 展开
- `<choose>` → 优先 otherwise，否则第一个 when
- `<foreach>` → 展开一次哑值迭代
- `<include>` → 同文件内 `<sql>` 片段内联替换

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
# → check_sql (约 5.8 MB, statically linked, stripped)

# 本地开发编译
make build

# 运行测试
make test
```

编译要求：
- Go 1.21+
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

### Q: 如何批量检查多个系统的 XML？

使用配置文件模式，TOML 中可定义多个系统：

```toml
[check]
systems = [
  { name = "order_sys", xml_dir = "./xmls/order", schema = "order_db" },
  { name = "user_sys",  xml_dir = "./xmls/user",  schema = "user_db" },
]
```

### Q: 改写建议的准确性如何保证？

改写建议基于 39 条规则匹配 SQL 错误码和语法模式。部分建议标注了行为差异（如 NULL 处理、自增 ID 分配等），实际改写时需结合业务逻辑确认。建议始终在测试环境验证改写后的 SQL。
# tidb_check_sql
