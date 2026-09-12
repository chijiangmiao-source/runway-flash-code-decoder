# 跑道入口灯报码判读 API

纯后端服务：把夜间巡检时灯控制器导出的**亮/灭毫秒时长序列**判读为设备播出的 Morse 报码。
技术栈：Go + Gin，测试使用 testify。

## 记录格式

请求体为从**亮灯**开始的正整数毫秒时长数组，亮灭严格交替，且必须以**亮灯**结束（长度必为奇数）：

```json
{"durations": [100, 100, 300]}
```

下标 0、2、4… 为亮灯脉冲，1、3、5… 为灭灯间隔。

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

- `422 Unprocessable Entity`：任一脉冲非法（非正整数、亮/灭时长落在窗口之外、记录以灭灯结尾）或字符不在映射表。
  **整次记录被拒绝，绝不返回部分报码**。响应指出**首个问题脉冲的下标**；若问题为未映射字符，`index`
  为该字符首个脉冲的下标（脉冲按下标从小到大检查，损坏记录稳定停在首个异常位置）：

  ```json
  {"index": 7, "error": "light-off duration 50ms is neither an intra-character gap (80-120ms) nor an inter-character gap (240-360ms)"}
  ```

- `400 Bad Request`：请求体不是合法 JSON，或 `durations` 不是整数数组（如含小数、字符串）。

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
internal/decoder/           判读核心：闭区间窗口、映射表、首个异常定位
internal/server/            Gin 路由与错误映射（200 / 422 / 400）
acceptance/                 verify 服务执行的黑盒验收测试
```
