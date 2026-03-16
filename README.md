# MYBFT (Go)

基于给定规范实现的最小可运行项目，提供：

- `genkey`：写入 Redis 密钥与集群配置。
- `client`：提供 `/start` 和 `/end`，按 `q=floor(N/3)+1` 输出时延。
- `node`：支持 `sbft/hotstuff/fast-hotstuff/hpbft` 四类路由与闭环流程。

## 构建

```bash
go build ./cmd/genkey
go build ./cmd/client
go build ./cmd/node
```

## 运行（N=4）

1. 启动 Redis。
2. `go run ./cmd/genkey 4`
3. `go run ./cmd/client 4`
4. 分别启动节点（示例 SBFT）：
   - `go run ./cmd/node 2 sbft`
   - `go run ./cmd/node 3 sbft`
   - `go run ./cmd/node 4 sbft`
   - `go run ./cmd/node 1 sbft`

Client 会在每个 `height` 收到 `q=2` 个去重 `/end` 后打印：

```text
height %d latency is %f batch is %d throughput is %f tx/s
```

## 实现说明

- 时间戳权威统一为 Client 接收时间（UnixNano）。
- `/end` 使用 `(height, from)` 去重，first-end-wins 写入 `latency:end`。
- Client 额外输出滑动窗口吞吐量，默认窗口为最近 `5` 秒，可通过环境变量 `MYBFT_TPS_WINDOW_SECONDS` 调整。
- Client 会将吞吐量样本和每个高度的统计结果持久化到本地 `LevelDB`，默认路径为 `data/client/metrics/leveldb`。
- SBFT：`PrePrepare -> Prepare(t, 回 leader) -> CommitProof(广播) -> /end`。
- HotStuff：链式 proposal + QC，按 three-chain commit 提交祖先块后 `/end`。
- Fast-HotStuff/HPBFT：当前仍为 Proposal + Vote + QC 的简化闭环。
- 负载模拟在 proposal 校验后、投票前执行，`nums` 每个高度重置。


## Windows 一键启动脚本（BAT）

使用仓库根目录下的 `start_cluster.cmd`，支持“脚本名 + 算法名 + 节点数”自动执行：

1. 运行 `genkey` 生成签名材料与 `cluster:config`
2. 打开一个新终端运行 `client`
3. 为每个 `node` 打开独立终端并运行指定算法

### 用法

```bat
.\start_cluster.cmd sbft 4
.\start_cluster.cmd hotstuff 7
.\start_cluster.cmd fast-hotstuff 4
.\start_cluster.cmd hpbft 10
```

### 参数

- 第 1 个参数：算法名，必须为 `sbft | hotstuff | fast-hotstuff | hpbft`
- 第 2 个参数：节点数 `N`（正整数）

### 前置条件

- 已安装 Go（`go` 在 PATH）
- 已安装 Redis 客户端（`redis-cli` 在 PATH）
- 已安装 Redis 服务端（`redis-server` 在 PATH）
- 本机可访问 Redis（默认 `127.0.0.1:6379`）

> 说明：已改为纯 BAT 启动，不再依赖 PowerShell 脚本签名策略。
> 脚本会先检测 Redis；若未运行，会自动打开新终端执行 `redis-server`。

## 查看本地指标

```bash
go run ./cmd/metrics
go run ./cmd/metrics 10
```

- 不带参数时：列出本地 `LevelDB` 中所有已保存的高度指标
- 带 `height` 参数时：查看指定高度的时延、batch、吞吐量与记录时间
