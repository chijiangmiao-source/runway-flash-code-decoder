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

## 错误语义

- `422 Unprocessable Entity`：按 `tick_micros` 整数换算后，任一脉冲非法（非正整数、亮/灭时长落在窗口之外、记录以灭灯结尾）或字符不在映射表。
  **整次记录被拒绝，绝不返回部分报码**。响应指出**首个问题脉冲的原始下标**；若问题为未映射字符，`index`
  为该字符首个脉冲的下标（脉冲按下标从小到大检查，损坏记录稳定停在首个异常位置）：

  ```json
  {"index": 7, "error": "light-off duration 50ms is neither an intra-character gap (80-120ms) nor an inter-character gap (240-360ms)"}
  ```

- `400 Bad Request`：请求体不是合法 JSON，`durations` 不是整数数组（如含小数、字符串），
  `tick_micros` 不是整数或不在 1–1,000,000 闭区间内，或 `durations[i] × tick_micros`
  发生整数乘法溢出。响应通过 `field` 指出字段；数组元素溢出时字段形如
  `durations[3]`：

  ```json
  {"field": "tick_micros", "error": "tick_micros must be between 1 and 1000000 microseconds, got 0"}
  ```

另提供 `GET /healthz` 健康检查。

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
internal/server/            Gin 路由与错误映射（200 / 422 / 400）
acceptance/                 verify 服务执行的黑盒验收测试
```
