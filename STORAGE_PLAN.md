# 区块存储方案

本文档确定当前项目后续的区块与共识状态存储方式。

## 目标

- 区块数据不再只保存在内存中
- 每个节点拥有自己的本地数据库
- 区块与共识状态按节点隔离，便于恢复、调试和多节点本地运行
- 为 HotStuff 系列流水线和 SBFT 视图切换提供持久化基础

## 存储原则

- 区块保存单独放在一个目录下
- 每个节点对应一个独立数据库
- 数据库优先采用 `LevelDB`
- Redis 继续用于：
  - 集群配置
  - 演示密钥材料
  - client 侧延迟统计
- 区块、QC、view-change 证明、投票状态不再依赖 Redis 保存

## 建议目录结构

建议在仓库根目录下新增：

```text
data/
  node-1/
    blocks/
      leveldb/
    state/
      leveldb/
  node-2/
    blocks/
      leveldb/
    state/
      leveldb/
  node-3/
    ...
```

如果想简化，也可以每个节点只保留一个数据库：

```text
data/
  node-1/
    leveldb/
  node-2/
    leveldb/
```

推荐优先采用“两库分离”：
- `blocks` 库保存区块和 QC
- `state` 库保存运行态、投票记录、提交指针、`highQC`、`lockedQC`
- `client/metrics` 库保存时延、batch 和滑动窗口吞吐量样本

这样更利于后续做清理、恢复和调试。

## 每个节点数据库建议保存的内容

### 1. blocks 库

- `block:<blockID>`
  - 区块头
  - 父块 ID
  - view
  - height 或 slot
  - payload / tx
  - digest
- `qc:<blockID>`
  - blockID
  - view
  - QC 内容
  - signer 集合
- `commitqc:<blockID>`
  - Fast-HotStuff 或 HPBFT 的 CommitQC
- `viewchange:<height>:<view>:<from>`
  - 视图切换证明
- `newview:<height>:<view>`
  - new-view 证明

### 2. state 库

- `meta:highQC`
- `meta:lockedQC`
- `meta:lastCommittedBlock`
- `meta:lastExecutedBlock`
- `meta:currentView`
- `meta:currentHeight`
- `vote:<view>`
  - 本节点在该 view 已投票给哪个 block
- `prepare:<height>:<view>`
  - SBFT 的 Prepare 收集状态
- `commitproof:<height>:<view>`
  - SBFT 的 CommitProof

### 3. client metrics 库

- `metric:<height>`
  - 该高度的 latency
  - batch
  - throughput
  - 记录时间
- `sample:<recordedAt>`
  - 滑动窗口吞吐量样本
  - 对应提交交易数

## 按算法的存储重点

### SBFT

重点持久化：
- `PrePrepare`
- `Prepare` 收集结果
- `CommitProof`
- `SBFTViewChange`
- `SBFTNewView`

原因：
- 视图切换需要恢复 prepared proof
- 节点重启后需要知道自己在哪个 view/height

### HotStuff

重点持久化：
- block tree
- `highQC`
- `lockedQC`
- 已投票 view
- 已提交链头

原因：
- 流水线提交依赖连续 QC 链
- 重启后必须继续围绕已知最高 `highQC` 延展

### Fast-HotStuff

重点持久化：
- block tree
- 普通 QC
- `CommitQC`
- 快路径 / 回退路径的状态标记

### HPBFT

重点持久化：
- 沿用 Fast-HotStuff 的所有状态
- 组领导者转发记录
- 分组签名聚合结果
- 主 leader 汇总得到的最终 QC / CommitQC

原因：
- HPBFT 的核心增量在“分层收集”
- 这些中间聚合结果不能只放内存，否则 leader 或组领导者重启后难以恢复

## 代码落点建议

建议新增目录：

```text
internal/storage/
  leveldb/
    blocks.go
    state.go
    codec.go
```

建议抽象接口：

```go
type BlockStore interface {
    SaveBlock(block Block) error
    GetBlock(id string) (Block, error)
    SaveQC(qc QC) error
    GetQC(blockID string) (QC, error)
}

type StateStore interface {
    SaveHighQC(qc QC) error
    LoadHighQC() (QC, error)
    SaveLockedQC(qc QC) error
    LoadLockedQC() (QC, error)
    SaveCurrentView(view int) error
    LoadCurrentView() (int, error)
}
```

节点初始化时按 `nodeID` 打开自己的数据库：
- `data/node-<id>/blocks/leveldb`
- `data/node-<id>/state/leveldb`

## 为什么每节点独立数据库更合适

- 更符合真实部署方式，每个副本维护自己的本地账本和状态
- 调试时更容易看单个节点到底保存了什么
- 节点崩溃恢复不依赖共享状态
- 避免多个节点竞争同一个本地数据库文件

## 为什么优先 LevelDB

- Go 生态可用实现成熟
- KV 模型足够覆盖 block/QC/state 的持久化
- 本地开发方便，不需要额外服务
- 对当前这个最小项目来说，比引入完整 SQL 存储更轻

## 建议实现顺序

1. 先为每个节点引入 `state` 库
2. 再引入 `blocks` 库
3. 先让 `hotstuff` 使用持久化 `highQC` / `lockedQC`
4. 再让 `sbft` 使用持久化 prepared proof / view-change proof
5. 最后为 `hpbft` 增加组领导者分层聚合结果的持久化

## 当前需要确认的工程决策

- 是使用一个 LevelDB 还是 `blocks/state` 两个 LevelDB
- key 编码采用：
  - 纯字符串前缀
  - 还是固定二进制前缀
- value 编码采用：
  - JSON
  - 还是 gob / protobuf
- 是否允许启动时自动清空旧数据
- 是否需要给每个算法独立子目录
