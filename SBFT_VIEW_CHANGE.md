# SBFT 视图切换清单与流程说明

本文档面向当前仓库中的简化 `sbft` 实现，目标是为后续增加视图切换（view change）提供一份可执行的开发清单，并说明推荐的算法流程。

当前代码基础：
- 正常路径为 `PrePrepare -> Prepare -> CommitProof`。
- leader 按 `leader = ((view-1) % N) + 1` 轮换。
- 节点状态主要位于 `internal/nodesvc/service.go`。
- 当前尚未实现超时、视图切换、持久化日志、checkpoint、BLS。
- 后续建议为每个节点引入独立 `LevelDB`，保存 prepared proof、view-change proof 和当前 view/height。

## 实现清单

### 1. 配置与状态

- [ ] 在 `Service` 中增加视图切换相关状态：
  - `viewChanging bool`
  - `lastProgressAt time.Time`
  - `viewChangeVotes map[int]map[int]common.ConsensusMessage`
  - `newViewSent map[int]bool`
  - `preparedProofs map[int]PreparedProof`
- [ ] 为每个高度保留可用于切换视图的最小证明信息：
  - 最近一次已接受的 proposal `digest`
  - 已收集的签名份额
  - 是否已经提交完成
- [ ] 增加超时配置：
  - `requestTimeout`
  - `viewChangeTimeout`
  - 可选的指数退避或随 view 增长的超时

### 2. 消息类型

- [ ] 在 `internal/common/types.go` 的 `ConsensusMessage` 中增加视图切换消息字段：
  - `ViewChangeProof`
  - `PreparedDigest`
  - `PreparedQC`
  - `ReplicaState`
  - `CandidateView`
- [ ] 定义两类新消息：
  - `SBFTViewChange`
  - `SBFTNewView`
- [ ] 明确每类消息的签名输入，避免不同节点对同一消息编码不一致

### 3. 定时器与触发条件

- [ ] 在节点启动时启动每高度/每视图的进度定时器
- [ ] 在以下事件发生时刷新 `lastProgressAt`：
  - 收到合法 proposal
  - 收到合法 commit proof
  - 本地完成一个高度
- [ ] 在超时后触发视图切换：
  - 若本视图内长时间未收到 proposal
  - 或收到了 proposal 但长时间未形成 commit proof

### 4. 视图切换投票

- [ ] 节点超时后构造 `SBFTViewChange` 并广播
- [ ] `SBFTViewChange` 至少携带：
  - 目标 `view`
  - 当前 `height`
  - 本地已知的最高 prepared/committed 证明
  - 发送者 `from`
  - 签名
- [ ] 节点收到 `SBFTViewChange` 后校验：
  - `height` 是否匹配当前高度
  - `view` 是否大于当前视图
  - 签名是否正确
  - 若带 prepared proof，则 proof 是否自洽

### 5. NewView 生成

- [ ] 新 leader 在收到至少 `t=floor(2N/3)+1` 个 `SBFTViewChange` 后生成 `SBFTNewView`
- [ ] `SBFTNewView` 中应包含：
  - 新视图号
  - 收到的 view-change 证明集合或聚合摘要
  - 被选中的 proposal/digest
  - 若需要，携带重发的 proposal 内容
- [ ] 选择 proposal 的规则要固定：
  - 优先使用 view-change 中证明最高的 prepared digest
  - 如果没有 prepared proof，则由新 leader 生成新 proposal

### 6. 切换后的恢复

- [ ] 节点收到合法 `SBFTNewView` 后：
  - 更新本地 `view`
  - 清理旧视图的临时投票缓存
  - 装载新 leader 指定的 proposal
  - 重新进入 `PrePrepare -> Prepare -> CommitProof`
- [ ] 避免重复进入同一 `view`
- [ ] 若已经完成该高度，则拒绝旧高度的视图切换消息

### 7. 安全性规则

- [ ] 同一节点在同一 `height`、同一 `view` 只发送一次 `SBFTViewChange`
- [ ] 节点不能为两个不同 digest 在同一视图重复签名
- [ ] 新 leader 不能忽略更高 prepared proof
- [ ] `SBFTNewView` 必须可验证，不能只是“我宣布自己是新 leader”

### 8. 工程改动点

- [ ] 修改 `internal/common/types.go`
  - 增加消息字段与可能的证明结构
- [ ] 修改 `internal/nodesvc/service.go`
  - 增加定时器
  - 增加 `processViewChange`
  - 增加 `processNewView`
  - 在正常路径中刷新进度时间
- [ ] 新增 `internal/storage/`
  - 为每个节点打开独立数据库
  - 保存 prepared proof、view-change、new-view 和当前 view
- [ ] 如需 Redis 辅助调试，可在 `internal/redisx` 中增加状态观测键
- [ ] 更新 `ALGORITHMS.md` 与 `readme.me`

### 9. 验证与测试

- [ ] 正常路径无故障时，流程不应频繁误触发 view change
- [ ] leader 不发送 proposal 时，其余节点应能推进到下一个 view
- [ ] 老 leader 恢复后发送旧 proposal，节点应拒绝
- [ ] 新 leader 应能沿用旧视图中的最高 prepared proposal
- [ ] 多节点同时超时的情况下，只能形成一个合法 new view
- [ ] 在高度已完成后，旧 view-change 消息不应污染下一高度状态

## 推荐算法流程

下面的流程是针对当前简化 `sbft` 代码结构的建议版本，不是完整论文版 SBFT，而是“能落地到现有代码”的最小视图切换流程。

### 一、正常路径

1. leader 根据当前 `view` 生成 `PrePrepare`
2. 副本校验 proposal 后，返回 `Prepare` 给 leader
3. leader 收集至少 `t` 个签名份额后，聚合出 `CommitProof`
4. leader 广播 `CommitProof`
5. 所有节点完成该高度并推进到 `height+1`

### 二、进入视图切换

触发条件：
- 当前视图在超时时间内没有收到合法 proposal
- 或已收到 proposal，但一直没有形成 `CommitProof`

节点触发后执行：
1. 本地将目标视图设为 `view+1`
2. 构造 `SBFTViewChange`
3. 携带本地最高 prepared proof
4. 广播给所有节点，特别是新视图 leader

### 三、新 leader 收集视图切换消息

新视图 leader 为 `leader = ((newView-1) % N) + 1`。

它需要：
1. 收集至少 `t` 个 `SBFTViewChange`
2. 从这些消息中选出“最高 prepared proof”
3. 按固定规则生成 `SBFTNewView`

选择规则建议：
- 若存在 prepared proof，则必须沿用其中视图最高、证明最强的 `digest`
- 若不存在 prepared proof，则允许新 leader 生成新的 proposal

### 四、广播 NewView

新 leader 广播 `SBFTNewView`，消息中至少包括：
- 新视图号
- view-change 证明集合
- 选中的 `digest`
- 若需要，附带 proposal 内容

副本收到后执行：
1. 校验 `SBFTNewView` 是否包含至少 `t` 个合法 `SBFTViewChange`
2. 校验 leader 选中的 proposal 是否符合“最高 prepared proof”规则
3. 若验证通过，更新本地 `view`
4. 进入该视图下的正常 `sbft` 提交流程

### 五、完成新视图中的提交流程

进入新视图后，流程重新回到正常路径：
1. 新 leader 发送 proposal
2. 副本返回 sign share
3. leader 聚合 commit proof
4. 广播 commit proof
5. 高度完成

## 与当前代码的对应关系

- 正常路径入口：`internal/nodesvc/service.go` 中的 `processSBFT`
- leader 判断：`internal/nodesvc/service.go` 中的 `isLeader` 和 `leaderID`
- 广播/单播：`internal/nodesvc/service.go` 中的 `broadcast`、`sendTo`
- 消息结构：`internal/common/types.go` 中的 `ConsensusMessage`

## 建议的最小落地顺序

1. 先加超时检测与 `SBFTViewChange`
2. 再加新 leader 收集 `t` 个 view-change 后广播 `SBFTNewView`
3. 再加“最高 prepared proof”选择规则
4. 最后补测试与文档

## 当前需要提前确认的设计决策

- 是否继续沿用当前演示签名模型，还是同时切到 BLS
- `SBFTNewView` 是否直接携带完整 proposal
- prepared proof 是否只保留单个最高项，还是保留完整证据集
- 超时是否固定，还是按 view 增长
