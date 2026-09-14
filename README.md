# 跑道入口灯报码判读 API

纯后端服务：把夜间巡检时灯控制器导出的**亮/灭采样刻度时长序列**判读为设备播出的 Morse 报码。
技术栈：Go + Gin，测试使用 testify。

## 记录格式

请求体为从**亮灯**开始的正整数采样刻度时长数组，亮灭严格交替，且必须以**亮灯**结束（长度必为奇数）。
可选字段 `tick_micros` 表示每个时长单位对应的整数微秒数；未传时按默认值 `1000`
处理，即数组仍表示毫秒：

```json
{"durations": [100, 100, 300]}
```

下标 0、2、4… 为亮灯脉冲，1、3、5… 为灭灯间隔。服务端会对每个时长执行整数乘法
`微秒时长 = durations[i] × tick_micros`，然后按固定的微秒窗口判读；返回的 `start`、
`end` 和错误 `index` 始终是原始 `durations` 数组下标。`tick_micros` 必须位于
1–1,000,000（含端点）；乘法发生整数溢出时，请求同样失败且不会产生部分结果。

可选布尔字段 `include_trace` 缺省为 `false`。设为 `true` 且解码成功时，响应额外携带
`trace`：判读核心按原始数组顺序为每个时长生成一项，给出原始下标、亮灭角色
（`light_on` / `light_off`）、换算后的微秒值和判读结果
（`dot` / `dash` / `intra_gap` / `inter_gap`）。`inter_gap` 只负责切字，不进入任何
字符的 `pattern`；`characters` 与 `message` 的结构和值与不传该字段时完全一致。
`include_trace` 只接受 JSON 布尔值；`null`、字符串、数字（包括 `1`/`0`）一律以
`400` 指向 `include_trace`。解码失败时仍是原有的首错 `422`，不会夹带 `trace` 或
部分报码。

## 判读规则（全部为闭区间，端点有效）

| 脉冲 | 含义 | 合法窗口（含端点） |
|---|---|---|
| 亮灯 | 点 `.` | 80–120 ms |
| 亮灯 | 划 `−` | 240–360 ms |
| 灭灯 | 字符内间隔 | 80–120 ms |
| 灭灯 | 字符间间隔（负责切分字符） | 240–360 ms |

其余时长一律非法。支持的报码映射表（除此之外的字符组合均非法，禁止空字符）：

```
A=.-   N=-.   R=.-.   K=-.-   S=...   O=---
```

## 请求示例

```bash
curl -X POST "http://localhost:${API_PORT:-8080}/decode" \
  -H 'Content-Type: application/json' \
  -d '{"durations": [100,100,100,100,100, 300, 300,100,300,100,300, 300, 100,100,100,100,100]}'
```

固件若按 500 微秒采样刻度导出，同一组脉冲的数组数值翻倍；显式携带
`"tick_micros": 500` 后，响应仍解出同一报码，字符下标也仍对应输入数组：

```bash
curl -X POST "http://localhost:${API_PORT:-8080}/decode" \
  -H 'Content-Type: application/json' \
  -d '{"tick_micros": 500, "durations": [200,200,200,200,200, 600, 600,200,600,200,600, 600, 200,200,200,200,200]}'
```

未传 `tick_micros` 的旧请求等价于 `"tick_micros": 1000`，不需要修改。

`200 OK` 响应按输入顺序给出报码，以及每个字符覆盖的起止脉冲下标（0 起始、闭区间），可据此逐字符复核：

```json
{
  "message": "SOS",
  "characters": [
    {"char": "S", "pattern": "...", "start": 0, "end": 4},
    {"char": "O", "pattern": "---", "start": 6, "end": 10},
    {"char": "S", "pattern": "...", "start": 12, "end": 16}
  ]
}
```

### 现场复核诊断（`include_trace`）

现场复核报码时需要逐个原始脉冲核对其换算后的归属，在请求中加上
`"include_trace": true`：

```bash
curl -X POST "http://localhost:${API_PORT:-8080}/decode" \
  -H 'Content-Type: application/json' \
  -d '{"durations": [100,100,300, 300, 300,100,100], "include_trace": true}'
```

`message` 与 `characters` 与不传该字段时逐字段一致，响应额外给出按原始数组顺序
排列的 `trace`，可与下标逐项对照（上例解出 `AN`）：

```json
{
  "message": "AN",
  "characters": [
    {"char": "A", "pattern": ".-", "start": 0, "end": 2},
    {"char": "N", "pattern": "-.", "start": 4, "end": 6}
  ],
  "trace": [
    {"index": 0, "role": "light_on",  "micros": 100000, "result": "dot"},
    {"index": 1, "role": "light_off", "micros": 100000, "result": "intra_gap"},
    {"index": 2, "role": "light_on",  "micros": 300000, "result": "dash"},
    {"index": 3, "role": "light_off", "micros": 300000, "result": "inter_gap"},
    {"index": 4, "role": "light_on",  "micros": 300000, "result": "dash"},
    {"index": 5, "role": "light_off", "micros": 100000, "result": "intra_gap"},
    {"index": 6, "role": "light_on",  "micros": 100000, "result": "dot"}
  ]
}
```

`micros` 是 `durations[index] × tick_micros` 的换算结果；`inter_gap`（字间间隔）
只负责切字，因此不出现在任何字符的 `pattern` 里。`include_trace: false` 或不传该
字段时响应中不含 `trace` 键。

## 错误语义

- `422 Unprocessable Entity`：按 `tick_micros` 整数换算后，任一脉冲非法（非正整数、亮/灭时长落在窗口之外、记录以灭灯结尾）或字符不在映射表。
  **整次记录被拒绝，绝不返回部分报码**。响应指出**首个问题脉冲的原始下标**；若问题为未映射字符，`index`
  为该字符首个脉冲的下标（脉冲按下标从小到大检查，损坏记录稳定停在首个异常位置）：

  ```json
  {"index": 7, "error": "light-off duration 50ms is neither an intra-character gap (80-120ms) nor an inter-character gap (240-360ms)"}
  ```

- `400 Bad Request`：请求体不是合法 JSON，`durations` 不是整数数组（如含小数、字符串），
  `tick_micros` 不是整数或不在 1–1,000,000 闭区间内，`include_trace` 不是布尔值
  （`null`、字符串、数字等），或 `durations[i] × tick_micros`
  发生整数乘法溢出。响应通过 `field` 指出字段；数组元素溢出时字段形如
  `durations[3]`：

  ```json
  {"field": "tick_micros", "error": "tick_micros must be between 1 and 1000000 microseconds, got 0"}
  ```

另提供 `GET /healthz` 健康检查与 `GET /stats` 解码统计（见下节）。

## 解码统计（GET /stats）

值班负责人在一轮巡检后可查看本进程启动以来的解码概况：

```bash
curl "http://localhost:${API_PORT:-8080}/stats"
```

```json
{
  "started_at": "2026-09-14T01:23:45Z",
  "total": 12,
  "success": 9,
  "bad_request": 2,
  "undecodable": 1
}
```

统计口径：

- 每次 `POST /decode` 完成时按最终结果**只记一次**：`2xx` 计入 `success`，`400`
  计入 `bad_request`（非法 JSON、字段类型错误、`tick_micros` 越界、乘法溢出），
  `422` 计入 `undecodable`（记录无法判读）；`total` 为三类之和。分类完全由现有
  解码链路产生的最终状态码决定。
- `GET /healthz`、`GET /stats` 本身以及未知路由不参与累计。
- 统计只保存启动时间与计数，**不保存、也不暴露任何脉冲原文**；统计在响应写出后
  才记账，其自身的任何异常都不会改变解码结果。

生命周期：计数为**进程级**，自服务启动（`started_at`，UTC，秒精度）开始累计，
进程重启后从零开始，不做持久化；尚无请求时也返回完整的零值结构。计数并发安全，
读取时所有字段来自同一快照，因此任意时刻 `total` 恒等于三类计数之和。

## 运行

```bash
# 启动 api（宿主端口默认 8080，可用 API_PORT 覆盖）
API_PORT=9000 docker compose up --build api

# 一次性验收：verify 服务等待 api 健康后运行 ./acceptance 黑盒测试并退出，
# 退出码即验收结果
API_PORT=9000 docker compose up --build --exit-code-from verify --abort-on-container-exit
```

## 本地开发

```bash
go test ./...     # 单元测试（acceptance 在未设置 API_URL 时自动跳过）
go build -o api . && ./api
```

## 代码结构

```
main.go                     入口（LISTEN_ADDR 可覆盖监听地址，默认 :8080）
internal/decoder/           判读核心：整数刻度换算、闭区间窗口、映射表、首个异常定位
internal/server/            Gin 路由与错误映射（200 / 422 / 400）、GET /stats 接线
internal/stats/             进程级解码结果计数（并发安全、仅内存、重启清零）
acceptance/                 verify 服务执行的黑盒验收测试
```
